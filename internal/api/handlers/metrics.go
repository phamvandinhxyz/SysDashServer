package handlers

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"SysDashServer/internal/collector"
	"SysDashServer/internal/database"
	"SysDashServer/internal/models"
	"SysDashServer/internal/service"

	"github.com/gin-gonic/gin"
)

type MetricsHandler struct {
	db        *database.DB
	monitor   *service.MonitorService
	collector *collector.SystemCollector
}

func NewMetricsHandler(db *database.DB, monitor *service.MonitorService, collector *collector.SystemCollector) *MetricsHandler {
	return &MetricsHandler{
		db:        db,
		monitor:   monitor,
		collector: collector,
	}
}

// GetCurrent returns the latest real-time system metrics snapshot
func (h *MetricsHandler) GetCurrent(c *gin.Context) {
	metric := h.monitor.GetCurrentMetric()
	c.JSON(http.StatusOK, metric)
}

// GetHostInfo returns static host, CPU and OS information
func (h *MetricsHandler) GetHostInfo(c *gin.Context) {
	info := h.collector.GetHostInfo()
	c.JSON(http.StatusOK, info)
}

// GetProcesses returns filtered, searched and sorted process list
func (h *MetricsHandler) GetProcesses(c *gin.Context) {
	search := c.Query("search")
	sort := c.DefaultQuery("sort", "cpu")
	order := c.DefaultQuery("order", "desc")
	limitStr := c.DefaultQuery("limit", "10")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 10
	}

	procs := h.collector.GetProcesses(search, sort, order, limit)
	c.JSON(http.StatusOK, gin.H{
		"count":     len(procs),
		"search":    search,
		"sort":      sort,
		"order":     order,
		"processes": procs,
	})
}

// GetHistory returns filtered and downsampled historical metrics (with downsampling)
// Query params:
// - range: "1h", "6h", "12h", "24h", "7d", "30d", "90d", "180d", "365d", "1y"
// - from, to: RFC3339 format (optional)
// - bucket: "none", "1m", "5m", "15m", "1h", "1d", "auto" (default "auto")
// - limit: max records (default 500, max 2000)
func (h *MetricsHandler) GetHistory(c *gin.Context) {
	rangeParam := c.Query("range")
	fromStr := c.Query("from")
	toStr := c.Query("to")
	bucketParam := c.DefaultQuery("bucket", "auto")

	from, to := parseTimeRange(rangeParam, fromStr, toStr)

	limitStr := c.DefaultQuery("limit", "500")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 2000 {
		limit = 500
	}

	var records []models.MetricRecord
	query := h.db.Model(&models.MetricRecord{}).
		Where("timestamp >= ? AND timestamp <= ?", from, to).
		Order("timestamp asc")

	if err := query.Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query history metrics"})
		return
	}

	// Determine bucket duration for downsampling
	bucketDuration := determineBucketDuration(bucketParam, from, to, len(records))

	if bucketDuration > 0 && len(records) > 0 {
		downsampled := aggregateRecords(records, bucketDuration)
		if len(downsampled) > limit {
			downsampled = downsampled[len(downsampled)-limit:]
		}
		c.JSON(http.StatusOK, gin.H{
			"range":     rangeParam,
			"from":      from,
			"to":        to,
			"bucket":    bucketDuration.String(),
			"count":     len(downsampled),
			"raw_count": len(records),
			"metrics":   downsampled,
		})
		return
	}

	if len(records) > limit {
		records = records[len(records)-limit:]
	}

	c.JSON(http.StatusOK, gin.H{
		"range":     rangeParam,
		"from":      from,
		"to":        to,
		"bucket":    "none",
		"count":     len(records),
		"raw_count": len(records),
		"metrics":   records,
	})
}

// GetSummary returns min, max, avg statistics over a given range (for summary card)
func (h *MetricsHandler) GetSummary(c *gin.Context) {
	rangeParam := c.Query("range")
	fromStr := c.Query("from")
	toStr := c.Query("to")

	from, to := parseTimeRange(rangeParam, fromStr, toStr)

	var records []models.MetricRecord
	err := h.db.Model(&models.MetricRecord{}).
		Where("timestamp >= ? AND timestamp <= ?", from, to).
		Find(&records).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to calculate metrics summary"})
		return
	}

	if len(records) == 0 {
		c.JSON(http.StatusOK, models.MetricSummary{
			TimeRange:   rangeParam,
			From:        from,
			To:          to,
			SampleCount: 0,
		})
		return
	}

	var sumCPU, sumMem, sumDisk, sumUp, sumDown float64
	minCPU := math.MaxFloat64
	maxCPU := 0.0
	minMem := math.MaxFloat64
	maxMem := 0.0
	maxDisk := 0.0
	maxUp := 0.0
	maxDown := 0.0

	for _, r := range records {
		sumCPU += r.CPUPercent
		if r.CPUPercent < minCPU {
			minCPU = r.CPUPercent
		}
		if r.CPUPercent > maxCPU {
			maxCPU = r.CPUPercent
		}

		sumMem += r.MemUsedPercent
		if r.MemUsedPercent < minMem {
			minMem = r.MemUsedPercent
		}
		if r.MemUsedPercent > maxMem {
			maxMem = r.MemUsedPercent
		}

		sumDisk += r.DiskPercent
		if r.DiskPercent > maxDisk {
			maxDisk = r.DiskPercent
		}

		sumUp += r.NetUploadRate
		if r.NetUploadRate > maxUp {
			maxUp = r.NetUploadRate
		}

		sumDown += r.NetDownloadRate
		if r.NetDownloadRate > maxDown {
			maxDown = r.NetDownloadRate
		}
	}

	n := float64(len(records))
	summary := models.MetricSummary{
		TimeRange:       rangeParam,
		From:            from,
		To:              to,
		SampleCount:     len(records),
		AvgCPU:          round(sumCPU / n),
		MinCPU:          round(minCPU),
		MaxCPU:          round(maxCPU),
		AvgMemPercent:   round(sumMem / n),
		MinMemPercent:   round(minMem),
		MaxMemPercent:   round(maxMem),
		AvgDiskPercent:  round(sumDisk / n),
		MaxDiskPercent:  round(maxDisk),
		AvgUploadRate:   round(sumUp / n),
		MaxUploadRate:   round(maxUp),
		AvgDownloadRate: round(sumDown / n),
		MaxDownloadRate: round(maxDown),
	}

	c.JSON(http.StatusOK, summary)
}

func parseTimeRange(rangeParam, fromStr, toStr string) (time.Time, time.Time) {
	now := time.Now()
	to := now
	if toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	if fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			return t, to
		}
	}

	switch strings.ToLower(rangeParam) {
	case "1h":
		return to.Add(-1 * time.Hour), to
	case "6h":
		return to.Add(-6 * time.Hour), to
	case "12h":
		return to.Add(-12 * time.Hour), to
	case "24h", "1d":
		return to.Add(-24 * time.Hour), to
	case "7d", "1w":
		return to.Add(-7 * 24 * time.Hour), to
	case "30d", "1m":
		return to.Add(-30 * 24 * time.Hour), to
	case "90d", "3m":
		return to.Add(-90 * 24 * time.Hour), to
	case "180d", "6m":
		return to.Add(-180 * 24 * time.Hour), to
	case "365d", "1y", "1year":
		return to.Add(-365 * 24 * time.Hour), to
	default:
		// Default 1 hour if nothing given
		return to.Add(-1 * time.Hour), to
	}
}

func determineBucketDuration(bucketParam string, from, to time.Time, count int) time.Duration {
	switch strings.ToLower(bucketParam) {
	case "none", "0":
		return 0
	case "1m":
		return 1 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h":
		return 1 * time.Hour
	case "1d", "24h":
		return 24 * time.Hour
	case "auto":
		diff := to.Sub(from)
		if count < 100 {
			return 0 // No downsampling needed if small count
		}
		if diff <= 2*time.Hour {
			return 1 * time.Minute
		} else if diff <= 12*time.Hour {
			return 5 * time.Minute
		} else if diff <= 2*24*time.Hour {
			return 15 * time.Minute
		} else if diff <= 14*24*time.Hour {
			return 1 * time.Hour
		} else if diff <= 60*24*time.Hour {
			return 6 * time.Hour
		} else {
			return 24 * time.Hour // 1 day bucket for up to 1-year range (~365 points)
		}
	default:
		return 0
	}
}

func aggregateRecords(records []models.MetricRecord, bucket time.Duration) []models.MetricRecord {
	if len(records) == 0 || bucket <= 0 {
		return records
	}

	var result []models.MetricRecord
	var currentBucketTime time.Time
	var count int
	var sumCPU, sumMem, sumDisk, sumUp, sumDown float64
	var lastMemTotal, lastDiskTotal uint64

	flushBucket := func() {
		if count == 0 {
			return
		}
		c := float64(count)
		result = append(result, models.MetricRecord{
			Timestamp:       currentBucketTime,
			CPUPercent:      round(sumCPU / c),
			MemTotal:        lastMemTotal,
			MemUsed:         uint64(float64(lastMemTotal) * (sumMem / c) / 100.0),
			MemUsedPercent:  round(sumMem / c),
			DiskTotal:       lastDiskTotal,
			DiskPercent:     round(sumDisk / c),
			NetUploadRate:   round(sumUp / c),
			NetDownloadRate: round(sumDown / c),
		})
	}

	for _, r := range records {
		bucketStart := r.Timestamp.Truncate(bucket)
		if currentBucketTime.IsZero() {
			currentBucketTime = bucketStart
		}

		if !bucketStart.Equal(currentBucketTime) {
			flushBucket()
			currentBucketTime = bucketStart
			count = 0
			sumCPU = 0
			sumMem = 0
			sumDisk = 0
			sumUp = 0
			sumDown = 0
		}

		count++
		sumCPU += r.CPUPercent
		sumMem += r.MemUsedPercent
		sumDisk += r.DiskPercent
		sumUp += r.NetUploadRate
		sumDown += r.NetDownloadRate
		lastMemTotal = r.MemTotal
		lastDiskTotal = r.DiskTotal
	}

	flushBucket()
	return result
}

func round(val float64) float64 {
	return math.Round(val*100) / 100
}

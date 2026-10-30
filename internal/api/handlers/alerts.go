package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"SysDashServer/internal/database"
	"SysDashServer/internal/models"

	"github.com/gin-gonic/gin"
)

type AlertHandler struct {
	db *database.DB
}

func NewAlertHandler(db *database.DB) *AlertHandler {
	return &AlertHandler{db: db}
}

// ListAlerts returns filtered and paginated past alert logs (with filtering & pagination)
// Query params:
// - type: "CPU", "MEMORY", "DISK", or "" (all)
// - level: "WARNING", "CRITICAL", or "" (all)
// - range: "1h", "24h", "7d", "30d"
// - from, to: RFC3339 timestamps
// - page: 1-based page number (default 1)
// - limit: items per page (default 20, max 200)
func (h *AlertHandler) ListAlerts(c *gin.Context) {
	typeParam := c.Query("type")
	levelParam := c.Query("level")
	rangeParam := c.Query("range")
	fromStr := c.Query("from")
	toStr := c.Query("to")

	pageStr := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 200 {
		limit = 20
	}

	query := h.db.Model(&models.AlertLog{})

	if typeParam != "" {
		query = query.Where("UPPER(type) = ?", strings.ToUpper(typeParam))
	}

	if levelParam != "" {
		query = query.Where("UPPER(level) = ?", strings.ToUpper(levelParam))
	}

	if rangeParam != "" || fromStr != "" || toStr != "" {
		from, to := parseTimeRange(rangeParam, fromStr, toStr)
		query = query.Where("timestamp >= ? AND timestamp <= ?", from, to)
	}

	var totalCount int64
	query.Count(&totalCount)

	offset := (page - 1) * limit
	var logs []models.AlertLog
	if err := query.Order("timestamp desc").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query alerts"})
		return
	}

	totalPages := int((totalCount + int64(limit) - 1) / int64(limit))
	if totalPages == 0 {
		totalPages = 1
	}

	c.JSON(http.StatusOK, gin.H{
		"page":        page,
		"limit":       limit,
		"total":       totalCount,
		"total_pages": totalPages,
		"alerts":      logs,
	})
}

// GetThreshold returns configured alert thresholds
func (h *AlertHandler) GetThreshold(c *gin.Context) {
	var threshold models.AlertThreshold
	if err := h.db.First(&threshold).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "threshold not found"})
		return
	}
	c.JSON(http.StatusOK, threshold)
}

// UpdateThreshold updates alert thresholds (Admin only)
func (h *AlertHandler) UpdateThreshold(c *gin.Context) {
	var req models.AlertThreshold
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	var current models.AlertThreshold
	if err := h.db.First(&current).Error; err != nil {
		req.UpdatedAt = time.Now()
		if err := h.db.Create(&req).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save threshold"})
			return
		}
		c.JSON(http.StatusOK, req)
		return
	}

	current.CPUPercent = req.CPUPercent
	current.MemPercent = req.MemPercent
	current.DiskPercent = req.DiskPercent
	current.Enabled = req.Enabled
	current.UpdatedAt = time.Now()

	if err := h.db.Save(&current).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update threshold"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "threshold updated successfully", "threshold": current})
}

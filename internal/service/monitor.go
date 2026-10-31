package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"SysDashServer/config"
	"SysDashServer/internal/collector"
	"SysDashServer/internal/database"
	"SysDashServer/internal/models"

	"github.com/gorilla/websocket"
)

type ClientWSOptions struct {
	Limit int    `json:"limit"`
	Sort  string `json:"sort"`
	Order string `json:"order"`
}

func normalizeWSOptions(opt ClientWSOptions) ClientWSOptions {
	if opt.Limit <= 0 || opt.Limit > 100 {
		opt.Limit = 5
	}
	opt.Sort = strings.ToLower(strings.TrimSpace(opt.Sort))
	if opt.Sort == "" {
		opt.Sort = "cpu"
	}
	opt.Order = strings.ToLower(strings.TrimSpace(opt.Order))
	if opt.Order == "" {
		opt.Order = "desc"
	}
	return opt
}

type MonitorService struct {
	cfg          *config.Config
	db           *database.DB
	collector    *collector.SystemCollector
	latestMetric models.RealtimeMetric

	mu         sync.RWMutex
	wsClients  map[*websocket.Conn]ClientWSOptions
	lastAlerts map[string]time.Time // Cooldown tracking per alert type
}

func NewMonitorService(cfg *config.Config, db *database.DB, collector *collector.SystemCollector) *MonitorService {
	return &MonitorService{
		cfg:        cfg,
		db:         db,
		collector:  collector,
		wsClients:  make(map[*websocket.Conn]ClientWSOptions),
		lastAlerts: make(map[string]time.Time),
	}
}

func (s *MonitorService) Start() {
	log.Printf("[MonitorService] Starting system monitoring service (Realtime: %v, DB Save: %v, Retention: %d days)",
		s.cfg.MetricInterval, s.cfg.PersistInterval, s.cfg.RetentionDays)

	// Initial metrics collection & immediate save
	s.latestMetric = s.collector.Collect()
	s.saveSnapshot(s.latestMetric)

	// 1. Realtime ticker (WebSocket broadcast + Alert check)
	go func() {
		ticker := time.NewTicker(s.cfg.MetricInterval)
		defer ticker.Stop()

		for range ticker.C {
			metric := s.collector.Collect()

			s.mu.Lock()
			s.latestMetric = metric
			s.mu.Unlock()

			// Check alerts
			s.checkAlerts(metric)

			// Broadcast to WS clients
			s.broadcastMetrics(metric)
		}
	}()

	// 2. Persistence ticker (Save to SQLite for historical graphs)
	go func() {
		ticker := time.NewTicker(s.cfg.PersistInterval)
		defer ticker.Stop()

		for range ticker.C {
			s.mu.RLock()
			m := s.latestMetric
			s.mu.RUnlock()

			s.saveSnapshot(m)
		}
	}()

	// 3. Housekeeping ticker: Clean historical records older than retentionDays (e.g. 365 days / 1 year)
	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			cutoff := time.Now().AddDate(0, 0, -s.cfg.RetentionDays)
			resMetrics := s.db.Where("timestamp < ?", cutoff).Delete(&models.MetricRecord{})
			resAlerts := s.db.Where("timestamp < ?", cutoff).Delete(&models.AlertLog{})
			if resMetrics.RowsAffected > 0 || resAlerts.RowsAffected > 0 {
				log.Printf("[Housekeeping] Cleaned %d metrics and %d alert logs older than %d days (%s)",
					resMetrics.RowsAffected, resAlerts.RowsAffected, s.cfg.RetentionDays, cutoff.Format("2006-01-02"))
			}
		}
	}()
}

func (s *MonitorService) saveSnapshot(m models.RealtimeMetric) {
	var diskTotal, diskUsed uint64
	var diskPct float64
	if len(m.Partitions) > 0 {
		diskTotal = m.Partitions[0].Total
		diskUsed = m.Partitions[0].Used
		diskPct = m.Partitions[0].UsedPercent
	}

	record := models.MetricRecord{
		Timestamp:       m.Timestamp,
		CPUPercent:      m.CPUPercent,
		MemTotal:        m.MemTotal,
		MemUsed:         m.MemUsed,
		MemUsedPercent:  m.MemUsedPercent,
		DiskTotal:       diskTotal,
		DiskUsed:        diskUsed,
		DiskPercent:     diskPct,
		DiskReadRate:    m.DiskReadRate,
		DiskWriteRate:   m.DiskWriteRate,
		NetBytesSent:    m.NetBytesSent,
		NetBytesRecv:    m.NetBytesRecv,
		NetUploadRate:   m.NetUploadRate,
		NetDownloadRate: m.NetDownloadRate,
	}

	if err := s.db.Create(&record).Error; err != nil {
		log.Printf("[MonitorService] Error saving metric record: %v", err)
	}
}

// filterMetricForClient returns a shallow copy of RealtimeMetric with TopProcesses sorted by requested (sortBy, order, limit)
// and TopMemoryProcesses clamped or re-sorted to limit.
func (s *MonitorService) filterMetricForClient(m models.RealtimeMetric, sortBy, order string, limit int) models.RealtimeMetric {
	if limit <= 0 || limit > 100 {
		limit = 5
	}
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	if sortBy == "" {
		sortBy = "cpu"
	}
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "" {
		order = "desc"
	}

	res := m
	if procs := s.collector.GetCachedProcesses(sortBy, order, limit); len(procs) > 0 {
		res.TopProcesses = procs
	} else if len(res.TopProcesses) > limit {
		res.TopProcesses = make([]models.ProcessInfo, limit)
		copy(res.TopProcesses, m.TopProcesses[:limit])
	}

	if memProcs := s.collector.GetCachedProcesses("memory", "desc", limit); len(memProcs) > 0 {
		res.TopMemoryProcesses = memProcs
	} else if len(res.TopMemoryProcesses) > limit {
		res.TopMemoryProcesses = make([]models.ProcessInfo, limit)
		copy(res.TopMemoryProcesses, m.TopMemoryProcesses[:limit])
	}

	return res
}

func (s *MonitorService) GetCurrentMetric(limit int, sortBy, order string) models.RealtimeMetric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filterMetricForClient(s.latestMetric, sortBy, order, limit)
}

func (s *MonitorService) AddWSClient(conn *websocket.Conn, opt ClientWSOptions) {
	opt = normalizeWSOptions(opt)
	s.mu.Lock()
	s.wsClients[conn] = opt
	m := s.filterMetricForClient(s.latestMetric, opt.Sort, opt.Order, opt.Limit)
	totalClients := len(s.wsClients)
	s.mu.Unlock()

	log.Printf("[WebSocket] Client connected (limit: %d, sort: %s, order: %s). Total active clients: %d", opt.Limit, opt.Sort, opt.Order, totalClients)

	// Send current metric snapshot immediately on connect
	msg, err := json.Marshal(m)
	if err == nil {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = conn.WriteMessage(websocket.TextMessage, msg)
	}
}

func (s *MonitorService) UpdateWSClientOptions(conn *websocket.Conn, limit int, sort, order string) {
	s.mu.Lock()
	opt, exists := s.wsClients[conn]
	if !exists {
		s.mu.Unlock()
		return
	}
	if limit > 0 && limit <= 100 {
		opt.Limit = limit
	}
	if sort != "" {
		opt.Sort = sort
	}
	if order != "" {
		opt.Order = order
	}
	opt = normalizeWSOptions(opt)
	s.wsClients[conn] = opt
	m := s.filterMetricForClient(s.latestMetric, opt.Sort, opt.Order, opt.Limit)
	s.mu.Unlock()

	// Send immediate snapshot with newly requested options
	msg, err := json.Marshal(m)
	if err == nil {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = conn.WriteMessage(websocket.TextMessage, msg)
	}
}

func (s *MonitorService) SetWSClientLimit(conn *websocket.Conn, limit int) {
	s.UpdateWSClientOptions(conn, limit, "", "")
}

func (s *MonitorService) RemoveWSClient(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.wsClients, conn)
	_ = conn.Close()
	log.Printf("[WebSocket] Client disconnected. Total active clients: %d", len(s.wsClients))
}

func (s *MonitorService) broadcastMetrics(metric models.RealtimeMetric) {
	s.mu.RLock()
	if len(s.wsClients) == 0 {
		s.mu.RUnlock()
		return
	}

	type clientEntry struct {
		conn *websocket.Conn
		opt  ClientWSOptions
	}
	clients := make([]clientEntry, 0, len(s.wsClients))
	for client, opt := range s.wsClients {
		clients = append(clients, clientEntry{conn: client, opt: opt})
	}
	s.mu.RUnlock()

	payloads := make(map[string][]byte)

	for _, c := range clients {
		cacheKey := fmt.Sprintf("%d-%s-%s", c.opt.Limit, c.opt.Sort, c.opt.Order)
		payload, ok := payloads[cacheKey]
		if !ok {
			filtered := s.filterMetricForClient(metric, c.opt.Sort, c.opt.Order, c.opt.Limit)
			data, err := json.Marshal(filtered)
			if err != nil {
				continue
			}
			payload = data
			payloads[cacheKey] = payload
		}

		c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			s.RemoveWSClient(c.conn)
		}
	}
}

func (s *MonitorService) checkAlerts(metric models.RealtimeMetric) {
	var threshold models.AlertThreshold
	if err := s.db.First(&threshold).Error; err != nil || !threshold.Enabled {
		return
	}

	now := time.Now()
	cooldown := 5 * time.Minute

	// Helper to check and record alert with cooldown
	triggerAlert := func(alertType, level string, value, limit float64, message string) {
		s.mu.Lock()
		lastTime, exists := s.lastAlerts[alertType]
		if exists && now.Sub(lastTime) < cooldown {
			s.mu.Unlock()
			return
		}
		s.lastAlerts[alertType] = now
		s.mu.Unlock()

		logRecord := models.AlertLog{
			Timestamp: now,
			Type:      alertType,
			Level:     level,
			Value:     value,
			Threshold: limit,
			Message:   message,
		}
		s.db.Create(&logRecord)
		log.Printf("[ALERT - %s] %s (Current: %.1f%%, Limit: %.1f%%)", alertType, message, value, limit)
	}

	// 1. CPU
	if metric.CPUPercent >= threshold.CPUPercent {
		triggerAlert("CPU", "WARNING", metric.CPUPercent, threshold.CPUPercent,
			fmt.Sprintf("CPU usage reached %.1f%% exceeding threshold %.1f%%", metric.CPUPercent, threshold.CPUPercent))
	}

	// 2. RAM
	if metric.MemUsedPercent >= threshold.MemPercent {
		triggerAlert("MEMORY", "WARNING", metric.MemUsedPercent, threshold.MemPercent,
			fmt.Sprintf("RAM usage reached %.1f%% exceeding threshold %.1f%%", metric.MemUsedPercent, threshold.MemPercent))
	}

	// 3. Disk
	for _, p := range metric.Partitions {
		if p.UsedPercent >= threshold.DiskPercent {
			triggerAlert("DISK", "CRITICAL", p.UsedPercent, threshold.DiskPercent,
				fmt.Sprintf("Disk %s (%s) usage reached %.1f%% exceeding threshold %.1f%%", p.Mountpoint, p.Device, p.UsedPercent, threshold.DiskPercent))
			break
		}
	}
}

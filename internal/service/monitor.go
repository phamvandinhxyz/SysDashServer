package service

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"SysDashServer/config"
	"SysDashServer/internal/collector"
	"SysDashServer/internal/database"
	"SysDashServer/internal/models"

	"github.com/gorilla/websocket"
)

type MonitorService struct {
	cfg          *config.Config
	db           *database.DB
	collector    *collector.SystemCollector
	latestMetric models.RealtimeMetric

	mu         sync.RWMutex
	wsClients  map[*websocket.Conn]bool
	lastAlerts map[string]time.Time // Cooldown tracking per alert type
}

func NewMonitorService(cfg *config.Config, db *database.DB, collector *collector.SystemCollector) *MonitorService {
	return &MonitorService{
		cfg:        cfg,
		db:         db,
		collector:  collector,
		wsClients:  make(map[*websocket.Conn]bool),
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
		NetBytesSent:    m.NetBytesSent,
		NetBytesRecv:    m.NetBytesRecv,
		NetUploadRate:   m.NetUploadRate,
		NetDownloadRate: m.NetDownloadRate,
	}

	if err := s.db.Create(&record).Error; err != nil {
		log.Printf("[MonitorService] Error saving metric record: %v", err)
	}
}

func (s *MonitorService) GetCurrentMetric() models.RealtimeMetric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestMetric
}

func (s *MonitorService) AddWSClient(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsClients[conn] = true
	log.Printf("[WebSocket] Client connected. Total active clients: %d", len(s.wsClients))

	// Send current metric immediately on connect
	msg, err := json.Marshal(s.latestMetric)
	if err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, msg)
	}
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

	payload, err := json.Marshal(metric)
	if err != nil {
		s.mu.RUnlock()
		return
	}

	clients := make([]*websocket.Conn, 0, len(s.wsClients))
	for client := range s.wsClients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	for _, client := range clients {
		client.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
			s.RemoveWSClient(client)
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

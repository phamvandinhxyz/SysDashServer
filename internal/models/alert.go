package models

import "time"

type AlertThreshold struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CPUPercent    float64   `json:"cpu_percent"`  // e.g., 90.0%
	MemPercent    float64   `json:"mem_percent"`  // e.g., 90.0%
	DiskPercent   float64   `json:"disk_percent"` // e.g., 90.0%
	Enabled       bool      `json:"enabled"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AlertLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Timestamp time.Time `gorm:"index;not null" json:"timestamp"`
	Type      string    `json:"type"`      // "CPU", "MEMORY", "DISK"
	Level     string    `json:"level"`     // "WARNING", "CRITICAL"
	Value     float64   `json:"value"`     // e.g. 94.5%
	Threshold float64   `json:"threshold"` // e.g. 90.0%
	Message   string    `json:"message"`
}

package models

import "time"

// MetricRecord stores time-series data in SQLite for historical charts
type MetricRecord struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	Timestamp       time.Time `gorm:"index;not null" json:"timestamp"`
	CPUPercent      float64   `json:"cpu_percent"`
	MemTotal        uint64    `json:"mem_total"`
	MemUsed         uint64    `json:"mem_used"`
	MemUsedPercent  float64   `json:"mem_used_percent"`
	DiskTotal       uint64    `json:"disk_total"`
	DiskUsed        uint64    `json:"disk_used"`
	DiskPercent     float64   `json:"disk_percent"`
	NetBytesSent    uint64    `json:"net_bytes_sent"`
	NetBytesRecv    uint64    `json:"net_bytes_recv"`
	NetUploadRate   float64   `json:"net_upload_rate"`   // bytes/sec
	NetDownloadRate float64   `json:"net_download_rate"` // bytes/sec
}

// HostInfo contains static hardware, CPU details and OS details
type HostInfo struct {
	Hostname         string  `json:"hostname"`
	OS               string  `json:"os"`
	Platform         string  `json:"platform"`
	PlatformFamily   string  `json:"platform_family"`
	PlatformVersion  string  `json:"platform_version"`
	KernelVersion    string  `json:"kernel_version"`
	KernelArch       string  `json:"kernel_arch"`
	Uptime           uint64  `json:"uptime"`
	BootTime         uint64  `json:"boot_time"`
	CPUCores         int     `json:"cpu_cores"`          // Tổng số luồng logic (Logical Cores / Threads)
	CPUPhysicalCores int     `json:"cpu_physical_cores"` // Số nhân vật lý (Physical Cores)
	CPULogicalCores  int     `json:"cpu_logical_cores"`  // Số luồng (Logical Cores / Threads)
	CPUMhz           float64 `json:"cpu_mhz"`            // Tốc độ xung nhịp MHz (VD: 3400.0)
	CPUSpeedGHz      float64 `json:"cpu_speed_ghz"`      // Tốc độ xung nhịp GHz (VD: 3.40)
	CPUModel         string  `json:"cpu_model"`          // Tên vi xử lý (VD: Intel Core i7-14700K)
}

// PartitionStat represents single disk partition usage
type PartitionStat struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Fstype      string  `json:"fstype"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

// ProcessInfo represents running process details
type ProcessInfo struct {
	PID           int32   `json:"pid"`
	Name          string  `json:"name"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float32 `json:"memory_percent"`
	MemoryRSS     uint64  `json:"memory_rss"`
	Status        string  `json:"status"`
}

// RealtimeMetric is the full payload sent over WebSocket and current metric endpoint
type RealtimeMetric struct {
	Timestamp       time.Time       `json:"timestamp"`
	HostInfo        HostInfo        `json:"host_info"`
	CPUPercent      float64         `json:"cpu_percent"`
	CPUCoresPercent []float64       `json:"cpu_cores_percent"`
	MemTotal        uint64          `json:"mem_total"`
	MemUsed         uint64          `json:"mem_used"`
	MemFree         uint64          `json:"mem_free"`
	MemUsedPercent  float64         `json:"mem_used_percent"`
	SwapTotal       uint64          `json:"swap_total"`
	SwapUsed        uint64          `json:"swap_used"`
	SwapPercent     float64         `json:"swap_percent"`
	Partitions      []PartitionStat `json:"partitions"`
	NetBytesSent    uint64          `json:"net_bytes_sent"`
	NetBytesRecv    uint64          `json:"net_bytes_recv"`
	NetUploadRate   float64         `json:"net_upload_rate"`
	NetDownloadRate float64         `json:"net_download_rate"`
	TopProcesses    []ProcessInfo   `json:"top_processes"`
}

// MetricSummary provides statistical aggregates (Min, Max, Avg) over a time period
type MetricSummary struct {
	TimeRange       string    `json:"time_range"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	SampleCount     int       `json:"sample_count"`
	AvgCPU          float64   `json:"avg_cpu"`
	MinCPU          float64   `json:"min_cpu"`
	MaxCPU          float64   `json:"max_cpu"`
	AvgMemPercent   float64   `json:"avg_mem_percent"`
	MinMemPercent   float64   `json:"min_mem_percent"`
	MaxMemPercent   float64   `json:"max_mem_percent"`
	AvgDiskPercent  float64   `json:"avg_disk_percent"`
	MaxDiskPercent  float64   `json:"max_disk_percent"`
	AvgUploadRate   float64   `json:"avg_upload_rate"`
	MaxUploadRate   float64   `json:"max_upload_rate"`
	AvgDownloadRate float64   `json:"avg_download_rate"`
	MaxDownloadRate float64   `json:"max_download_rate"`
}

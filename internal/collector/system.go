package collector

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"SysDashServer/internal/models"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type SystemCollector struct {
	mu           sync.Mutex
	lastNetSent  uint64
	lastNetRecv  uint64
	lastNetCheck time.Time
	cachedHost   *models.HostInfo
}

func NewSystemCollector() *SystemCollector {
	return &SystemCollector{
		lastNetCheck: time.Now(),
	}
}

// GetHostInfo retrieves static host information (safe for external callers)
func (c *SystemCollector) GetHostInfo() models.HostInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getHostInfoLocked()
}

func (c *SystemCollector) getHostInfoLocked() models.HostInfo {
	if c.cachedHost != nil {
		if h, err := host.Info(); err == nil {
			c.cachedHost.Uptime = h.Uptime
		}
		return *c.cachedHost
	}

	info := models.HostInfo{}
	if h, err := host.Info(); err == nil {
		info.Hostname = h.Hostname
		info.OS = h.OS
		info.Platform = h.Platform
		info.PlatformFamily = h.PlatformFamily
		info.PlatformVersion = h.PlatformVersion
		info.KernelVersion = h.KernelVersion
		info.KernelArch = h.KernelArch
		info.Uptime = h.Uptime
		info.BootTime = h.BootTime
	}

	// 1. Physical cores (số nhân)
	if physical, err := cpu.Counts(false); err == nil && physical > 0 {
		info.CPUPhysicalCores = physical
	}

	// 2. Logical cores/threads (số luồng)
	if logical, err := cpu.Counts(true); err == nil && logical > 0 {
		info.CPULogicalCores = logical
		info.CPUCores = logical
	}

	// 3. CPU Model & Speed (MHz / GHz)
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CPUModel = cpuInfo[0].ModelName
		info.CPUMhz = math.Round(cpuInfo[0].Mhz*100) / 100
		info.CPUSpeedGHz = math.Round((cpuInfo[0].Mhz/1000.0)*100) / 100

		if info.CPUCores == 0 {
			info.CPUCores = int(cpuInfo[0].Cores)
		}
		if info.CPUPhysicalCores == 0 {
			info.CPUPhysicalCores = int(cpuInfo[0].Cores)
		}
	}

	c.cachedHost = &info
	return info
}

// Collect gathers current real-time metrics across CPU, RAM, Disk, Network and Processes
func (c *SystemCollector) Collect() models.RealtimeMetric {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	metric := models.RealtimeMetric{
		Timestamp: now,
		HostInfo:  c.getHostInfoLocked(),
	}

	// 1. CPU Percent (non-blocking)
	if overall, err := cpu.Percent(0, false); err == nil && len(overall) > 0 {
		metric.CPUPercent = overall[0]
	}
	if perCore, err := cpu.Percent(0, true); err == nil {
		metric.CPUCoresPercent = perCore
	}

	// 2. Memory
	if vm, err := mem.VirtualMemory(); err == nil {
		metric.MemTotal = vm.Total
		metric.MemUsed = vm.Used
		metric.MemFree = vm.Free
		metric.MemUsedPercent = vm.UsedPercent
	}
	if swap, err := mem.SwapMemory(); err == nil {
		metric.SwapTotal = swap.Total
		metric.SwapUsed = swap.Used
		metric.SwapPercent = swap.UsedPercent
	}

	// 3. Disk Partitions
	if partitions, err := disk.Partitions(false); err == nil {
		var parts []models.PartitionStat
		for _, p := range partitions {
			usage, err := disk.Usage(p.Mountpoint)
			if err != nil || usage.Total == 0 {
				continue
			}
			parts = append(parts, models.PartitionStat{
				Device:      p.Device,
				Mountpoint:  p.Mountpoint,
				Fstype:      p.Fstype,
				Total:       usage.Total,
				Used:        usage.Used,
				Free:        usage.Free,
				UsedPercent: usage.UsedPercent,
			})
		}
		metric.Partitions = parts
	}

	// 4. Network Rates & Totals
	if ioCounters, err := net.IOCounters(false); err == nil && len(ioCounters) > 0 {
		total := ioCounters[0]
		metric.NetBytesSent = total.BytesSent
		metric.NetBytesRecv = total.BytesRecv

		duration := now.Sub(c.lastNetCheck).Seconds()
		if duration > 0 && c.lastNetSent > 0 && c.lastNetRecv > 0 {
			metric.NetUploadRate = float64(total.BytesSent-c.lastNetSent) / duration
			metric.NetDownloadRate = float64(total.BytesRecv-c.lastNetRecv) / duration
			if metric.NetUploadRate < 0 {
				metric.NetUploadRate = 0
			}
			if metric.NetDownloadRate < 0 {
				metric.NetDownloadRate = 0
			}
		}
		c.lastNetSent = total.BytesSent
		c.lastNetRecv = total.BytesRecv
		c.lastNetCheck = now
	}

	// 5. Top 5 Processes (safe collector)
	metric.TopProcesses = c.GetProcesses("", "cpu", "desc", 5)

	return metric
}

// GetProcesses returns filtered, sorted and paginated process list
func (c *SystemCollector) GetProcesses(search, sortBy, order string, limit int) []models.ProcessInfo {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}

	searchLower := strings.ToLower(strings.TrimSpace(search))

	var list []models.ProcessInfo
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || name == "" {
			continue
		}

		pidStr := strconv.Itoa(int(p.Pid))
		if searchLower != "" {
			if !strings.Contains(strings.ToLower(name), searchLower) && !strings.Contains(pidStr, searchLower) {
				continue
			}
		}

		cpuPct, _ := p.CPUPercent()
		memPct, _ := p.MemoryPercent()
		memInfo, _ := p.MemoryInfo()
		var rss uint64
		if memInfo != nil {
			rss = memInfo.RSS
		}

		status, _ := p.Status()
		var statusStr string
		if len(status) > 0 {
			statusStr = status[0]
		}

		list = append(list, models.ProcessInfo{
			PID:           p.Pid,
			Name:          name,
			CPUPercent:    cpuPct,
			MemoryPercent: memPct,
			MemoryRSS:     rss,
			Status:        statusStr,
		})
	}

	isAsc := strings.ToLower(order) == "asc"

	// Sorting
	sort.Slice(list, func(i, j int) bool {
		var less bool
		switch strings.ToLower(sortBy) {
		case "mem", "memory":
			less = list[i].MemoryPercent < list[j].MemoryPercent
		case "rss":
			less = list[i].MemoryRSS < list[j].MemoryRSS
		case "name":
			less = strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		case "pid":
			less = list[i].PID < list[j].PID
		case "cpu":
			fallthrough
		default:
			less = list[i].CPUPercent < list[j].CPUPercent
		}

		if isAsc {
			return less
		}
		return !less
	})

	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}

	return list
}

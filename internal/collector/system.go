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

	// CPU times delta tracking for rock-solid accuracy on Linux & Windows
	lastCPUTotal  float64
	lastCPUBusy   float64
	lastCoreTimes []cpu.TimesStat
}

func NewSystemCollector() *SystemCollector {
	c := &SystemCollector{
		lastNetCheck: time.Now(),
	}
	// Initial CPU times sampling to prime the delta calculator
	if times, err := cpu.Times(false); err == nil && len(times) > 0 {
		c.lastCPUBusy, c.lastCPUTotal = calculateCPUTimes(times[0])
	}
	if coreTimes, err := cpu.Times(true); err == nil {
		c.lastCoreTimes = coreTimes
	}
	return c
}

func calculateCPUTimes(t cpu.TimesStat) (busy, total float64) {
	// Busy = User + System + Nice + Irq + Softirq + Steal
	busy = t.User + t.System + t.Nice + t.Irq + t.Softirq + t.Steal
	total = busy + t.Idle + t.Iowait
	return busy, total
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

	// 1. Cores detection (with VPS/KVM fallback)
	if logical, err := cpu.Counts(true); err == nil && logical > 0 {
		info.CPULogicalCores = logical
		info.CPUCores = logical
	}
	if physical, err := cpu.Counts(false); err == nil && physical > 0 {
		info.CPUPhysicalCores = physical
	} else if info.CPULogicalCores > 0 {
		// On VPS (KVM/QEMU/LXC), hypervisor might not expose socket topology
		info.CPUPhysicalCores = info.CPULogicalCores
	}

	// 2. CPU Model & Speed (MHz / GHz)
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

	// Final fallback for cores if still 0
	if info.CPUCores == 0 {
		info.CPUCores = 1
	}
	if info.CPUPhysicalCores == 0 {
		info.CPUPhysicalCores = info.CPUCores
	}
	if info.CPULogicalCores == 0 {
		info.CPULogicalCores = info.CPUCores
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

	// 1. CPU Percent via Times delta (accurate across Linux VPS, Containers, Windows)
	if times, err := cpu.Times(false); err == nil && len(times) > 0 {
		busy, total := calculateCPUTimes(times[0])
		deltaTotal := total - c.lastCPUTotal
		deltaBusy := busy - c.lastCPUBusy

		if deltaTotal > 0 && c.lastCPUTotal > 0 {
			pct := (deltaBusy / deltaTotal) * 100.0
			if pct < 0 {
				pct = 0
			} else if pct > 100 {
				pct = 100
			}
			metric.CPUPercent = math.Round(pct*100) / 100
		}
		c.lastCPUBusy = busy
		c.lastCPUTotal = total
	}

	// Per-core CPU % calculation
	if coreTimes, err := cpu.Times(true); err == nil {
		if len(c.lastCoreTimes) == len(coreTimes) {
			perCore := make([]float64, len(coreTimes))
			for i := range coreTimes {
				busy, total := calculateCPUTimes(coreTimes[i])
				lastBusy, lastTotal := calculateCPUTimes(c.lastCoreTimes[i])
				deltaTotal := total - lastTotal
				deltaBusy := busy - lastBusy
				if deltaTotal > 0 {
					pct := (deltaBusy / deltaTotal) * 100.0
					if pct < 0 {
						pct = 0
					} else if pct > 100 {
						pct = 100
					}
					perCore[i] = math.Round(pct*100) / 100
				}
			}
			metric.CPUCoresPercent = perCore
		}
		c.lastCoreTimes = coreTimes
	}

	// 2. Memory (Accurate calculation for Linux VPS: Used = Total - Available)
	// Linux uses free RAM for buffers/page cache. True used RAM ignores buffers/cache (like htop & free -m).
	if vm, err := mem.VirtualMemory(); err == nil {
		metric.MemTotal = vm.Total

		var used uint64
		var usedPct float64

		if vm.Available > 0 && vm.Available <= vm.Total {
			used = vm.Total - vm.Available
			usedPct = (float64(used) / float64(vm.Total)) * 100.0
			metric.MemFree = vm.Available
		} else {
			used = vm.Used
			usedPct = vm.UsedPercent
			metric.MemFree = vm.Free
		}

		metric.MemUsed = used
		metric.MemUsedPercent = math.Round(usedPct*100) / 100
	}

	if swap, err := mem.SwapMemory(); err == nil {
		metric.SwapTotal = swap.Total
		metric.SwapUsed = swap.Used
		metric.SwapPercent = math.Round(swap.UsedPercent*100) / 100
	}

	// 3. Disk Partitions (Filter out pseudo, virtual, snap squashfs and tmpfs mounts)
	if partitions, err := disk.Partitions(false); err == nil {
		var parts []models.PartitionStat
		seenMounts := make(map[string]bool)

		for _, p := range partitions {
			if isVirtualFS(p.Fstype, p.Mountpoint) {
				continue
			}
			if seenMounts[p.Mountpoint] {
				continue
			}

			usage, err := disk.Usage(p.Mountpoint)
			if err != nil || usage.Total == 0 {
				continue
			}

			seenMounts[p.Mountpoint] = true
			parts = append(parts, models.PartitionStat{
				Device:      p.Device,
				Mountpoint:  p.Mountpoint,
				Fstype:      p.Fstype,
				Total:       usage.Total,
				Used:        usage.Used,
				Free:        usage.Free,
				UsedPercent: math.Round(usage.UsedPercent*100) / 100,
			})
		}

		// Sort so Root "/" (Linux) or "C:" (Windows) is ALWAYS the first partition
		sort.Slice(parts, func(i, j int) bool {
			if parts[i].Mountpoint == "/" || strings.EqualFold(parts[i].Mountpoint, "C:") {
				return true
			}
			if parts[j].Mountpoint == "/" || strings.EqualFold(parts[j].Mountpoint, "C:") {
				return false
			}
			return parts[i].Mountpoint < parts[j].Mountpoint
		})

		metric.Partitions = parts
	}

	// 4. Network Rates (Exclude loopback 'lo' and internal virtual interfaces on Linux)
	if ifaces, err := net.IOCounters(true); err == nil && len(ifaces) > 0 {
		var totalSent, totalRecv uint64
		for _, iface := range ifaces {
			name := strings.ToLower(iface.Name)
			// Ignore loopback, docker virtual bridges, veth, podman, etc.
			if name == "lo" || strings.HasPrefix(name, "loopback") ||
				name == "docker0" || strings.HasPrefix(name, "veth") ||
				strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "virbr") {
				continue
			}
			totalSent += iface.BytesSent
			totalRecv += iface.BytesRecv
		}

		// Fallback if all interfaces were skipped
		if totalSent == 0 && totalRecv == 0 && len(ifaces) > 0 {
			totalSent = ifaces[0].BytesSent
			totalRecv = ifaces[0].BytesRecv
		}

		metric.NetBytesSent = totalSent
		metric.NetBytesRecv = totalRecv

		duration := now.Sub(c.lastNetCheck).Seconds()
		if duration > 0 && c.lastNetSent > 0 && c.lastNetRecv > 0 {
			if totalSent >= c.lastNetSent {
				metric.NetUploadRate = math.Round((float64(totalSent-c.lastNetSent)/duration)*100) / 100
			}
			if totalRecv >= c.lastNetRecv {
				metric.NetDownloadRate = math.Round((float64(totalRecv-c.lastNetRecv)/duration)*100) / 100
			}
		}
		c.lastNetSent = totalSent
		c.lastNetRecv = totalRecv
		c.lastNetCheck = now
	}

	// 5. Top 5 Processes (safe collector)
	metric.TopProcesses = c.GetProcesses("", "cpu", "desc", 5)

	return metric
}

// isVirtualFS checks if the filesystem is virtual/pseudo on Linux/Windows
func isVirtualFS(fstype, mountpoint string) bool {
	fs := strings.ToLower(fstype)
	mount := strings.ToLower(mountpoint)

	// Ignore Linux pseudo filesystems, RAM disks, Docker containers, Snap packages
	switch fs {
	case "squashfs", "tmpfs", "devtmpfs", "overlay", "cgroup", "cgroup2",
		"proc", "sysfs", "nsfs", "mqueue", "hugetlbfs", "autofs",
		"binfmt_misc", "tracefs", "debugfs", "fuse.lxcfs":
		return true
	}

	// Ignore common virtual mount paths on Ubuntu
	if strings.HasPrefix(mount, "/snap") ||
		strings.HasPrefix(mount, "/sys") ||
		strings.HasPrefix(mount, "/proc") ||
		strings.HasPrefix(mount, "/dev") ||
		strings.HasPrefix(mount, "/run") {
		return true
	}

	return false
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
			CPUPercent:    math.Round(cpuPct*100) / 100,
			MemoryPercent: float32(math.Round(float64(memPct)*100) / 100),
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

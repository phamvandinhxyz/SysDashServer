package collector

import (
	"math"
	"os"
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

	// CPU times delta tracking for rock-solid accuracy matching htop
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

// calculateCPUTimes computes busy and total CPU ticks matching htop:
// In htop, default CPU percentage reflects active CPU execution (User + Nice + System).
// Iowait is CPU IDLE waiting for I/O (shown in gray in htop, not counted as busy CPU load).
// Steal is hypervisor cycles taken by other VMs (shown separately in htop).
func calculateCPUTimes(t cpu.TimesStat) (busy, total float64) {
	busy = t.User + t.System + t.Nice + t.Irq + t.Softirq
	total = busy + t.Idle + t.Iowait + t.Steal + t.Guest + t.GuestNice
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

	// 1. CPU Percent via Times delta (matching htop)
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

	// 2. Memory (Exact htop & free -m formula)
	// In htop: Used = MemTotal - MemFree - Buffers - Cached
	// (accounting for Sreclaimable and Shared reported in /proc/meminfo)
	if vm, err := mem.VirtualMemory(); err == nil {
		metric.MemTotal = vm.Total

		var used uint64
		if vm.Buffers > 0 || vm.Cached > 0 {
			cacheAndBuffers := vm.Buffers + vm.Cached
			if vm.Sreclaimable > 0 {
				cacheAndBuffers += vm.Sreclaimable
			}
			if vm.Shared > 0 && cacheAndBuffers >= vm.Shared {
				cacheAndBuffers -= vm.Shared
			}

			if vm.Total >= (vm.Free + cacheAndBuffers) {
				used = vm.Total - vm.Free - cacheAndBuffers
			} else if vm.Available > 0 && vm.Available <= vm.Total {
				used = vm.Total - vm.Available
			} else {
				used = vm.Used
			}
		} else {
			// Windows / non-Linux fallback
			if vm.Used > 0 && vm.Used <= vm.Total {
				used = vm.Used
			} else {
				used = vm.Total - vm.Free
			}
		}

		if used > vm.Total {
			used = vm.Total
		}

		metric.MemUsed = used
		metric.MemFree = vm.Total - used
		usedPct := (float64(used) / float64(vm.Total)) * 100.0
		metric.MemUsedPercent = math.Round(usedPct*100) / 100
	}

	if swap, err := mem.SwapMemory(); err == nil {
		metric.SwapTotal = swap.Total
		metric.SwapUsed = swap.Used
		metric.SwapPercent = math.Round(swap.UsedPercent*100) / 100
	}

	// 3. Disk Partitions (Accurate host measurement & matching df -h)
	// Check if running in a container with host root mounted (e.g. -v /:/host:ro)
	hostRoot := os.Getenv("HOST_ROOT")
	if hostRoot == "" {
		if _, err := os.Stat("/host"); err == nil {
			hostRoot = "/host"
		}
	}

	if partitions, err := disk.Partitions(false); err == nil {
		var parts []models.PartitionStat
		seenDisplayMounts := make(map[string]bool)

		for _, p := range partitions {
			// If running inside container with hostRoot mounted:
			if hostRoot != "" {
				// Ignore container's internal rootfs overlay
				if p.Mountpoint == "/" && strings.EqualFold(p.Fstype, "overlay") {
					continue
				}
				// Only accept mounts from the host
				if p.Mountpoint != hostRoot && !strings.HasPrefix(p.Mountpoint, hostRoot+"/") {
					continue
				}
			}

			if isVirtualFS(p.Fstype, p.Mountpoint) {
				continue
			}

			usage, err := disk.Usage(p.Mountpoint)
			if err != nil || usage.Total == 0 {
				continue
			}

			displayMount := p.Mountpoint
			// Remap /host to "/" for dashboard display
			if hostRoot != "" {
				if displayMount == hostRoot {
					displayMount = "/"
				} else if strings.HasPrefix(displayMount, hostRoot+"/") {
					displayMount = strings.TrimPrefix(displayMount, hostRoot)
				}
			}

			if seenDisplayMounts[displayMount] {
				continue
			}
			seenDisplayMounts[displayMount] = true

			// Calculate used percent matching df -h: Use% = Used / (Used + Free) * 100
			var usedPct float64
			if usage.Used+usage.Free > 0 {
				usedPct = (float64(usage.Used) / float64(usage.Used+usage.Free)) * 100.0
			} else if usage.Total > 0 {
				usedPct = (float64(usage.Used) / float64(usage.Total)) * 100.0
			}

			parts = append(parts, models.PartitionStat{
				Device:      p.Device,
				Mountpoint:  displayMount,
				Fstype:      p.Fstype,
				Total:       usage.Total,
				Used:        usage.Used,
				Free:        usage.Free,
				UsedPercent: math.Round(usedPct*100) / 100,
			})
		}

		// Fallback: If no partition detected, measure hostRoot or "/" directly
		if len(parts) == 0 {
			targetMount := "/"
			if hostRoot != "" {
				targetMount = hostRoot
			}
			if usage, err := disk.Usage(targetMount); err == nil && usage.Total > 0 {
				var usedPct float64
				if usage.Used+usage.Free > 0 {
					usedPct = (float64(usage.Used) / float64(usage.Used+usage.Free)) * 100.0
				} else {
					usedPct = (float64(usage.Used) / float64(usage.Total)) * 100.0
				}
				parts = append(parts, models.PartitionStat{
					Device:      "root",
					Mountpoint:  "/",
					Fstype:      "ext4",
					Total:       usage.Total,
					Used:        usage.Used,
					Free:        usage.Free,
					UsedPercent: math.Round(usedPct*100) / 100,
				})
			}
		}

		// Sort so Root "/" (Linux) or "C:" (Windows) is ALWAYS index 0
		sort.Slice(parts, func(i, j int) bool {
			isRootI := parts[i].Mountpoint == "/" || strings.EqualFold(parts[i].Mountpoint, "C:")
			isRootJ := parts[j].Mountpoint == "/" || strings.EqualFold(parts[j].Mountpoint, "C:")
			if isRootI && !isRootJ {
				return true
			}
			if !isRootI && isRootJ {
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

	// Ignore Linux pseudo filesystems, RAM disks, Snap packages
	switch fs {
	case "squashfs", "tmpfs", "devtmpfs", "cgroup", "cgroup2",
		"proc", "sysfs", "nsfs", "mqueue", "hugetlbfs", "autofs",
		"binfmt_misc", "tracefs", "debugfs", "fuse.lxcfs", "rpc_pipefs":
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

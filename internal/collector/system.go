package collector

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"runtime"
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

	// CPU times delta tracking (matching htop)
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

// calculateCPUTimes computes busy and total CPU ticks matching htop.
//   - Iowait: CPU is idle waiting for I/O -> not busy.
//   - Steal: time stolen by hypervisor -> counted in total, not busy.
//   - Guest/GuestNice are ALREADY included in User/Nice by the Linux kernel,
//     so they must NOT be added again (otherwise total is inflated).
func calculateCPUTimes(t cpu.TimesStat) (busy, total float64) {
	busy = t.User + t.System + t.Nice + t.Irq + t.Softirq
	total = busy + t.Idle + t.Iowait + t.Steal
	return busy, total
}

// hostProcPath returns the procfs root used for reading host data (HOST_PROC or /proc)
func hostProcPath() string {
	if p := os.Getenv("HOST_PROC"); p != "" {
		return strings.TrimRight(p, "/")
	}
	return "/proc"
}

// detectHostRoot returns the directory where the host "/" is mounted when running in Docker
// (e.g. "-v /:/host:ro" -> "/host"). Returns "" when running directly on the host.
func detectHostRoot() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	hr := strings.TrimRight(os.Getenv("HOST_ROOT"), "/")
	if hr == "" {
		if fi, err := os.Stat("/host"); err == nil && fi.IsDir() {
			// Only treat /host as host root if it really looks like a root filesystem
			if _, err := os.Stat("/host/etc"); err == nil {
				hr = "/host"
			}
		}
	}
	return hr
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
			metric.CPUPercent = clampPct(deltaBusy / deltaTotal * 100.0)
		}
		c.lastCPUBusy = busy
		c.lastCPUTotal = total
	}

	// Per-core CPU %
	if coreTimes, err := cpu.Times(true); err == nil {
		if len(c.lastCoreTimes) == len(coreTimes) {
			perCore := make([]float64, len(coreTimes))
			for i := range coreTimes {
				busy, total := calculateCPUTimes(coreTimes[i])
				lastBusy, lastTotal := calculateCPUTimes(c.lastCoreTimes[i])
				if dt := total - lastTotal; dt > 0 {
					perCore[i] = clampPct((busy - lastBusy) / dt * 100.0)
				}
			}
			metric.CPUCoresPercent = perCore
		}
		c.lastCoreTimes = coreTimes
	}

	// 2. Memory — same formula as `free` / htop:
	//    used = MemTotal - MemFree - Buffers - (Cached + SReclaimable)
	//    NOTE: gopsutil's vm.Cached ALREADY includes SReclaimable on Linux,
	//    and gopsutil's vm.Used is (Total - Available) which is LARGER than htop.
	if vm, err := mem.VirtualMemory(); err == nil && vm.Total > 0 {
		used, buffCache := computeMemUsed(vm)

		metric.MemTotal = vm.Total
		metric.MemUsed = used
		metric.MemFree = vm.Total - used
		metric.MemAvailable = vm.Available
		metric.MemBuffCache = buffCache
		metric.MemUsedPercent = math.Round(float64(used)/float64(vm.Total)*10000) / 100
		metric.MemTotalFormatted = FormatBinary(vm.Total)
		metric.MemUsedFormatted = FormatBinary(used)
	}

	if swap, err := mem.SwapMemory(); err == nil {
		metric.SwapTotal = swap.Total
		metric.SwapUsed = swap.Used
		metric.SwapPercent = math.Round(swap.UsedPercent*100) / 100
	}

	// 3. Disk partitions (same numbers as `df` on the host)
	metric.Partitions, _ = collectPartitions(false)

	// 4. Network rates (exclude loopback & docker virtual interfaces)
	if ifaces, err := net.IOCounters(true); err == nil && len(ifaces) > 0 {
		var totalSent, totalRecv uint64
		for _, iface := range ifaces {
			name := strings.ToLower(iface.Name)
			if name == "lo" || strings.HasPrefix(name, "loopback") ||
				name == "docker0" || strings.HasPrefix(name, "veth") ||
				strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "virbr") {
				continue
			}
			totalSent += iface.BytesSent
			totalRecv += iface.BytesRecv
		}

		if totalSent == 0 && totalRecv == 0 {
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

	// 5. Top 5 processes
	metric.TopProcesses = c.GetProcesses("", "cpu", "desc", 5)

	return metric
}

func clampPct(p float64) float64 {
	if p < 0 {
		p = 0
	} else if p > 100 {
		p = 100
	}
	return math.Round(p*100) / 100
}

// computeMemUsed returns used memory and buff/cache exactly like `free -b`
func computeMemUsed(vm *mem.VirtualMemoryStat) (used, buffCache uint64) {
	if runtime.GOOS != "linux" {
		// Windows / macOS: gopsutil already returns the OS-native "in use" value
		return vm.Used, 0
	}
	buffCache = vm.Buffers + vm.Cached // vm.Cached = Cached + SReclaimable
	if vm.Total > vm.Free+buffCache {
		return vm.Total - vm.Free - buffCache, buffCache
	}
	if vm.Available > 0 && vm.Available <= vm.Total {
		return vm.Total - vm.Available, buffCache
	}
	return vm.Total - vm.Free, buffCache
}

// PartitionDiag explains why a mount was accepted / skipped (used by /system/diagnostics)
type PartitionDiag struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint"`
	Fstype     string `json:"fstype"`
	StatPath   string `json:"stat_path"`
	Display    string `json:"display"`
	Decision   string `json:"decision"`
	Total      uint64 `json:"total,omitempty"`
	Used       uint64 `json:"used,omitempty"`
	Free       uint64 `json:"free,omitempty"`
}

var realLinuxFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "fuseblk": true, "f2fs": true,
	"jfs": true, "reiserfs": true, "nfs": true, "nfs4": true, "cifs": true,
}

// collectPartitions returns real disk partitions with the same numbers as `df` on the host.
//
// When running in Docker with "-v /:/host:ro":
//   - the container "/" (overlay) is ignored,
//   - "/host"            -> displayed as "/"
//   - "/host/boot/efi"   -> displayed as "/boot/efi"
//   - host mounts read from /host/proc/1/mountinfo are statfs'ed through "/host<mountpoint>"
//   - bind-mounts of the same device (/etc/hosts, /app/sysdash.db, ...) are de-duplicated.
func collectPartitions(withDiag bool) ([]models.PartitionStat, []PartitionDiag) {
	hostRoot := detectHostRoot()
	isLinux := runtime.GOOS == "linux"

	var diags []PartitionDiag
	note := func(d PartitionDiag) {
		if withDiag {
			diags = append(diags, d)
		}
	}

	type entry struct {
		part    models.PartitionStat
		display string
	}
	byDevice := make(map[string]*entry)
	var order []string

	partitions, _ := disk.Partitions(false)
	for _, p := range partitions {
		d := PartitionDiag{Device: p.Device, Mountpoint: p.Mountpoint, Fstype: p.Fstype}
		fs := strings.ToLower(p.Fstype)

		display := p.Mountpoint
		statPath := p.Mountpoint
		if hostRoot != "" {
			switch {
			case p.Mountpoint == hostRoot:
				display, statPath = "/", hostRoot
			case strings.HasPrefix(p.Mountpoint, hostRoot+"/"):
				display = strings.TrimPrefix(p.Mountpoint, hostRoot)
			case p.Mountpoint == "/" && (fs == "overlay" || fs == "aufs"):
				d.Decision = "skip: container overlay root"
				note(d)
				continue
			default:
				// Mount path seen from the host namespace (or a container bind mount):
				// statfs it through the host root mount.
				statPath = filepath.Join(hostRoot, p.Mountpoint)
			}
		}
		d.Display, d.StatPath = display, statPath

		if isVirtualFS(fs, display) {
			d.Decision = "skip: virtual/pseudo filesystem"
			note(d)
			continue
		}
		if isLinux && !strings.HasPrefix(p.Device, "/dev/") && !realLinuxFS[fs] {
			d.Decision = "skip: not a block device"
			note(d)
			continue
		}

		usage, err := disk.Usage(statPath)
		if err != nil || usage.Total == 0 {
			d.Decision = "skip: statfs failed"
			note(d)
			continue
		}

		ps := buildPartitionStat(p.Device, display, p.Fstype, usage)
		d.Total, d.Used, d.Free = ps.Total, ps.Used, ps.Free

		key := p.Device
		if !isLinux || key == "" {
			key = display
		}
		if existing, ok := byDevice[key]; ok {
			// Same device mounted several times (bind mounts): keep the shortest path ("/" wins)
			if len(display) < len(existing.display) {
				existing.part = ps
				existing.display = display
				d.Decision = "accept (replaces longer duplicate)"
			} else {
				d.Decision = "skip: duplicate device"
			}
			note(d)
			continue
		}
		byDevice[key] = &entry{part: ps, display: display}
		order = append(order, key)
		d.Decision = "accept"
		note(d)
	}

	parts := make([]models.PartitionStat, 0, len(order))
	for _, k := range order {
		parts = append(parts, byDevice[k].part)
	}

	// Make sure root "/" is present (statfs host root directly)
	hasRoot := false
	for _, p := range parts {
		if p.Mountpoint == "/" || strings.EqualFold(p.Mountpoint, "C:") {
			hasRoot = true
			break
		}
	}
	if !hasRoot && isLinux {
		target := "/"
		if hostRoot != "" {
			target = hostRoot
		}
		if usage, err := disk.Usage(target); err == nil && usage.Total > 0 {
			parts = append(parts, buildPartitionStat("rootfs", "/", usage.Fstype, usage))
			note(PartitionDiag{Device: "rootfs", Mountpoint: target, StatPath: target, Display: "/",
				Fstype: usage.Fstype, Decision: "accept (fallback root)", Total: usage.Total, Used: usage.Used, Free: usage.Free})
		}
	}

	sort.SliceStable(parts, func(i, j int) bool {
		ri := parts[i].Mountpoint == "/" || strings.EqualFold(parts[i].Mountpoint, "C:")
		rj := parts[j].Mountpoint == "/" || strings.EqualFold(parts[j].Mountpoint, "C:")
		if ri != rj {
			return ri
		}
		return parts[i].Mountpoint < parts[j].Mountpoint
	})

	return parts, diags
}

func buildPartitionStat(device, mount, fstype string, u *disk.UsageStat) models.PartitionStat {
	// df: Use% = Used / (Used + Avail)  (root-reserved blocks excluded)
	var pct float64
	if u.Used+u.Free > 0 {
		pct = float64(u.Used) / float64(u.Used+u.Free) * 100
	}
	return models.PartitionStat{
		Device:         device,
		Mountpoint:     mount,
		Fstype:         fstype,
		Total:          u.Total,
		Used:           u.Used,
		Free:           u.Free,
		UsedPercent:    math.Round(pct*100) / 100,
		TotalFormatted: FormatBinary(u.Total),
		UsedFormatted:  FormatBinary(u.Used),
		FreeFormatted:  FormatBinary(u.Free),
	}
}

// isVirtualFS checks if the filesystem is virtual/pseudo (mountpoint is the HOST-relative path)
func isVirtualFS(fstype, mountpoint string) bool {
	fs := strings.ToLower(fstype)
	mount := strings.ToLower(mountpoint)

	switch fs {
	case "squashfs", "tmpfs", "devtmpfs", "cgroup", "cgroup2", "overlay", "aufs",
		"proc", "sysfs", "nsfs", "mqueue", "hugetlbfs", "autofs", "devpts",
		"binfmt_misc", "tracefs", "debugfs", "securityfs", "pstore", "bpf",
		"configfs", "fusectl", "fuse.lxcfs", "rpc_pipefs", "efivarfs", "ramfs":
		return true
	}

	if mount == "/" {
		return false
	}
	for _, prefix := range []string{"/snap", "/sys", "/proc", "/dev", "/run", "/var/lib/docker", "/var/lib/containerd", "/var/snap"} {
		if mount == prefix || strings.HasPrefix(mount, prefix+"/") {
			return true
		}
	}
	return false
}

// Diagnostics returns raw data so the numbers can be compared with `free -b`, `df -B1`, htop.
func (c *SystemCollector) Diagnostics() map[string]interface{} {
	result := map[string]interface{}{
		"goos":      runtime.GOOS,
		"host_root": detectHostRoot(),
		"env": map[string]string{
			"HOST_PROC": os.Getenv("HOST_PROC"),
			"HOST_SYS":  os.Getenv("HOST_SYS"),
			"HOST_ROOT": os.Getenv("HOST_ROOT"),
			"HOST_ETC":  os.Getenv("HOST_ETC"),
		},
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		result["running_in_docker"] = true
	} else {
		result["running_in_docker"] = false
	}

	procRoot := hostProcPath()
	if _, err := os.ReadFile(filepath.Join(procRoot, "1", "mountinfo")); err != nil {
		result["host_mountinfo_readable"] = "no: " + err.Error()
	} else {
		result["host_mountinfo_readable"] = "yes"
	}

	// Raw /proc/meminfo (bytes)
	raw := map[string]uint64{}
	if f, err := os.Open(filepath.Join(procRoot, "meminfo")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 2 {
				continue
			}
			key := strings.TrimSuffix(fields[0], ":")
			switch key {
			case "MemTotal", "MemFree", "MemAvailable", "Buffers", "Cached", "SReclaimable", "Shmem", "SwapTotal", "SwapFree":
				if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					raw[key] = v * 1024
				}
			}
		}
		f.Close()
	}
	result["meminfo_raw_bytes"] = raw

	if vm, err := mem.VirtualMemory(); err == nil {
		used, buffCache := computeMemUsed(vm)
		result["memory_computed"] = map[string]interface{}{
			"total":               vm.Total,
			"used_like_free_htop": used,
			"buff_cache":          buffCache,
			"available":           vm.Available,
			"gopsutil_used":       vm.Used,
			"total_formatted":     FormatBinary(vm.Total),
			"used_formatted":      FormatBinary(used),
			"formula":             "used = MemTotal - MemFree - Buffers - Cached - SReclaimable",
		}
	}

	_, diags := collectPartitions(true)
	result["partitions_scan"] = diags
	return result
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

package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"SysDashServer/internal/models"

	"github.com/shirou/gopsutil/v4/mem"
)

type DockerCollector struct {
	client *http.Client
	host   string
	isUnix bool
}

// Docker "stats?stream=false" waits for 2 samples (~1-2s), so timeouts must be generous
const (
	dockerClientTimeout = 15 * time.Second
	dockerStatsTimeout  = 8 * time.Second
	dockerListTimeout   = 5 * time.Second
)

func NewDockerCollector() *DockerCollector {
	dockerHost := os.Getenv("DOCKER_HOST")
	dockerSocket := os.Getenv("DOCKER_SOCKET")
	var isUnix bool
	var client *http.Client

	unixClient := func(sockPath string, fallback bool) *http.Client {
		return &http.Client{
			Timeout: dockerClientTimeout,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 16,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					conn, err := d.DialContext(ctx, "unix", sockPath)
					if err != nil && fallback && sockPath != "/var/run/docker.sock" {
						return d.DialContext(ctx, "unix", "/var/run/docker.sock")
					}
					return conn, err
				},
			},
		}
	}

	switch {
	case dockerHost == "" && runtime.GOOS == "windows":
		dockerHost = "http://localhost:2375"
		client = &http.Client{Timeout: dockerClientTimeout}
	case dockerHost == "":
		sockPath := "/var/run/docker.sock"
		if dockerSocket != "" {
			sockPath = dockerSocket
		} else if _, err := os.Stat("/run/docker.sock"); err == nil {
			sockPath = "/run/docker.sock"
		}
		dockerHost = "http://localhost"
		isUnix = true
		client = unixClient(sockPath, true)
	case strings.HasPrefix(dockerHost, "unix://"):
		sockPath := strings.TrimPrefix(dockerHost, "unix://")
		dockerHost = "http://localhost"
		isUnix = true
		client = unixClient(sockPath, false)
	default:
		if strings.HasPrefix(dockerHost, "tcp://") {
			dockerHost = "http://" + strings.TrimPrefix(dockerHost, "tcp://")
		}
		client = &http.Client{Timeout: dockerClientTimeout}
	}

	return &DockerCollector{
		client: client,
		host:   dockerHost,
		isUnix: isUnix,
	}
}

func (c *DockerCollector) getJSON(timeout time.Duration, path string, out interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.host+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker engine returned HTTP %d for %s", resp.StatusCode, path)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Ping checks if Docker daemon is accessible
func (c *DockerCollector) Ping() error {
	return c.getJSON(3*time.Second, "/_ping", nil)
}

// Raw struct for decoding Docker Engine /containers/{id}/stats?stream=false
type dockerContainerStatsRaw struct {
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
}

type containerStats struct {
	memUsage uint64
	memLimit uint64
	memPct   float64
	cpuPct   float64
	err      error
}

// getContainerStats returns live RAM matching docker stats and CPU in standard docker stats format (Irix mode: 1 core = 100%)
func (c *DockerCollector) getContainerStats(containerID string, hostMemTotal uint64) containerStats {
	var raw dockerContainerStatsRaw
	if err := c.getJSON(dockerStatsTimeout, "/containers/"+containerID+"/stats?stream=false", &raw); err != nil {
		return containerStats{err: err}
	}

	var s containerStats

	// RAM — docker/cli calculateMemUsageUnixNoCache():
	//   cgroup v1: usage - total_inactive_file
	//   cgroup v2: usage - inactive_file
	//   fallback:  usage - cache
	usage := raw.MemoryStats.Usage
	s.memUsage = usage
	if v, ok := raw.MemoryStats.Stats["total_inactive_file"]; ok && v < usage {
		s.memUsage = usage - v
	} else if v, ok := raw.MemoryStats.Stats["inactive_file"]; ok && v < usage {
		s.memUsage = usage - v
	} else if v, ok := raw.MemoryStats.Stats["cache"]; ok && v < usage {
		s.memUsage = usage - v
	}

	// RAM Limit — docker/cli:
	// If container has no memory limit configured, cgroup v1 reports 9223372036854771712 (~8 EiB).
	// Docker stats substitutes the host system's total physical memory in this case.
	limit := raw.MemoryStats.Limit
	const cgroupV1Unlimited = 9223372036854771712 // 0x7FFFFFFFFFFFF000
	if limit == 0 || limit >= cgroupV1Unlimited || (hostMemTotal > 0 && limit > hostMemTotal) {
		limit = hostMemTotal
	}
	s.memLimit = limit

	if s.memLimit > 0 {
		s.memPct = math.Round(float64(s.memUsage)/float64(s.memLimit)*10000) / 100
	}

	// CPU — docker/cli calculateCPUPercentUnix():
	// Docker CLI computes: (cpuDelta / systemDelta) * onlineCPUs * 100.0 (Irix mode: 1 core = 100%)
	if raw.PreCPUStats.CPUUsage.TotalUsage > 0 && raw.PreCPUStats.SystemCPUUsage > 0 {
		cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
		systemDelta := float64(raw.CPUStats.SystemCPUUsage) - float64(raw.PreCPUStats.SystemCPUUsage)
		onlineCPUs := float64(raw.CPUStats.OnlineCPUs)
		if onlineCPUs == 0 {
			onlineCPUs = float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
		}
		if onlineCPUs == 0 {
			onlineCPUs = float64(runtime.NumCPU())
		}
		if onlineCPUs == 0 {
			onlineCPUs = 1.0
		}

		if systemDelta > 0 && cpuDelta > 0 {
			s.cpuPct = math.Round(((cpuDelta/systemDelta)*onlineCPUs*100.0)*100) / 100
		}
	}

	return s
}

// GetContainers fetches docker containers along with their live RAM & CPU usage
func (c *DockerCollector) GetContainers() ([]models.DockerContainerInfo, error) {
	var raw []struct {
		ID      string            `json:"Id"`
		Names   []string          `json:"Names"`
		Image   string            `json:"Image"`
		ImageID string            `json:"ImageID"`
		Command string            `json:"Command"`
		Created int64             `json:"Created"`
		State   string            `json:"State"`
		Status  string            `json:"Status"`
		Labels  map[string]string `json:"Labels"`
	}
	if err := c.getJSON(dockerListTimeout, "/containers/json?all=true", &raw); err != nil {
		return nil, err
	}

	var hostMemTotal uint64
	if vm, err := mem.VirtualMemory(); err == nil && vm.Total > 0 {
		hostMemTotal = vm.Total
	}

	containers := make([]models.DockerContainerInfo, len(raw))
	stats := make([]containerStats, len(raw))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // limit concurrent stats calls

	for i, item := range raw {
		id := item.ID
		if len(id) > 12 {
			id = id[:12]
		}
		names := make([]string, 0, len(item.Names))
		for _, n := range item.Names {
			names = append(names, strings.TrimPrefix(n, "/"))
		}

		labels := item.Labels
		if labels == nil {
			labels = make(map[string]string)
		}

		containers[i] = models.DockerContainerInfo{
			ID:                id,
			Names:             names,
			Image:             item.Image,
			ImageID:           item.ImageID,
			Command:           item.Command,
			Created:           item.Created,
			State:             item.State,
			Status:            item.Status,
			Labels:            labels,
			MemUsageFormatted: "0 B",
		}

		if strings.EqualFold(item.State, "running") {
			wg.Add(1)
			go func(idx int, fullID string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				stats[idx] = c.getContainerStats(fullID, hostMemTotal)
			}(i, item.ID)
		}
	}
	wg.Wait()

	for i := range containers {
		s := stats[i]
		if s.err != nil || !strings.EqualFold(containers[i].State, "running") {
			continue
		}
		containers[i].MemUsage = s.memUsage
		containers[i].MemLimit = s.memLimit
		containers[i].MemPercent = s.memPct
		containers[i].CPUPercent = s.cpuPct
		// `docker stats` shows memory in 1024-based units (MiB / GiB)
		if s.memLimit > 0 {
			containers[i].MemUsageFormatted = FormatBinary(s.memUsage) + " / " + FormatBinary(s.memLimit)
		} else {
			containers[i].MemUsageFormatted = FormatBinary(s.memUsage)
		}
	}

	return containers, nil
}

// GetImages fetches docker images with disk size and aggregated live RAM of containers using them
func (c *DockerCollector) GetImages() ([]models.DockerImageInfo, error) {
	var raw []struct {
		ID       string   `json:"Id"`
		RepoTags []string `json:"RepoTags"`
		Size     int64    `json:"Size"`
		Created  int64    `json:"Created"`
	}
	if err := c.getJSON(dockerListTimeout, "/images/json", &raw); err != nil {
		return nil, err
	}

	// NOTE: the "Containers" field of /images/json is -1 (not calculated) by default,
	// so we compute container counts & RAM ourselves from the container list.
	containers, _ := c.GetContainers()

	type agg struct {
		total   int64
		running int
		mem     uint64
	}
	byImageID := make(map[string]*agg)
	byRef := make(map[string]*agg) // fallback by image reference (e.g. "nginx:alpine")
	get := func(m map[string]*agg, k string) *agg {
		if m[k] == nil {
			m[k] = &agg{}
		}
		return m[k]
	}

	for _, ct := range containers {
		var a *agg
		if ct.ImageID != "" {
			a = get(byImageID, ct.ImageID)
		} else {
			a = get(byRef, ct.Image)
		}
		a.total++
		if strings.EqualFold(ct.State, "running") {
			a.running++
			a.mem += ct.MemUsage
		}
	}

	images := make([]models.DockerImageInfo, 0, len(raw))
	for _, img := range raw {
		shortID := strings.TrimPrefix(img.ID, "sha256:")
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		tags := img.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}

		a := byImageID[img.ID]
		if a == nil {
			for _, t := range tags {
				if a = byRef[t]; a != nil {
					break
				}
			}
		}
		if a == nil {
			a = &agg{}
		}

		size := uint64(0)
		if img.Size > 0 {
			size = uint64(img.Size)
		}

		images = append(images, models.DockerImageInfo{
			ID:                shortID,
			RepoTags:          tags,
			Size:              img.Size,
			SizeFormatted:     FormatDecimal(size), // same units as `docker images` (SI: MB/GB)
			Created:           img.Created,
			Containers:        a.total,
			RunningContainers: a.running,
			MemUsage:          a.mem,
			MemFormatted:      FormatBinary(a.mem), // same units as `docker stats` (MiB/GiB)
		})
	}

	return images, nil
}

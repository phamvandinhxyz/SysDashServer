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
)

type DockerCollector struct {
	client *http.Client
	host   string
	isUnix bool
}

func NewDockerCollector() *DockerCollector {
	dockerHost := os.Getenv("DOCKER_HOST")
	dockerSocket := os.Getenv("DOCKER_SOCKET")
	var isUnix bool
	var client *http.Client

	if dockerHost == "" {
		if runtime.GOOS == "windows" {
			dockerHost = "http://localhost:2375"
			client = &http.Client{Timeout: 4 * time.Second}
		} else {
			dockerHost = "http://localhost"
			isUnix = true

			// Detect best unix socket location on Linux / VPS
			sockPath := "/var/run/docker.sock"
			if dockerSocket != "" {
				sockPath = dockerSocket
			} else if _, err := os.Stat("/run/docker.sock"); err == nil {
				sockPath = "/run/docker.sock"
			}

			client = &http.Client{
				Timeout: 4 * time.Second,
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						conn, err := net.Dial("unix", sockPath)
						if err != nil && sockPath != "/var/run/docker.sock" {
							return net.Dial("unix", "/var/run/docker.sock")
						}
						return conn, err
					},
				},
			}
		}
	} else if strings.HasPrefix(dockerHost, "unix://") {
		sockPath := strings.TrimPrefix(dockerHost, "unix://")
		dockerHost = "http://localhost"
		isUnix = true
		client = &http.Client{
			Timeout: 4 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", sockPath)
				},
			},
		}
	} else {
		// TCP host (e.g., tcp://127.0.0.1:2375 -> http://127.0.0.1:2375)
		if strings.HasPrefix(dockerHost, "tcp://") {
			dockerHost = "http://" + strings.TrimPrefix(dockerHost, "tcp://")
		}
		client = &http.Client{Timeout: 4 * time.Second}
	}

	return &DockerCollector{
		client: client,
		host:   dockerHost,
		isUnix: isUnix,
	}
}

// Ping checks if Docker daemon is accessible
func (c *DockerCollector) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping returned status %d", resp.StatusCode)
	}
	return nil
}

// Raw struct for decoding Docker Engine /containers/{id}/stats?stream=false
type dockerContainerStatsRaw struct {
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			Cache        uint64 `json:"cache"`
			InactiveFile uint64 `json:"inactive_file"`
		} `json:"stats"`
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

// getContainerStats queries real-time RAM and CPU metrics matching docker stats
func (c *DockerCollector) getContainerStats(containerID string) (memUsage, memLimit uint64, memPct, cpuPct float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/containers/%s/stats?stream=false", c.host, containerID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, 0, 0, 0
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, 0, 0, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, 0, 0
	}

	var raw dockerContainerStatsRaw
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return 0, 0, 0, 0
	}

	// 1. RAM Calculation matching `docker stats` exactly (cgroup v1 & cgroup v2)
	cache := raw.MemoryStats.Stats.InactiveFile
	if cache == 0 {
		cache = raw.MemoryStats.Stats.Cache
	}
	usage := raw.MemoryStats.Usage
	if usage > cache {
		memUsage = usage - cache
	} else {
		memUsage = usage
	}

	memLimit = raw.MemoryStats.Limit
	if memLimit > 0 {
		memPct = math.Round((float64(memUsage)/float64(memLimit))*10000) / 100
	}

	// 2. CPU Calculation matching `docker stats`
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(raw.CPUStats.SystemCPUUsage) - float64(raw.PreCPUStats.SystemCPUUsage)
	onlineCPUs := float64(raw.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
	}
	if onlineCPUs == 0 {
		onlineCPUs = 1.0
	}

	if systemDelta > 0 && cpuDelta > 0 {
		cpuPct = math.Round(((cpuDelta/systemDelta)*onlineCPUs*100.0)*100) / 100
	}

	return memUsage, memLimit, memPct, cpuPct
}

// FormatBytes formats byte counts into human-readable strings (e.g. 52.4 MB, 1.95 GB)
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// GetContainers fetches docker containers along with their active RAM & CPU usage
func (c *DockerCollector) GetContainers() ([]models.DockerContainerInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker engine returned HTTP %d", resp.StatusCode)
	}

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

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	containers := make([]models.DockerContainerInfo, len(raw))

	type statResult struct {
		index    int
		memUsage uint64
		memLimit uint64
		memPct   float64
		cpuPct   float64
	}

	var wg sync.WaitGroup
	statChan := make(chan statResult, len(raw))

	for i, item := range raw {
		id := item.ID
		if len(id) > 12 {
			id = id[:12]
		}
		var cleanNames []string
		for _, name := range item.Names {
			cleanNames = append(cleanNames, strings.TrimPrefix(name, "/"))
		}

		containers[i] = models.DockerContainerInfo{
			ID:                id,
			Names:             cleanNames,
			Image:             item.Image,
			ImageID:           item.ImageID,
			Command:           item.Command,
			Created:           item.Created,
			State:             item.State,
			Status:            item.Status,
			Labels:            item.Labels,
			MemUsageFormatted: "0 B",
		}

		// Query real-time stats concurrently for running containers
		if strings.EqualFold(item.State, "running") {
			wg.Add(1)
			go func(idx int, containerFullID string) {
				defer wg.Done()
				mu, ml, mp, cp := c.getContainerStats(containerFullID)
				statChan <- statResult{
					index:    idx,
					memUsage: mu,
					memLimit: ml,
					memPct:   mp,
					cpuPct:   cp,
				}
			}(i, item.ID)
		}
	}

	wg.Wait()
	close(statChan)

	for res := range statChan {
		containers[res.index].MemUsage = res.memUsage
		containers[res.index].MemLimit = res.memLimit
		containers[res.index].MemPercent = res.memPct
		containers[res.index].CPUPercent = res.cpuPct
		if res.memLimit > 0 {
			containers[res.index].MemUsageFormatted = fmt.Sprintf("%s / %s",
				FormatBytes(res.memUsage),
				FormatBytes(res.memLimit),
			)
		} else {
			containers[res.index].MemUsageFormatted = FormatBytes(res.memUsage)
		}
	}

	return containers, nil
}

// GetImages fetches docker images with disk size and aggregate RAM consumed by its containers
func (c *DockerCollector) GetImages() ([]models.DockerImageInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/images/json", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker engine returned HTTP %d", resp.StatusCode)
	}

	var raw []struct {
		ID         string   `json:"Id"`
		RepoTags   []string `json:"RepoTags"`
		Size       int64    `json:"Size"`
		Created    int64    `json:"Created"`
		Containers int64    `json:"Containers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	// Fetch containers to map RAM usage and running count to images
	containers, _ := c.GetContainers()
	imageMemMap := make(map[string]uint64)
	imageRunningMap := make(map[string]int)

	for _, ct := range containers {
		if strings.EqualFold(ct.State, "running") {
			// Map by ImageID
			imageMemMap[ct.ImageID] += ct.MemUsage
			imageRunningMap[ct.ImageID]++

			// Also map by short ImageID
			shortImgID := ct.ImageID
			if strings.HasPrefix(shortImgID, "sha256:") && len(shortImgID) > 19 {
				shortImgID = shortImgID[7:19]
			}
			imageMemMap[shortImgID] += ct.MemUsage
			imageRunningMap[shortImgID]++

			// Also map by image tag name (e.g. "nginx:alpine")
			imageMemMap[ct.Image] += ct.MemUsage
			imageRunningMap[ct.Image]++
		}
	}

	var images []models.DockerImageInfo
	for _, img := range raw {
		fullID := img.ID
		shortID := fullID
		if strings.HasPrefix(shortID, "sha256:") && len(shortID) > 19 {
			shortID = shortID[7:19]
		}
		tags := img.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}

		// Find aggregated RAM
		var ramUsage uint64
		var runningCount int
		if val, exists := imageMemMap[fullID]; exists {
			ramUsage = val
			runningCount = imageRunningMap[fullID]
		} else if val, exists := imageMemMap[shortID]; exists {
			ramUsage = val
			runningCount = imageRunningMap[shortID]
		} else {
			for _, t := range tags {
				if val, exists := imageMemMap[t]; exists {
					ramUsage = val
					runningCount = imageRunningMap[t]
					break
				}
			}
		}

		images = append(images, models.DockerImageInfo{
			ID:                shortID,
			RepoTags:          tags,
			Size:              img.Size,
			SizeFormatted:     FormatBytes(uint64(img.Size)),
			Created:           img.Created,
			Containers:        img.Containers,
			RunningContainers: runningCount,
			MemUsage:          ramUsage,
			MemFormatted:      FormatBytes(ramUsage),
		})
	}

	return images, nil
}

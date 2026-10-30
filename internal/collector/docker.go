package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
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
			client = &http.Client{Timeout: 3 * time.Second}
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
				Timeout: 3 * time.Second,
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
			Timeout: 3 * time.Second,
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
		client = &http.Client{Timeout: 3 * time.Second}
	}

	return &DockerCollector{
		client: client,
		host:   dockerHost,
		isUnix: isUnix,
	}
}

// Ping checks if Docker daemon is accessible
func (c *DockerCollector) Ping() error {
	req, err := http.NewRequest("GET", c.host+"/_ping", nil)
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

// GetImages fetches docker images from Docker Engine API (GET /images/json)
func (c *DockerCollector) GetImages() ([]models.DockerImageInfo, error) {
	req, err := http.NewRequest("GET", c.host+"/images/json", nil)
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

	var images []models.DockerImageInfo
	for _, img := range raw {
		id := img.ID
		if strings.HasPrefix(id, "sha256:") && len(id) > 19 {
			id = id[7:19] // Short ID
		}
		tags := img.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}
		images = append(images, models.DockerImageInfo{
			ID:         id,
			RepoTags:   tags,
			Size:       img.Size,
			Created:    img.Created,
			Containers: img.Containers,
		})
	}

	return images, nil
}

// GetContainers fetches docker containers from Docker Engine API (GET /containers/json?all=true)
func (c *DockerCollector) GetContainers() ([]models.DockerContainerInfo, error) {
	req, err := http.NewRequest("GET", c.host+"/containers/json?all=true", nil)
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

	var containers []models.DockerContainerInfo
	for _, item := range raw {
		id := item.ID
		if len(id) > 12 {
			id = id[:12]
		}
		var cleanNames []string
		for _, name := range item.Names {
			cleanNames = append(cleanNames, strings.TrimPrefix(name, "/"))
		}

		containers = append(containers, models.DockerContainerInfo{
			ID:      id,
			Names:   cleanNames,
			Image:   item.Image,
			ImageID: item.ImageID,
			Command: item.Command,
			Created: item.Created,
			State:   item.State,
			Status:  item.Status,
			Labels:  item.Labels,
		})
	}

	return containers, nil
}

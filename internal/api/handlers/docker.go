package handlers

import (
	"net/http"

	"SysDashServer/internal/collector"

	"github.com/gin-gonic/gin"
)

type DockerHandler struct {
	dockerCollector *collector.DockerCollector
}

func NewDockerHandler(dockerCollector *collector.DockerCollector) *DockerHandler {
	return &DockerHandler{dockerCollector: dockerCollector}
}

// GetImages returns list of Docker images on the host
func (h *DockerHandler) GetImages(c *gin.Context) {
	if err := h.dockerCollector.Ping(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"available": false,
			"message":   "Docker daemon is not accessible: " + err.Error(),
			"hint":      "If running inside Docker container, mount '-v /var/run/docker.sock:/var/run/docker.sock:ro'. On host, ensure user has read permission.",
			"count":     0,
			"images":    []interface{}{},
		})
		return
	}

	images, err := h.dockerCollector.GetImages()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to query docker images",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"available": true,
		"count":     len(images),
		"images":    images,
	})
}

// GetContainers returns list of Docker containers on the host
func (h *DockerHandler) GetContainers(c *gin.Context) {
	if err := h.dockerCollector.Ping(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"available":  false,
			"message":    "Docker daemon is not accessible: " + err.Error(),
			"hint":       "If running inside Docker container, mount '-v /var/run/docker.sock:/var/run/docker.sock:ro'. On host, ensure user has read permission.",
			"count":      0,
			"containers": []interface{}{},
		})
		return
	}

	containers, err := h.dockerCollector.GetContainers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to query docker containers",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"available":  true,
		"count":      len(containers),
		"containers": containers,
	})
}

package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetDiagnostics returns raw /proc/meminfo values, mount scan decisions and environment
// so API numbers can be compared 1:1 with `free -b`, `df -B1` and htop on the VPS.
func (h *MetricsHandler) GetDiagnostics(c *gin.Context) {
	c.JSON(http.StatusOK, h.collector.Diagnostics())
}

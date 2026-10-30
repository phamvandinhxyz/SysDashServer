package handlers

import (
	"log"
	"net/http"

	"SysDashServer/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for mobile apps and web dashboards
		return true
	},
}

type WSHandler struct {
	monitor *service.MonitorService
}

func NewWSHandler(monitor *service.MonitorService) *WSHandler {
	return &WSHandler{monitor: monitor}
}

// StreamMetrics handles incoming WebSocket connections for streaming real-time metrics
func (h *WSHandler) StreamMetrics(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] Upgrade error: %v", err)
		return
	}

	h.monitor.AddWSClient(conn)

	// Keep connection alive and wait for close or incoming messages
	go func() {
		defer h.monitor.RemoveWSClient(conn)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

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
	limit := 5
	limitStr := c.Query("limit")
	if limitStr == "" {
		limitStr = c.Query("top_limit")
	}
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	sort := c.DefaultQuery("sort", "cpu")
	order := c.DefaultQuery("order", "desc")

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] Upgrade error: %v", err)
		return
	}

	h.monitor.AddWSClient(conn, service.ClientWSOptions{
		Limit: limit,
		Sort:  sort,
		Order: order,
	})

	// Keep connection alive and wait for close or incoming messages
	go func() {
		defer h.monitor.RemoveWSClient(conn)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			// Allow client to dynamically change top processes limit, sort, and order during WebSocket session
			var req struct {
				Limit    int    `json:"limit"`
				TopLimit int    `json:"top_limit"`
				Sort     string `json:"sort"`
				Order    string `json:"order"`
			}
			if err := json.Unmarshal(msg, &req); err == nil {
				newLimit := req.Limit
				if newLimit <= 0 {
					newLimit = req.TopLimit
				}
				h.monitor.UpdateWSClientOptions(conn, newLimit, req.Sort, req.Order)
			}
		}
	}()
}

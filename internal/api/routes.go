package api

import (
	"net/http"

	"SysDashServer/internal/api/handlers"
	"SysDashServer/internal/api/middleware"
	"SysDashServer/internal/auth"
	"SysDashServer/internal/collector"
	"SysDashServer/internal/database"
	"SysDashServer/internal/models"
	"SysDashServer/internal/service"

	"github.com/gin-gonic/gin"
)

func SetupRouter(
	db *database.DB,
	jwtService *auth.JWTService,
	monitor *service.MonitorService,
	sysCollector *collector.SystemCollector,
	dockerCollector *collector.DockerCollector,
) *gin.Engine {
	r := gin.Default()

	// Apply CORS
	r.Use(middleware.CORSMiddleware())

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(db, jwtService)
	userHandler := handlers.NewUserHandler(db)
	metricsHandler := handlers.NewMetricsHandler(db, monitor, sysCollector)
	alertHandler := handlers.NewAlertHandler(db)
	wsHandler := handlers.NewWSHandler(monitor)
	dockerHandler := handlers.NewDockerHandler(dockerCollector)

	// Health check endpoint
	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "SysDashServer"})
	})

	// Public auth routes
	authGroup := r.Group("/api/v1/auth")
	{
		authGroup.POST("/login", authHandler.Login)
	}

	// Protected routes (Both Viewer and Admin can access)
	api := r.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(jwtService))
	{
		// Profile
		api.GET("/auth/me", authHandler.Me)

		// System & Metrics (Viewer & Admin)
		api.GET("/system/info", metricsHandler.GetHostInfo)
		api.GET("/system/processes", metricsHandler.GetProcesses) // Process search, sort & limit
		api.GET("/metrics/current", metricsHandler.GetCurrent)
		api.GET("/metrics/history", metricsHandler.GetHistory)   // Filter by range, bucket, from, to
		api.GET("/metrics/summary", metricsHandler.GetSummary)   // Statistical summary (Min, Max, Avg)

		// Docker (Viewer & Admin)
		api.GET("/docker/images", dockerHandler.GetImages)         // List Docker Images
		api.GET("/docker/containers", dockerHandler.GetContainers) // List Docker Containers

		// Alerts viewing (Viewer & Admin)
		api.GET("/alerts", alertHandler.ListAlerts)              // Filter by type, level, range & pagination
		api.GET("/alerts/config", alertHandler.GetThreshold)

		// Realtime WebSocket stream (Viewer & Admin)
		api.GET("/ws/metrics", wsHandler.StreamMetrics)

		// Admin-only management endpoints
		adminOnly := api.Group("")
		adminOnly.Use(middleware.RequireRoles(models.RoleAdmin))
		{
			// User management (Search, role filter, pagination)
			adminOnly.POST("/auth/register", authHandler.Register)
			adminOnly.GET("/users", userHandler.ListUsers)
			adminOnly.POST("/users", userHandler.CreateUser)
			adminOnly.PUT("/users/:id", userHandler.UpdateUser)
			adminOnly.DELETE("/users/:id", userHandler.DeleteUser)

			// Alert thresholds configuration
			adminOnly.PUT("/alerts/config", alertHandler.UpdateThreshold)
		}
	}

	return r
}

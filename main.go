package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"SysDashServer/config"
	"SysDashServer/internal/api"
	"SysDashServer/internal/auth"
	"SysDashServer/internal/collector"
	"SysDashServer/internal/database"
	"SysDashServer/internal/service"
)

func main() {
	log.Println("==================================================")
	log.Println("              Starting SysDashServer              ")
	log.Println("==================================================")

	// 1. Load Configuration
	cfg := config.LoadConfig()

	// 2. Initialize Database (SQLite with AutoMigrate & Seeder)
	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	log.Printf("[Database] Connected to SQLite database: %s", cfg.DBPath)

	// 3. Initialize JWT Service
	jwtService := auth.NewJWTService(cfg.JWTSecret, cfg.TokenDuration)

	// 4. Initialize Collectors (System & Docker)
	sysCollector := collector.NewSystemCollector()
	dockerCollector := collector.NewDockerCollector()

	// 5. Initialize & Start Monitor Service (Realtime + SQLite Historical Time-series)
	monitorService := service.NewMonitorService(cfg, db, sysCollector)
	monitorService.Start()

	// 6. Setup Gin Router & Middleware (Auth + RBAC + CORS + Docker)
	router := api.SetupRouter(db, jwtService, monitorService, sysCollector, dockerCollector)

	// 7. Start HTTP Server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("[Server] SysDashServer listening on http://0.0.0.0:%s", cfg.Port)
		log.Printf("[Server] WebSocket endpoint available at ws://0.0.0.0:%s/api/v1/ws/metrics", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server listen error: %v", err)
		}
	}()

	// 8. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("\n[Server] Shutting down SysDashServer gracefully...")
}

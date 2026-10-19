package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"edupilot-gateway/internal/config"
	"edupilot-gateway/internal/middleware"
	"edupilot-gateway/internal/model"
	"edupilot-gateway/internal/rabbitmq"
	"edupilot-gateway/internal/repository"
	"edupilot-gateway/internal/route"
	"edupilot-gateway/internal/service"

	"github.com/gin-gonic/gin"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	db, err := cfg.ConnectDB(ctx, 10)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}

	if err := db.AutoMigrate(&model.RequestLog{}, &model.AuditLog{}); err != nil {
		log.Fatalf("Failed to auto migrate: %v", err)
	}

	logRepo := repository.NewLogRepository(db)
	logService := service.NewLogService(logRepo)
	logWriter := middleware.NewAsyncLogWriter(logService, cfg.RequestLogBuffer, cfg.RequestLogWorkers)

	// Audit log consumer (reconnects on its own until ctx is cancelled).
	var consumerWG sync.WaitGroup
	consumerWG.Add(1)
	go func() {
		defer consumerWG.Done()
		rabbitmq.Run(ctx, rabbitmq.ConsumerConfig{
			URL:      cfg.RabbitMQURL,
			Queue:    cfg.AuditQueue,
			DLX:      cfg.AuditDLX,
			DLQ:      cfg.AuditDLQ,
			Prefetch: cfg.RabbitMQPrefetch,
		}, logService)
	}()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// The gateway is the edge: trust no forwarding headers; ClientIP() = RemoteAddr.
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("Failed to configure trusted proxies: %v", err)
	}
	r.Use(gin.Recovery())
	route.SetupRoutes(r, cfg, logWriter)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second, // allows slow 5MB uploads
		WriteTimeout:      90 * time.Second, // > upstream ResponseHeaderTimeout
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Starting EduPilot API Gateway on port %s (env=%s)", cfg.Port, cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
		log.Printf("Shutdown signal received")
	case err := <-serverErr:
		if err != nil {
			log.Printf("Server error: %v", err)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	logWriter.Close(shutdownCtx)
	consumerWG.Wait()
	log.Printf("Gateway stopped")
}

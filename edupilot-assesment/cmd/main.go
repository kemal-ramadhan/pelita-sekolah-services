package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"edupilot-assesment/internal/client"
	"edupilot-assesment/internal/config"
	"edupilot-assesment/internal/middleware"
	"edupilot-assesment/internal/rabbitmq"
	"edupilot-assesment/internal/route"
	"edupilot-assesment/internal/service/scoring"

	"github.com/gin-gonic/gin"
)

func main() {
	config.LoadEnv()
	if os.Getenv("APP_ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	config.InitLogger()
	defer func() { _ = config.Logger.Sync() }()

	if os.Getenv("JWT_SECRET") == "" {
		log.Fatal("JWT_SECRET is not set; refusing to start")
	}
	if os.Getenv("GATEWAY_SECRET") == "" {
		log.Println("Warning: GATEWAY_SECRET is not set; gateway authentication and instantion internal calls will fail")
	}
	if _, err := scoring.LoadConfig(); err != nil {
		log.Fatalf("invalid scoring config: %v", err)
	}
	client.Default = client.NewFromEnv()

	config.ConnectDB()
	config.ConnectRedis()
	config.ConnectRabbitMQ()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workers, _ := strconv.Atoi(config.Getenv("ASSESSMENT_WORKERS", "4"))
	if workers <= 0 {
		workers = 4
	}
	rabbitmq.StartWorkerPool(ctx, workers)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.GatewayAuthMiddleware())
	r.Use(func(c *gin.Context) { // 1 MB body limit
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	})
	route.SetupRoutes(r)

	port := config.Getenv("PORT", "8080")
	srv := &http.Server{Addr: ":" + port, Handler: r, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second}
	go func() {
		log.Printf("Starting edupilot-assesment service on port %s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down edupilot-assesment service...")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	if config.RabbitCh != nil {
		_ = config.RabbitCh.Close()
	}
	if config.RabbitConn != nil {
		_ = config.RabbitConn.Close()
	}
}

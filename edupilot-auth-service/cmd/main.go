package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Depado/ginprom"
	_ "github.com/edupilot-auth-service/docs"
	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/middleware"
	"github.com/edupilot-auth-service/internal/route"
	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

// @title EduPilot Auth Service API
// @version 1.0
// @description Authentication service for edupilot-auth-service
// @BasePath /

// @host localhost:8080
// @schemes http

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
func main() {
	config.LoadEnv()

	if os.Getenv("JWT_SECRET") == "" {
		log.Fatal("JWT_SECRET is not set; refusing to start")
	}
	if config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	config.InitLogger()
	defer func() { _ = config.Logger.Sync() }()

	config.ConnectDB()
	config.ConnectRedis()

	if err := service.SeedRBAC(); err != nil {
		config.Logger.Fatal("RBAC seed failed", zap.Error(err))
	}
	if err := service.SeedSuperAdmin(); err != nil {
		config.Logger.Error("super_admin seed failed", zap.Error(err))
	}

	config.InitTracer()
	defer config.ShutdownTracer(context.Background())

	r := gin.New()
	// Client IP is resolved by middleware.ClientIP (X-Real-IP only from the verified gateway).
	if err := r.SetTrustedProxies(nil); err != nil {
		config.Logger.Fatal("failed to configure trusted proxies", zap.Error(err))
	}
	r.Use(gin.Recovery())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(otelgin.Middleware(config.ServiceName))

	prom := ginprom.New(
		ginprom.Engine(r),
		ginprom.Subsystem("edupilot_auth_service"),
		ginprom.Path("/metrics"),
	)
	r.Use(prom.Instrument())

	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.SecureHeaders())
	r.Use(middleware.GatewayAuthMiddleware())
	r.Use(middleware.BodyLimit(1<<20, 3<<20, "/auth/photo-profile", "/auth/cover"))

	r.Use(middleware.RateLimiter(middleware.RateLimitOptions{
		Prefix:    "rl:global",
		Requests:  100,
		Window:    time.Minute,
		UseIPOnly: true,
	}))

	route.SetupRoutes(r)

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		config.Logger.Info("EduPilot Auth Service running",
			zap.String("port", port),
			zap.String("env", os.Getenv("APP_ENV")),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			config.Logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	config.Logger.Info("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		config.Logger.Error("Forced to shutdown", zap.Error(err))
	}
	config.Logger.Info("Server exited cleanly")
}

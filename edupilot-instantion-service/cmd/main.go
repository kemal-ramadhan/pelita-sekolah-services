package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Depado/ginprom"
	_ "github.com/edupilot-instantion-service/docs"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/route"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

const (
	defaultBodyLimit = 1 << 20  // 1 MB
	importBodyLimit  = 6 << 20  // 5 MB file + multipart overhead
	logoBodyLimit    = 3 << 20  // 2 MB image + multipart overhead
	studentImportLim = 11 << 20 // 10 MB .xlsx + multipart overhead
	studentFormLimit = 4 << 20  // student JSON incl. an echoed photo data URL
)

// @title EduPilot Instantion Service API
// @version 1.0
// @description School (institution) and teacher management service for EduPilot.
// @BasePath /

// @host localhost:8888
// @schemes http

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
func main() {
	config.LoadEnv()

	if os.Getenv("APP_ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	config.InitLogger()
	defer func() { _ = config.Logger.Sync() }()

	if os.Getenv("JWT_SECRET") == "" {
		config.Logger.Fatal("JWT_SECRET is not set; refusing to start")
	}
	if os.Getenv("GATEWAY_SECRET") == "" {
		config.Logger.Warn("GATEWAY_SECRET is not set; gateway authentication is disabled")
	}
	if os.Getenv("INTERNAL_SERVICE_SECRET") == "" {
		config.Logger.Warn("INTERNAL_SERVICE_SECRET is not set; admin school+account creation will fail")
	}

	config.ConnectDB()
	config.ConnectRedis()

	config.InitTracer()
	defer config.ShutdownTracer(context.Background())

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(otelgin.Middleware(config.ServiceName))

	prom := ginprom.New(
		ginprom.Engine(r),
		ginprom.Subsystem("edupilot_instantion_service"),
		ginprom.Path("/metrics"),
	)
	r.Use(prom.Instrument())

	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.SecureHeaders())
	r.Use(middleware.GatewayAuthMiddleware())

	r.Use(middleware.RateLimiter(middleware.RateLimitOptions{
		Prefix:    "rl:instantion:global",
		Requests:  100,
		Window:    1 * time.Minute,
		UseIPOnly: true,
	}))

	r.Use(middleware.BodyLimit(defaultBodyLimit, map[string]int64{
		route.TeacherImportPath:   importBodyLimit,
		route.SchoolLogoPath:      logoBodyLimit,
		route.StudentImportPath:   studentImportLim,
		route.StudentPhotoPath:    logoBodyLimit,
		route.StudentDocumentPath: logoBodyLimit,
		route.StudentsPath:        studentFormLimit,
		route.StudentPath:         studentFormLimit,
	}))

	route.SetupRoutes(r)

	port := config.Getenv("APP_PORT", "8080")
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		config.Logger.Info(config.ServiceName+" running",
			zap.String("port", port),
			zap.String("env", os.Getenv("APP_ENV")),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			config.Logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	config.Logger.Info("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		config.Logger.Error("Forced to shutdown", zap.Error(err))
	}
	if sqlDB, err := config.DB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	_ = config.RDB.Close()

	config.Logger.Info("Server exited cleanly")
}

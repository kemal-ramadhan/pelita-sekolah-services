package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/gin-gonic/gin"
)

type HealthResponse struct {
	Status    string `json:"status"`
	Database  string `json:"database"`
	Redis     string `json:"redis"`
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
}

// HealthCheck godoc
// @Summary Health Check
// @Description Check service, database, and Redis connectivity (503 if any is down)
// @Tags System
// @Produce json
// @Success 200 {object} HealthResponse
// @Failure 503 {object} HealthResponse
// @Router /health [get]
func HealthCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	status, code := "ok", http.StatusOK

	dbStatus := "ok"
	if sqlDB, err := config.DB.DB(); err != nil || sqlDB.PingContext(ctx) != nil {
		dbStatus, status, code = "error", "degraded", http.StatusServiceUnavailable
	}
	redisStatus := "ok"
	if err := config.RedisDB().Ping(ctx).Err(); err != nil {
		redisStatus, status, code = "error", "degraded", http.StatusServiceUnavailable
	}

	c.JSON(code, HealthResponse{
		Status:    status,
		Database:  dbStatus,
		Redis:     redisStatus,
		Timestamp: time.Now().Format(time.RFC3339),
		Service:   config.ServiceName,
	})
}

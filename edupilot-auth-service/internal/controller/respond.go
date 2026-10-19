package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// respondError sends client-safe AppErrors as-is; anything else is logged and
// answered with a generic 500 (never leaking DB error text).
func respondError(c *gin.Context, err error) {
	var appErr *service.AppError
	if errors.As(err, &appErr) {
		body := gin.H{"error": appErr.Message}
		if appErr.Code != "" {
			body["code"] = appErr.Code
		}
		c.AbortWithStatusJSON(appErr.Status, body)
		return
	}
	if config.Logger != nil {
		config.Logger.Error("request failed",
			zap.String("request_id", c.GetString("request_id")),
			zap.String("method", c.Request.Method),
			zap.String("path", c.FullPath()),
			zap.Error(err),
		)
	}
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "Terjadi kesalahan pada server",
		"code":  "INTERNAL_ERROR",
	})
}

func respondBadRequest(c *gin.Context, err error) {
	body := gin.H{"error": "Data permintaan tidak valid", "code": "VALIDATION_ERROR"}
	if err != nil {
		body["details"] = err.Error()
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, body)
}

// currentUser returns the authenticated user id and role set by AuthMiddleware.
func currentUser(c *gin.Context) (uint, string, bool) {
	v, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "UNAUTHORIZED"})
		return 0, "", false
	}
	id, ok := v.(uint)
	if !ok || id == 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "UNAUTHORIZED"})
		return 0, "", false
	}
	return id, c.GetString("role"), true
}

func paramUint(c *gin.Context, name string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil || v == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "ID tidak valid", "code": "VALIDATION_ERROR"})
		return 0, false
	}
	return uint(v), true
}

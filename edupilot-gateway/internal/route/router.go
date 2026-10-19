package route

import (
	"net/http"

	"edupilot-gateway/internal/config"
	"edupilot-gateway/internal/middleware"

	"github.com/gin-gonic/gin"
)

func notFound(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found", "code": "NOT_FOUND"})
}

// SetupRoutes registers explicit upstream routes only. Anything else is 404.
func SetupRoutes(r *gin.Engine, cfg *config.Config, logWriter *middleware.AsyncLogWriter) {
	r.Use(middleware.RequestID())
	r.Use(middleware.RequestLogger(logWriter))

	transport := middleware.NewTransport()
	authProxy := middleware.NewReverseProxy(cfg.AuthServiceURL, cfg.GatewaySecret, transport)
	instantionProxy := middleware.NewReverseProxy(cfg.InstantionServiceURL, cfg.GatewaySecret, transport)
	assessmentProxy := middleware.NewReverseProxy(cfg.AssessmentServiceURL, cfg.GatewaySecret, transport)

	// Gateway's own health check.
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "edupilot-gateway"})
	})

	// Never expose internal endpoints or metrics through the gateway.
	r.Any("/internal", notFound)
	r.Any("/internal/*path", notFound)
	r.Any("/metrics", notFound)
	r.Any("/metrics/*path", notFound)

	// API docs are only exposed outside production (served by auth-service).
	if cfg.IsProduction() {
		r.Any("/docs", notFound)
		r.Any("/docs/*path", notFound)
	} else {
		r.Any("/docs/*path", authProxy)
	}

	for _, prefix := range []string{"/auth", "/other", "/customer"} {
		r.Any(prefix, authProxy)
		r.Any(prefix+"/*path", authProxy)
	}
	r.Any("/instantion", instantionProxy)
	r.Any("/instantion/*path", instantionProxy)
	r.Any("/assessment", assessmentProxy)
	r.Any("/assessment/*path", assessmentProxy)

	r.NoRoute(notFound)
}

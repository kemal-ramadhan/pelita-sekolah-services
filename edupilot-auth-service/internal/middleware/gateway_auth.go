package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/edupilot-auth-service/internal/utils"
	"github.com/gin-gonic/gin"
)

const gatewayVerifiedKey = "gateway_verified"

// GatewayAuthMiddleware enforces X-Gateway-Auth == GATEWAY_SECRET when GATEWAY_SECRET is set.
// /health, /metrics and /internal/* are exempt (/internal has its own secret; /health and
// /metrics are used by docker/prometheus on the private network; the gateway 404s them).
func GatewayAuthMiddleware() gin.HandlerFunc {
	expected := os.Getenv("GATEWAY_SECRET")
	return func(c *gin.Context) {
		header := c.GetHeader("X-Gateway-Auth")
		if expected != "" && header != "" && utils.SecureCompare(header, expected) {
			c.Set(gatewayVerifiedKey, true)
			c.Next()
			return
		}

		path := c.Request.URL.Path
		if expected == "" || path == "/health" || path == "/metrics" || strings.HasPrefix(path, "/internal/") {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized gateway", "code": "GATEWAY_UNAUTHORIZED"})
	}
}

// InternalAuthMiddleware guards /internal/* with X-Internal-Secret == INTERNAL_SERVICE_SECRET.
func InternalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := os.Getenv("INTERNAL_SERVICE_SECRET")
		if expected == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "internal API not configured", "code": "INTERNAL_DISABLED"})
			return
		}
		if !utils.SecureCompare(c.GetHeader("X-Internal-Secret"), expected) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "UNAUTHORIZED"})
			return
		}
		c.Next()
	}
}

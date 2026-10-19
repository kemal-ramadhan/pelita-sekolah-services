package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// GatewayAuthMiddleware enforces X-Gateway-Auth (constant-time compare) when
// GATEWAY_SECRET is set. /health and /metrics are skipped. When the header is
// validated, CtxGatewayVerified is set so X-Real-IP can be trusted.
func GatewayAuthMiddleware() gin.HandlerFunc {
	expected := []byte(os.Getenv("GATEWAY_SECRET"))
	return func(c *gin.Context) {
		if len(expected) == 0 {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path == "/health" || path == "/metrics" {
			// Still mark as verified if a valid header happens to be present.
			if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Gateway-Auth")), expected) == 1 {
				c.Set(CtxGatewayVerified, true)
			}
			c.Next()
			return
		}
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Gateway-Auth")), expected) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized gateway", "code": "UNAUTHORIZED_GATEWAY"})
			return
		}
		c.Set(CtxGatewayVerified, true)
		c.Next()
	}
}

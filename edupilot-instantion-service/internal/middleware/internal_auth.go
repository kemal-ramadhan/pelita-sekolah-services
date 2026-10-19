package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// InternalPathPrefix marks service-to-service routes (API_CONTRACT §13.1).
// The gateway 404s them; they are only reachable on the docker network.
const InternalPathPrefix = "/internal/"

var errUnauthorizedGateway = gin.H{"error": "unauthorized gateway", "code": "UNAUTHORIZED_GATEWAY"}

// RequireInternalGateway guards /internal/* routes with X-Gateway-Auth =
// GATEWAY_SECRET (constant-time compare). Unlike GatewayAuthMiddleware it
// fails CLOSED: with an empty GATEWAY_SECRET every request is rejected, so
// internal routes are never open by misconfiguration.
func RequireInternalGateway() gin.HandlerFunc {
	expected := []byte(os.Getenv("GATEWAY_SECRET"))
	return func(c *gin.Context) {
		if len(expected) == 0 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Gateway-Auth")), expected) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errUnauthorizedGateway)
			return
		}
		c.Set(CtxGatewayVerified, true)
		c.Next()
	}
}

// isVerifiedInternalCall: /internal/* request whose gateway header was validated.
func isVerifiedInternalCall(c *gin.Context) bool {
	return c.GetBool(CtxGatewayVerified) && strings.HasPrefix(c.Request.URL.Path, InternalPathPrefix)
}

package middleware

import (
	"net/http"
	"strings"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/utils"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// revokedBeforeLookup is overridable in tests.
var revokedBeforeLookup = config.GetRevokedBefore

// authenticate validates an access token and checks the Redis revocation marker.
//
// Fail-open choice: an invalid/expired/wrong-typ token is always rejected, but if Redis
// is unreachable the revocation check is skipped (logged) so a Redis outage does not
// lock every user out. Access tokens are short-lived, which bounds the exposure.
func authenticate(token string) (*utils.JWTClaim, error) {
	claims, err := utils.ValidateToken(token, utils.TokenTypeAccess)
	if err != nil {
		return nil, err
	}
	revokedBefore, err := revokedBeforeLookup(claims.UserID)
	if err != nil {
		if config.Logger != nil {
			config.Logger.Warn("revocation check skipped: redis error", zap.Uint("user_id", claims.UserID), zap.Error(err))
		}
		return claims, nil
	}
	if utils.IsRevoked(claims.IssuedAt.Time, revokedBefore) {
		return nil, utils.ErrTokenRevoked
	}
	return claims, nil
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if authHeader == "" || token == authHeader || strings.TrimSpace(token) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token tidak ditemukan", "code": "UNAUTHORIZED"})
			return
		}

		claims, err := authenticate(strings.TrimSpace(token))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token tidak valid atau kedaluwarsa", "code": "UNAUTHORIZED"})
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)
		c.Set("customer_role", claims.CustomerRole)
		c.Next()
	}
}

// RequireRole allows the request only if the JWT role is one of roles. Must run after AuthMiddleware.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		if !allowed[c.GetString("role")] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki akses", "code": "FORBIDDEN"})
			return
		}
		c.Next()
	}
}

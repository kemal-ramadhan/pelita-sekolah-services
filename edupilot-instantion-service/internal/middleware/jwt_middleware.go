package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RevokedBeforeKey is the Redis key holding the unix-seconds cutoff below which
// tokens of a user are rejected (API_CONTRACT.md §3).
func RevokedBeforeKey(userID uint) string {
	return fmt.Sprintf("auth:revoked_before:user:%d", userID)
}

// AuthMiddleware validates an HS256 access token, checks Redis revocation and
// stores user_id (uint), email and role in the gin context.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header", "code": "UNAUTHORIZED"})
			return
		}

		token, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization format", "code": "UNAUTHORIZED"})
			return
		}

		claims, err := utils.ParseAccessToken(strings.TrimSpace(token), utils.JWTSecret())
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token", "code": "UNAUTHORIZED"})
			return
		}

		if isRevoked(c.Request.Context(), claims.UserID, claims.IssuedAt.Unix()) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token has been revoked", "code": "TOKEN_REVOKED"})
			return
		}

		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxEmail, claims.Email)
		c.Set(CtxRole, claims.Role)
		c.Set(CtxCustomerRole, claims.CustomerRole)
		c.Next()
	}
}

// isRevoked reports whether a token issued at iat was revoked. On Redis errors
// it logs and allows the request (fail-open, per contract decision).
func isRevoked(ctx context.Context, userID uint, iat int64) bool {
	if config.RDB == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	val, err := config.RDB.Get(ctx, RevokedBeforeKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return false
	}
	if err != nil {
		config.Logger.Warn("revocation check failed, allowing request", zap.Error(err), zap.Uint("user_id", userID))
		return false
	}
	cutoff, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	if err != nil {
		config.Logger.Warn("invalid revocation value", zap.Uint("user_id", userID))
		return false
	}
	return iat < cutoff
}

// RequireRoles allows the request only when the JWT role is one of roles.
func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c *gin.Context) {
		if _, ok := allowed[c.GetString(CtxRole)]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki akses", "code": "FORBIDDEN"})
			return
		}
		c.Next()
	}
}

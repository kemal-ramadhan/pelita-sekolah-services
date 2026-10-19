package middleware

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/config"
	"github.com/gin-gonic/gin"
)

type RateLimitOptions struct {
	Prefix    string
	Requests  int
	Window    time.Duration
	UseIPOnly bool
	KeyGetter func(*gin.Context) string
}

func RateLimiter(opts RateLimitOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if os.Getenv("RATE_LIMIT_ENABLED") == "false" || opts.Requests <= 0 {
			c.Next()
			return
		}
		// Service-to-service calls (assessment → /internal/*) all come from one
		// container IP; they are authenticated by the gateway secret instead.
		if isVerifiedInternalCall(c) {
			c.Next()
			return
		}

		ip := ClientIP(c)
		key := ip
		if !opts.UseIPOnly && opts.KeyGetter != nil {
			if extra := opts.KeyGetter(c); extra != "" {
				key = fmt.Sprintf("%s:%s", ip, strings.ToLower(extra))
			}
		}
		redisKey := fmt.Sprintf("%s:%s", opts.Prefix, key)

		ctx := c.Request.Context()
		current, err := config.RDB.Incr(ctx, redisKey).Result()
		if err != nil {
			c.Next() // fail open when Redis is unavailable
			return
		}
		// Only start the window on the first hit, so it is a fixed window.
		if current == 1 {
			config.RDB.Expire(ctx, redisKey, opts.Window)
		}

		ttl, _ := config.RDB.TTL(ctx, redisKey).Result()
		if ttl < 0 {
			// Key without TTL (e.g. Expire failed earlier): repair it.
			config.RDB.Expire(ctx, redisKey, opts.Window)
			ttl = opts.Window
		}

		if current > int64(opts.Requests) {
			c.Header("Retry-After", fmt.Sprintf("%d", int(ttl.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests, please slow down",
				"code":  "RATE_LIMITED",
			})
			return
		}

		remaining := opts.Requests - int(current)
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", opts.Requests))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", int(ttl.Seconds())))

		c.Next()
	}
}

// ClientIP returns X-Real-IP only when the gateway header was validated by
// GatewayAuthMiddleware; otherwise the TCP peer address.
func ClientIP(c *gin.Context) string {
	if c.GetBool(CtxGatewayVerified) {
		if v := strings.TrimSpace(c.GetHeader("X-Real-IP")); v != "" {
			return v
		}
	}
	return remoteIP(c.Request)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if r.RemoteAddr == "" {
			return "unknown"
		}
		return r.RemoteAddr
	}
	return host
}

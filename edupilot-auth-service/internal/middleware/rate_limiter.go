package middleware

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type RateLimitOptions struct {
	Prefix    string
	Requests  int
	Window    time.Duration
	UseIPOnly bool
	KeyGetter func(*gin.Context) string
}

// RateLimiter is a fixed-window limiter in Redis. The window starts at the first
// request (EXPIRE only when INCR == 1). On Redis errors it fails open (logged).
func RateLimiter(opts RateLimitOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if os.Getenv("RATE_LIMIT_ENABLED") == "false" || opts.Requests <= 0 {
			c.Next()
			return
		}

		key := ClientIP(c)
		if !opts.UseIPOnly && opts.KeyGetter != nil {
			if extra := opts.KeyGetter(c); extra != "" {
				key = fmt.Sprintf("%s:%s", key, strings.ToLower(extra))
			}
		}
		redisKey := fmt.Sprintf("%s:%s", opts.Prefix, key)

		ctx := config.Ctx()
		current, err := config.RDB.Incr(ctx, redisKey).Result()
		if err != nil {
			logLimiterErr(err)
			c.Next()
			return
		}
		ttl, err := config.RDB.TTL(ctx, redisKey).Result()
		if err != nil {
			logLimiterErr(err)
		}
		// New key, or a key left without expiry (e.g. crash between INCR and EXPIRE).
		if current == 1 || (err == nil && ttl < 0) {
			if err := config.RDB.Expire(ctx, redisKey, opts.Window).Err(); err != nil {
				logLimiterErr(err)
			}
			ttl = opts.Window
		}

		remaining := opts.Requests - int(current)
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", opts.Requests))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", int(ttl.Seconds())))

		if current > int64(opts.Requests) {
			c.Header("Retry-After", fmt.Sprintf("%d", int(ttl.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Terlalu banyak permintaan, silakan coba lagi nanti",
				"code":  "RATE_LIMITED",
			})
			return
		}
		c.Next()
	}
}

func logLimiterErr(err error) {
	if config.Logger != nil {
		config.Logger.Warn("rate limiter redis error, failing open", zap.Error(err))
	}
}

// ClientIP returns X-Real-IP only when the request came through the verified gateway,
// otherwise the TCP peer address.
func ClientIP(c *gin.Context) string {
	if c.GetBool(gatewayVerifiedKey) {
		if v := strings.TrimSpace(c.GetHeader("X-Real-IP")); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		if c.Request.RemoteAddr == "" {
			return "unknown"
		}
		return c.Request.RemoteAddr
	}
	return host
}

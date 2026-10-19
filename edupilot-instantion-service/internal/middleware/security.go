package middleware

import (
	"net/http"
	"strings"

	"github.com/edupilot-instantion-service/internal/config"
	"github.com/gin-gonic/gin"
)

// ParseOrigins splits a comma-separated origin allowlist.
func ParseOrigins(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" {
			out[o] = struct{}{}
		}
	}
	return out
}

// CORSMiddleware echoes the Origin only when it is in CORS_ALLOW_ORIGINS.
func CORSMiddleware() gin.HandlerFunc {
	allowed := ParseOrigins(config.Getenv("CORS_ALLOW_ORIGINS", "http://localhost:5173,http://localhost:5174"))

	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		origin := c.GetHeader("Origin")
		_, ok := allowed[origin]
		if origin != "" && ok {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-Request-ID")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions && origin != "" {
			if ok {
				c.AbortWithStatus(http.StatusNoContent)
			} else {
				c.AbortWithStatus(http.StatusForbidden)
			}
			return
		}
		c.Next()
	}
}

func SecureHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-XSS-Protection", "0")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}

// BodyLimit caps request bodies; routes listed in overrides (by gin FullPath)
// get their own limit (e.g. the teacher .xlsx import).
func BodyLimit(defaultLimit int64, overrides map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := defaultLimit
		if l, ok := overrides[c.FullPath()]; ok {
			limit = l
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

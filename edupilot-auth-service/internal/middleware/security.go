package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

const defaultCORSOrigins = "http://localhost:5173,http://localhost:5174,http://localhost:5175"

// CORSMiddleware echoes the request Origin only when it is in CORS_ALLOW_ORIGINS.
func CORSMiddleware() gin.HandlerFunc {
	raw := os.Getenv("CORS_ALLOW_ORIGINS")
	if strings.TrimSpace(raw) == "" {
		raw = defaultCORSOrigins
	}
	allowed := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			allowed[o] = true
		}
	}

	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-Request-ID")
			h.Set("Access-Control-Expose-Headers", "X-Request-ID")
			h.Set("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func SecureHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "DENY")
		c.Writer.Header().Set("X-XSS-Protection", "0")
		c.Writer.Header().Set("Referrer-Policy", "no-referrer")
		c.Writer.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}

// BodyLimit caps request bodies; uploadPaths get a larger limit.
func BodyLimit(defaultMax int64, uploadMax int64, uploadPaths ...string) gin.HandlerFunc {
	uploads := map[string]bool{}
	for _, p := range uploadPaths {
		uploads[p] = true
	}
	return func(c *gin.Context) {
		limit := defaultMax
		if uploads[c.Request.URL.Path] {
			limit = uploadMax
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

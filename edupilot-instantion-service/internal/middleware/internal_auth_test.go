package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func internalRouter() *gin.Engine {
	r := gin.New()
	r.GET("/internal/assessment/principal", RequireInternalGateway(), func(c *gin.Context) { c.Status(200) })
	return r
}

func doInternal(r *gin.Engine, secret string) int {
	req := httptest.NewRequest(http.MethodGet, "/internal/assessment/principal", nil)
	if secret != "" {
		req.Header.Set("X-Gateway-Auth", secret)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRequireInternalGateway_EmptySecretFailsClosed(t *testing.T) {
	t.Setenv("GATEWAY_SECRET", "")
	r := internalRouter()
	for _, h := range []string{"", "anything"} {
		if code := doInternal(r, h); code != http.StatusUnauthorized {
			t.Errorf("empty secret, header %q: got %d want 401", h, code)
		}
	}
}

func TestRequireInternalGateway_Header(t *testing.T) {
	t.Setenv("GATEWAY_SECRET", "s3cret")
	r := internalRouter()
	if code := doInternal(r, ""); code != http.StatusUnauthorized {
		t.Errorf("missing header: %d", code)
	}
	if code := doInternal(r, "wrong"); code != http.StatusUnauthorized {
		t.Errorf("wrong header: %d", code)
	}
	if code := doInternal(r, "s3cret"); code != http.StatusOK {
		t.Errorf("valid header: %d", code)
	}
}

// Verified internal calls skip the global limiter before Redis is touched
// (config.RDB is nil in unit tests, so reaching Incr would panic).
func TestRateLimiterSkipsVerifiedInternalCalls(t *testing.T) {
	t.Setenv("RATE_LIMIT_ENABLED", "true")
	t.Setenv("GATEWAY_SECRET", "s3cret")
	r := gin.New()
	r.Use(GatewayAuthMiddleware())
	r.Use(RateLimiter(RateLimitOptions{Prefix: "rl:test", Requests: 1, Window: time.Minute, UseIPOnly: true}))
	r.GET("/internal/assessment/classes", func(c *gin.Context) { c.Status(200) })
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/internal/assessment/classes", nil)
		req.Header.Set("X-Gateway-Auth", "s3cret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("call %d: got %d", i, w.Code)
		}
	}
}

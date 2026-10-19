package middleware

import (
	"context"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"edupilot-gateway/internal/model"
	"edupilot-gateway/internal/service"

	"github.com/gin-gonic/gin"
)

// sensitiveQueryKeys are redacted from logged query strings (case-insensitive).
var sensitiveQueryKeys = map[string]struct{}{
	"token": {}, "access_token": {}, "refresh_token": {}, "id_token": {},
	"password": {}, "code": {}, "state": {}, "secret": {}, "client_secret": {},
	"otp": {}, "key": {}, "api_key": {}, "signature": {},
}

func isSensitiveKey(k string) bool {
	k = strings.ToLower(k)
	if _, ok := sensitiveQueryKeys[k]; ok {
		return true
	}
	return strings.Contains(k, "token") || strings.Contains(k, "password") || strings.Contains(k, "secret")
}

// RedactQuery returns the raw query with sensitive values replaced by
// "[REDACTED]". Unparseable queries are dropped entirely.
func RedactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[UNPARSEABLE]"
	}
	for k, vs := range values {
		if isSensitiveKey(k) {
			for i := range vs {
				vs[i] = "[REDACTED]"
			}
		}
	}
	return values.Encode()
}

// AsyncLogWriter persists request logs with a bounded worker pool. When the
// buffer is full, entries are dropped and counted rather than blocking requests.
type AsyncLogWriter struct {
	ch      chan *model.RequestLog
	svc     service.LogService
	wg      sync.WaitGroup
	dropped atomic.Uint64
	closed  atomic.Bool
}

func NewAsyncLogWriter(svc service.LogService, buffer, workers int) *AsyncLogWriter {
	w := &AsyncLogWriter{ch: make(chan *model.RequestLog, buffer), svc: svc}
	for i := 0; i < workers; i++ {
		w.wg.Add(1)
		go w.worker()
	}
	return w
}

func (w *AsyncLogWriter) worker() {
	defer w.wg.Done()
	for entry := range w.ch {
		if err := w.svc.SaveRequestLog(entry); err != nil {
			log.Printf("failed to save request log: %v", err)
		}
	}
}

// Enqueue never blocks.
func (w *AsyncLogWriter) Enqueue(entry *model.RequestLog) {
	if w.closed.Load() {
		return
	}
	select {
	case w.ch <- entry:
	default:
		n := w.dropped.Add(1)
		if n == 1 || n%1000 == 0 {
			log.Printf("request log buffer full; dropped %d entries so far", n)
		}
	}
}

// Dropped returns the number of dropped log entries.
func (w *AsyncLogWriter) Dropped() uint64 { return w.dropped.Load() }

// Close stops accepting entries and waits (up to ctx) for workers to drain.
// Call it only after the HTTP server has shut down (no handlers in flight).
func (w *AsyncLogWriter) Close(ctx context.Context) {
	if !w.closed.CompareAndSwap(false, true) {
		return
	}
	close(w.ch)
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("request log drain timed out")
	}
	if d := w.dropped.Load(); d > 0 {
		log.Printf("request logger dropped %d entries in total", d)
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// RequestLogger records method, path, redacted query, status, latency, client
// IP and user agent. It never records bodies or auth headers.
func RequestLogger(w *AsyncLogWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := RedactQuery(c.Request.URL.RawQuery)

		c.Next()

		w.Enqueue(&model.RequestLog{
			RequestID: truncate(c.GetString(requestIDKey), 128),
			Method:    truncate(c.Request.Method, 20),
			Path:      truncate(path, 255),
			Query:     truncate(query, 2048),
			Status:    c.Writer.Status(),
			LatencyMs: float64(time.Since(start).Nanoseconds()) / 1e6,
			IP:        truncate(c.ClientIP(), 100),
			UserAgent: truncate(c.Request.UserAgent(), 512),
			ErrorMsg:  truncate(c.Errors.ByType(gin.ErrorTypePrivate).String(), 1024),
		})
	}
}

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Headers clients must never be able to inject.
var strippedHeaders = []string{"X-Gateway-Auth", "X-Internal-Secret", "X-Real-IP"}

// NewTransport returns a pooled HTTP transport with sane timeouts, shared by
// all upstream proxies.
func NewTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   50,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewReverseProxy builds a reverse proxy to target once. It panics on an
// invalid target URL (configuration error at startup).
func NewReverseProxy(target, gatewaySecret string, transport http.RoundTripper) gin.HandlerFunc {
	targetURL, err := url.Parse(target)
	if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
		panic(fmt.Sprintf("invalid upstream URL %q: %v", target, err))
	}
	if gatewaySecret == "" {
		panic("gateway secret must not be empty")
	}

	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Rewrite has already removed hop-by-hop and X-Forwarded-* headers from Out.
			pr.SetURL(targetURL)
			pr.Out.Host = targetURL.Host

			for _, h := range strippedHeaders {
				pr.Out.Header.Del(h)
			}

			// X-Forwarded-For = direct client IP, X-Forwarded-Host = original Host,
			// X-Forwarded-Proto = http/https based on the inbound connection.
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)

			if ip := remoteIP(pr.In.RemoteAddr); ip != "" {
				pr.Out.Header.Set("X-Real-IP", ip)
			}
			pr.Out.Header.Set("X-Gateway-Auth", gatewaySecret)
			if rid := pr.In.Header.Get(RequestIDHeader); rid != "" {
				pr.Out.Header.Set(RequestIDHeader, rid)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// The gateway already set X-Request-ID on the response; avoid duplicates.
			// CORS headers from upstream are passed through untouched.
			resp.Header.Del(RequestIDHeader)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, context.Canceled) {
				// Client went away; nothing useful to send.
				w.WriteHeader(499)
				return
			}
			log.Printf("upstream error: method=%s path=%s upstream=%s request_id=%s err=%v",
				r.Method, r.URL.Path, targetURL.Host, r.Header.Get(RequestIDHeader), err)
			setErrorCORS(w, r)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(gin.H{
				"error": "upstream service unavailable",
				"code":  "UPSTREAM_UNAVAILABLE",
			})
		},
	}

	return func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// setErrorCORS echoes an allowlisted Origin on gateway-generated errors, so the
// browser can read the 502 body instead of reporting a CORS failure. Normal
// responses keep the upstream's CORS headers untouched.
func setErrorCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	allowed := os.Getenv("CORS_ALLOW_ORIGINS")
	if allowed == "" {
		allowed = "http://localhost:5173,http://localhost:5174"
	}
	for _, o := range strings.Split(allowed, ",") {
		if strings.TrimSpace(o) == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
			return
		}
	}
}

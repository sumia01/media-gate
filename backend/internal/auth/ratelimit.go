package auth

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type visitor struct {
	count       int
	windowStart time.Time
}

type ipLimiter struct {
	mu      sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Lazy cleanup: remove expired entries.
	for k, v := range l.visitors {
		if now.Sub(v.windowStart) > l.window {
			delete(l.visitors, k)
		}
	}

	v, ok := l.visitors[ip]
	if !ok || now.Sub(v.windowStart) > l.window {
		l.visitors[ip] = &visitor{count: 1, windowStart: now}
		return true
	}

	v.count++
	return v.count <= l.limit
}

// clientIP extracts the client IP from the request, checking proxy headers first.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first (leftmost) IP — that's the original client.
		if i := strings.IndexByte(xff, ','); i > 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimitMiddleware returns middleware that limits requests per IP using a fixed window.
func RateLimitMiddleware(limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := newIPLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !limiter.allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				if err := json.NewEncoder(w).Encode(map[string]any{
					"code":    429,
					"message": "too many requests, please try again later",
				}); err != nil {
					slog.Debug("failed to write rate limit response", "error", err)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

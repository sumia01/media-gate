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
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration

	// trustProxyHeaders gates whether clientIP honors X-Forwarded-For /
	// X-Real-IP. It defaults to false (see WithTrustProxyHeaders) and is
	// only ever written once, before RateLimitMiddleware starts serving
	// requests, so it is safe to read from request goroutines without a
	// lock.
	trustProxyHeaders bool
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

// clientIP extracts the client IP from the request. X-Forwarded-For and
// X-Real-IP are attacker-controlled request headers: any client can send an
// arbitrary value (or a fresh random one per request) unless a trusted
// reverse proxy sits in front of media-gate and overwrites/strips them
// before the request reaches this process. They are therefore only
// consulted when trustProxyHeaders is true; otherwise the key is derived
// solely from the TCP connection's RemoteAddr, which the client cannot
// forge.
func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
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
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr had no port (e.g. in unit tests or some listeners) —
		// use it as-is rather than dropping the address entirely.
		return r.RemoteAddr
	}
	return host
}

// LimiterOption configures optional behavior of the limiter created by
// RateLimitMiddleware.
type LimiterOption func(*ipLimiter)

// WithTrustProxyHeaders controls whether the rate limiter derives the
// client IP from the X-Forwarded-For / X-Real-IP headers instead of the
// connection's RemoteAddr. It defaults to false.
//
// Do NOT enable this unless media-gate is deployed behind a trusted reverse
// proxy that always sets (and strips any client-supplied copy of) these
// headers. Trusting them from arbitrary clients lets an attacker attach a
// unique X-Forwarded-For value to every request — e.g. to every login
// attempt — so each request lands in its own fresh rate-limit bucket and
// the limit never triggers, defeating brute-force protection entirely.
func WithTrustProxyHeaders(trust bool) LimiterOption {
	return func(l *ipLimiter) {
		l.trustProxyHeaders = trust
	}
}

// RateLimitMiddleware returns middleware that limits requests per IP using a fixed window.
// By default the client IP is taken solely from the connection's RemoteAddr; pass
// WithTrustProxyHeaders(true) to honor X-Forwarded-For/X-Real-IP when media-gate runs
// behind a trusted reverse proxy that sets those headers itself.
func RateLimitMiddleware(limit int, window time.Duration, opts ...LimiterOption) func(http.Handler) http.Handler {
	limiter := newIPLimiter(limit, window)
	for _, opt := range opts {
		opt(limiter)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, limiter.trustProxyHeaders)
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

package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name              string
		remoteAddr        string
		xff               string
		xri               string
		trustProxyHeaders bool
		want              string
	}{
		{
			name:       "default ignores spoofed X-Forwarded-For, uses RemoteAddr host",
			remoteAddr: "203.0.113.5:1234",
			xff:        "1.2.3.4",
			want:       "203.0.113.5",
		},
		{
			name:       "default ignores spoofed X-Real-IP, uses RemoteAddr host",
			remoteAddr: "203.0.113.5:1234",
			xri:        "9.9.9.9",
			want:       "203.0.113.5",
		},
		{
			name:              "trusted proxy honors leftmost X-Forwarded-For entry",
			remoteAddr:        "203.0.113.5:1234",
			xff:               "1.2.3.4, 203.0.113.5",
			trustProxyHeaders: true,
			want:              "1.2.3.4",
		},
		{
			name:              "trusted proxy falls back to X-Real-IP",
			remoteAddr:        "203.0.113.5:1234",
			xri:               "9.9.9.9",
			trustProxyHeaders: true,
			want:              "9.9.9.9",
		},
		{
			name:       "RemoteAddr without a port is used as-is",
			remoteAddr: "203.0.113.5",
			want:       "203.0.113.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			got := clientIP(req, tt.trustProxyHeaders)
			if got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRateLimitMiddleware_DefaultIgnoresSpoofedForwardedFor is the regression
// test for bug #15: without an explicit WithTrustProxyHeaders(true) opt-in,
// a client sending a unique X-Forwarded-For value on every request must
// still be rate-limited as a single visitor (keyed by RemoteAddr), not get a
// fresh bucket per request.
func TestRateLimitMiddleware_DefaultIgnoresSpoofedForwardedFor(t *testing.T) {
	var calls int
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	mw := RateLimitMiddleware(3, time.Minute)(next)

	const remoteAddr = "203.0.113.5:54321"
	var lastStatus int
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = remoteAddr
		// Attacker-controlled header: a different value on every request.
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		lastStatus = rec.Code
	}

	if calls != 3 {
		t.Fatalf("expected exactly 3 requests to reach the handler before the limit kicked in, got %d", calls)
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("expected the request past the limit to be rejected with 429, got status %d", lastStatus)
	}
}

// TestRateLimitMiddleware_TrustProxyHeadersOptIn verifies the opt-in escape
// hatch works as intended: when explicitly enabled (e.g. once media-gate is
// deployed behind a trusted reverse proxy), distinct X-Forwarded-For values
// do produce distinct buckets, so a shared front-door proxy doesn't collapse
// every real client into one rate-limit bucket.
func TestRateLimitMiddleware_TrustProxyHeadersOptIn(t *testing.T) {
	var calls int
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	mw := RateLimitMiddleware(3, time.Minute, WithTrustProxyHeaders(true))(next)

	const remoteAddr = "203.0.113.5:54321" // shared proxy address
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = remoteAddr
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 (distinct bucket per client IP), got %d", i, rec.Code)
		}
	}

	if calls != 5 {
		t.Fatalf("expected all 5 requests (distinct forwarded IPs) to reach the handler, got %d", calls)
	}
}

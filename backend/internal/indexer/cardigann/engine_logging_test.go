package cardigann

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Indexer search responses are raw provider payloads that routinely embed
// credentials (session tokens, RSS keys). Engine.Search must never write the
// response body into logs, while still emitting safe diagnostic metadata.
func TestEngine_Search_DoesNotLogResponseBody(t *testing.T) {
	const secret = "rss.php?key=SUPERSECRETTOKEN123"
	const bodyMarker = "secret-link-marker"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><a href="/%s">%s</a>`+
			`<table><tr><td><a href="/details.php?id=1">Some Release</a></td></tr></table></body></html>`,
			secret, bodyMarker)
	}))
	defer srv.Close()

	yamlDef := fmt.Sprintf(`
id: secret-body
name: Secret Body
links:
  - %s
search:
  paths:
    - path: browse.php
  rows:
    selector: table tr
  fields:
    title:
      selector: a
`, srv.URL)

	def, err := ParseDefinition([]byte(yamlDef))
	if err != nil {
		t.Fatalf("ParseDefinition failed: %v", err)
	}

	e, err := NewEngine(def, map[string]string{})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	e.loggedIn = true // skip login flow, not under test here

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	results, err := e.Search(context.Background(), SearchQuery{Q: "test"})
	if err != nil {
		t.Fatalf("Search returned an unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	logs := buf.String()
	if strings.Contains(logs, secret) {
		t.Fatalf("search response credential leaked into logs: %s", logs)
	}
	if strings.Contains(logs, bodyMarker) {
		t.Fatalf("raw search response body leaked into logs: %s", logs)
	}
	for _, field := range []string{"indexer search response", "duration_ms=", "final_url=", "content_encoding=",
		"content_length=", "indexer search parsed", "response_type=html", "rows_found=1", "results=1"} {
		if !strings.Contains(logs, field) {
			t.Errorf("expected logs to contain %q, got: %s", field, logs)
		}
	}
}

func TestEngine_Search_RedactsMalformedRedirects(t *testing.T) {
	const secret = "FAKE_REDIRECT_KEY"
	tests := []struct {
		name     string
		location string
		absolute bool
	}{
		{"absolute", "/invalid%zz?apikey=" + secret, true},
		{"root-relative", "/invalid%zz?apikey=" + secret, false},
		{"path-relative", "invalid%zz?apikey=" + secret, false},
		{"escaped-quotes", `/invalid%zz?apikey="` + secret + `"`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				location := tc.location
				if tc.absolute {
					location = "http://" + r.Host + location
				}
				w.Header().Set("Location", location)
				w.WriteHeader(http.StatusFound)
			}))
			defer srv.Close()

			e, err := NewEngine(&Definition{
				ID:     "malformed-redirect",
				Links:  []string{srv.URL},
				Search: Search{Paths: []SearchPath{{Path: "browse.php"}}},
			}, map[string]string{})
			if err != nil {
				t.Fatalf("NewEngine failed: %v", err)
			}
			_, err = e.Search(context.Background(), SearchQuery{Q: "test"})
			if err == nil {
				t.Fatal("expected malformed redirect to fail")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("redirect credential leaked in returned error: %v", err)
			}
			if !strings.Contains(err.Error(), "failed to parse Location header") {
				t.Errorf("redirect failure context lost: %v", err)
			}
			var urlErr *url.Error
			if !errors.As(err, &urlErr) {
				t.Error("HTTP error cause not preserved")
			}

			for _, format := range []string{"text", "json"} {
				t.Run(format, func(t *testing.T) {
					var logs bytes.Buffer
					var handler slog.Handler = slog.NewTextHandler(&logs, nil)
					if format == "json" {
						handler = slog.NewJSONHandler(&logs, nil)
					}
					slog.New(handler).Warn("indexer search failed", "error", err)
					if strings.Contains(logs.String(), secret) {
						t.Errorf("redirect credential leaked in warning log: %s", logs.String())
					}
				})
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"query", "https://tracker.example/browse.php?apikey=SECRET&q=x", "https://tracker.example/browse.php?[redacted]"},
		{"userinfo", "https://user:pass@tracker.example/path", "https://redacted@tracker.example/path"},
		{"fragment", "https://tracker.example/path#token=SECRET", "https://tracker.example/path#redacted"},
		{"plain", "https://tracker.example/path", "https://tracker.example/path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactURL(tc.in); got != tc.want {
				t.Errorf("redactURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactURLsInText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "absolute URLs",
			in:   `failed https://tracker.example/path?apikey=SECRET and https://user:pw@host/x`,
			want: `failed https://tracker.example/path?[redacted] and https://redacted@host/x`,
		},
		{
			name: "relative redirect",
			in:   `failed to parse Location header "/invalid%zz?apikey=SECRET": invalid URL escape "%zz"`,
			want: `failed to parse Location header "[invalid-url]": invalid URL escape "%zz"`,
		},
		{
			name: "query-only reference",
			in:   `redirect to "?apikey=SECRET" failed`,
			want: `redirect to "?[redacted]" failed`,
		},
		{
			name: "protocol-relative reference",
			in:   `redirect to "//user:pw@tracker.example/login?apikey=SECRET" failed`,
			want: `redirect to "//redacted@tracker.example/login?[redacted]" failed`,
		},
		{
			name: "escaped quotes in credential",
			in:   `redirect to "/login?apikey=\"SECRET\"" failed`,
			want: `redirect to "/login?[redacted]" failed`,
		},
		{
			name: "fragment-only reference",
			in:   `redirect to "#token=SECRET" failed`,
			want: `redirect to "#redacted" failed`,
		},
		{
			name: "non-URL diagnostic",
			in:   `invalid URL escape "%zz": "connection refused"`,
			want: `invalid URL escape "%zz": "connection refused"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactURLsInText(tc.in); got != tc.want {
				t.Errorf("redactURLsInText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRedactURLError_PreservesCause(t *testing.T) {
	raw := &url.Error{
		Op:  "Get",
		URL: "https://tracker.example/browse.php?apikey=SECRET",
		Err: context.Canceled,
	}

	err := redactURLError(raw)
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("credential leaked in error: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("underlying cause not preserved: %v", err)
	}
}

func TestRedactURLError_NonURLErrorPreservesCause(t *testing.T) {
	raw := fmt.Errorf("request to https://tracker.example/path?apikey=SECRET failed: %w", context.DeadlineExceeded)

	err := redactURLError(raw)
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("credential leaked in error: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("underlying cause not preserved: %v", err)
	}
}

func TestRedactURLError_NestedErrorsPreserveContextAndCauses(t *testing.T) {
	inner := &url.Error{
		Op:  "redirect",
		URL: "/next?apikey=INNER_SECRET",
		Err: context.DeadlineExceeded,
	}
	requestErr := &url.Error{
		Op:  "Get",
		URL: "https://tracker.example/browse.php?apikey=OUTER_SECRET",
		Err: inner,
	}
	raw := fmt.Errorf("indexer attempt failed: %w", requestErr)
	err := redactURLError(raw)
	for _, secret := range []string{"INNER_SECRET", "OUTER_SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("credential leaked in nested error: %v", err)
		}
	}
	if !strings.HasPrefix(err.Error(), "indexer attempt failed:") {
		t.Errorf("outer error context lost: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, inner) || !errors.Is(err, raw) {
		t.Error("original error chain not preserved")
	}
	var cause *url.Error
	if !errors.As(err, &cause) || !cause.Timeout() {
		t.Error("typed HTTP timeout cause not preserved")
	}
}

package tvdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestMaxSeasonNumber(t *testing.T) {
	tests := []struct {
		name    string
		seasons []SeasonEntry
		want    int
	}{
		{
			name:    "empty",
			seasons: nil,
			want:    0,
		},
		{
			name:    "single season",
			seasons: []SeasonEntry{{ID: 1, Number: 1}},
			want:    1,
		},
		{
			name: "with specials (season 0)",
			seasons: []SeasonEntry{
				{ID: 1, Number: 0},
				{ID: 2, Number: 1},
				{ID: 3, Number: 2},
			},
			want: 2,
		},
		{
			name: "specials inflate len but not max",
			seasons: []SeasonEntry{
				{ID: 1, Number: 0},
				{ID: 2, Number: 1},
			},
			want: 1,
		},
		{
			name: "unordered entries",
			seasons: []SeasonEntry{
				{ID: 3, Number: 3},
				{ID: 1, Number: 1},
				{ID: 2, Number: 2},
				{ID: 0, Number: 0},
			},
			want: 3,
		},
		{
			name: "duplicate season numbers from different orderings",
			seasons: []SeasonEntry{
				{ID: 1, Number: 0},
				{ID: 2, Number: 1},
				{ID: 3, Number: 1}, // e.g. DVD order duplicate
				{ID: 4, Number: 2},
			},
			want: 2,
		},
		{
			name:    "only specials",
			seasons: []SeasonEntry{{ID: 1, Number: 0}},
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &SeriesDetails{Seasons: tt.seasons}
			if got := d.MaxSeasonNumber(); got != tt.want {
				t.Errorf("MaxSeasonNumber() = %d, want %d", got, tt.want)
			}
		})
	}
}

// newLoginHandler returns an http.HandlerFunc that answers TVDB's /login
// endpoint, issuing a fresh, incrementing token on every call and tracking
// how many times it was invoked.
func newLoginHandler(calls *int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{"token": fmt.Sprintf("token-%d", n)},
		})
	}
}

// TestGetReauthenticatesOn401 proves that get() transparently re-authenticates
// and retries exactly once when the API responds with 401 (expired token),
// and that the retried request succeeds using the fresh token.
func TestGetReauthenticatesOn401(t *testing.T) {
	var loginCalls int32
	var searchCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/login", newLoginHandler(&loginCalls))
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&searchCalls, 1)
		auth := r.Header.Get("Authorization")

		if n == 1 {
			// Stale token: simulate an expired-token 401.
			if auth != "Bearer stale-token" {
				t.Errorf("first attempt: unexpected Authorization header %q", auth)
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Retry after re-authentication must use the freshly issued token,
		// not the stale one from before the 401.
		if auth != "Bearer token-1" {
			t.Errorf("retry attempt: unexpected Authorization header %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []SeriesResult{{TVDBID: "123", Name: "Test Show"}},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := &Client{
		baseURL:    server.URL,
		apiKey:     "test-key",
		httpClient: server.Client(),
	}
	// Seed a stale token so ensureAuthenticated() doesn't itself trigger the
	// first login; the 401-driven re-auth path is what we're testing. It's
	// deliberately distinct from the "token-N" values the login handler
	// issues so a successful re-auth is unambiguous.
	c.token = "stale-token"

	results, err := c.SearchSeries("Test", nil)
	if err != nil {
		t.Fatalf("SearchSeries() error = %v", err)
	}
	if len(results) != 1 || results[0].Name != "Test Show" {
		t.Fatalf("unexpected results: %+v", results)
	}
	if got := atomic.LoadInt32(&loginCalls); got != 1 {
		t.Errorf("expected exactly 1 re-authentication call, got %d", got)
	}
	if got := atomic.LoadInt32(&searchCalls); got != 2 {
		t.Errorf("expected exactly 2 search calls (401 then retry), got %d", got)
	}
	if c.token != "token-1" {
		t.Errorf("expected client to retain the fresh token, got %q", c.token)
	}
}

// TestGetGivesUpAfterSingleRetry proves that get() retries at most once on a
// 401 — if the retried request also comes back 401, get() must return an
// error instead of looping indefinitely.
func TestGetGivesUpAfterSingleRetry(t *testing.T) {
	var loginCalls int32
	var searchCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/login", newLoginHandler(&loginCalls))
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&searchCalls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := &Client{
		baseURL:    server.URL,
		apiKey:     "test-key",
		httpClient: server.Client(),
	}
	c.token = "stale-token"

	_, err := c.SearchSeries("Test", nil)
	if err == nil {
		t.Fatal("expected an error when the retried request also returns 401, got nil")
	}
	if got := atomic.LoadInt32(&loginCalls); got != 1 {
		t.Errorf("expected exactly 1 re-authentication attempt, got %d", got)
	}
	if got := atomic.LoadInt32(&searchCalls); got != 2 {
		t.Errorf("expected exactly 2 search calls (original + single retry), got %d", got)
	}
}

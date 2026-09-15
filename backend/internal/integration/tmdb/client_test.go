package tmdb

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// failingRoundTripper simulates a transport-level failure (DNS/timeout/EOF),
// which is what net/http wraps in a *url.Error whose Error() string embeds
// the full request URL — including any query parameters.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("simulated transport failure")
}

func TestGetWithParams_TransportErrorDoesNotLeakAPIKey(t *testing.T) {
	const secretKey = "super-secret-tmdb-key-1234567890"

	c := NewClient(secretKey, &http.Client{Transport: failingRoundTripper{}})

	_, err := c.getWithParams("/movie/123", nil)
	if err == nil {
		t.Fatal("expected an error from the failing transport, got nil")
	}

	if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("error message leaks the API key: %q", err.Error())
	}
}

func TestAPIErrorDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"status_message":"private provider response"}`)
	}))
	defer server.Close()

	client := NewClient("test-key", server.Client())
	client.baseURL = server.URL
	_, err := client.GetPerson(999)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "private provider response") {
		t.Fatalf("provider response leaked through error: %q", err)
	}
}

func TestRedact(t *testing.T) {
	c := &Client{apiKey: "abc123"}

	t.Run("nil error passes through", func(t *testing.T) {
		if got := c.redact(nil); got != nil {
			t.Errorf("redact(nil) = %v, want nil", got)
		}
	})

	t.Run("strips raw key", func(t *testing.T) {
		err := errors.New(`Get "https://api.themoviedb.org/3/movie/1?api_key=abc123": dial tcp: timeout`)
		got := c.redact(err)
		if strings.Contains(got.Error(), "abc123") {
			t.Errorf("redact() left the key in place: %q", got.Error())
		}
		if !strings.Contains(got.Error(), "REDACTED") {
			t.Errorf("redact() did not mark the redaction: %q", got.Error())
		}
	})

	t.Run("no key present leaves message untouched", func(t *testing.T) {
		err := errors.New("some unrelated error")
		got := c.redact(err)
		if got.Error() != err.Error() {
			t.Errorf("redact() = %q, want %q", got.Error(), err.Error())
		}
	})

	t.Run("empty api key is a no-op", func(t *testing.T) {
		empty := &Client{apiKey: ""}
		err := errors.New("boom")
		if got := empty.redact(err); got != err {
			t.Errorf("redact() with empty key should return the original error unchanged")
		}
	})
}

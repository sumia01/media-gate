package apiv1

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func posterRequest(t *testing.T, h http.HandlerFunc, id, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/"+id+"/poster", nil)
	req.SetPathValue("id", id)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func writePoster(t *testing.T, dir, name string, content []byte, mod time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing poster: %v", err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("setting mtime: %v", err)
	}
	return path
}

func TestPosterHandlerServesWithValidator(t *testing.T) {
	dir := t.TempDir()
	body := []byte("first-poster-bytes")
	writePoster(t, dir, "1.jpg", body, time.Unix(1_700_000_000, 0))

	h := (&Handlers{posterDir: dir}).PosterHandler()
	rec := posterRequest(t, h, "1", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != string(body) {
		t.Errorf("body = %q, want %q", got, body)
	}
	// The whole point of the switch: the browser must revalidate rather than
	// pin the response for a fixed window.
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-cache")
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("ETag not set — revalidation would have no validator to compare")
	}
}

func TestPosterHandlerReturns304ForUnchangedPoster(t *testing.T) {
	dir := t.TempDir()
	writePoster(t, dir, "1.jpg", []byte("first-poster-bytes"), time.Unix(1_700_000_000, 0))

	h := (&Handlers{posterDir: dir}).PosterHandler()
	etag := posterRequest(t, h, "1", "").Header().Get("ETag")

	rec := posterRequest(t, h, "1", etag)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 must not carry a body, got %d bytes", rec.Body.Len())
	}
}

// The regression this whole change exists to prevent: a re-matched poster must
// replace the one the browser is holding. Under the old max-age the client
// never even asked; now the stale validator forces a full response.
func TestPosterHandlerServesReplacedPosterToStaleValidator(t *testing.T) {
	dir := t.TempDir()
	writePoster(t, dir, "1.jpg", []byte("first-poster-bytes"), time.Unix(1_700_000_000, 0))

	h := (&Handlers{posterDir: dir}).PosterHandler()
	staleETag := posterRequest(t, h, "1", "").Header().Get("ETag")

	// A re-match installs different artwork via os.Rename, advancing mtime.
	replaced := []byte("second-poster-bytes-after-rematch")
	writePoster(t, dir, "1.jpg", replaced, time.Unix(1_700_009_999, 0))

	rec := posterRequest(t, h, "1", staleETag)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the stale poster would stay pinned", rec.Code)
	}
	if got := rec.Body.String(); got != string(replaced) {
		t.Errorf("body = %q, want the replacement %q", got, replaced)
	}
	if newETag := rec.Header().Get("ETag"); newETag == staleETag {
		t.Error("ETag did not change after the poster was replaced")
	}
}

// A same-size replacement must still invalidate: the ETag carries mtime, not
// just length.
func TestPosterHandlerValidatorChangesOnSameSizeReplacement(t *testing.T) {
	dir := t.TempDir()
	writePoster(t, dir, "1.jpg", []byte("aaaaaaaaaaaa"), time.Unix(1_700_000_000, 0))

	h := (&Handlers{posterDir: dir}).PosterHandler()
	staleETag := posterRequest(t, h, "1", "").Header().Get("ETag")

	writePoster(t, dir, "1.jpg", []byte("bbbbbbbbbbbb"), time.Unix(1_700_009_999, 0))

	rec := posterRequest(t, h, "1", staleETag)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a same-size replacement", rec.Code)
	}
	if got := rec.Body.String(); got != "bbbbbbbbbbbb" {
		t.Errorf("body = %q, want the replacement", got)
	}
}

// Zero-byte files are leftovers from the pre-atomic-write era; serving one
// would hand the browser a broken image with a 200.
func TestPosterHandlerRejectsZeroByteposter(t *testing.T) {
	dir := t.TempDir()
	writePoster(t, dir, "1.jpg", nil, time.Unix(1_700_000_000, 0))

	rec := posterRequest(t, (&Handlers{posterDir: dir}).PosterHandler(), "1", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a zero-byte poster", rec.Code)
	}
}

func TestPosterHandlerMissingPoster(t *testing.T) {
	rec := posterRequest(t, (&Handlers{posterDir: t.TempDir()}).PosterHandler(), "42", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

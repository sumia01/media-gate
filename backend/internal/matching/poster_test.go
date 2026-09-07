package matching

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

// jpeg builds a syntactically complete JPEG of the requested payload size.
func jpeg(payload int) []byte {
	b := []byte{0xFF, 0xD8}
	b = append(b, bytes.Repeat([]byte{0x42}, payload)...)
	return append(b, 0xFF, 0xD9)
}

func TestDownloadPosterWritesCompleteImage(t *testing.T) {
	want := jpeg(1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(want)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "posters", "1.jpg")
	if err := downloadPoster(srv.Client(), srv.URL, dest); err != nil {
		t.Fatalf("downloadPoster: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading poster: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("poster contents differ: got %d bytes, want %d", len(got), len(want))
	}

	// os.CreateTemp defaults to 0600; the installed poster must keep the 0644
	// that os.Create used to produce.
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat poster: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("poster mode = %v, want -rw-r--r--", got)
	}
}

// A body that stops mid-image must be rejected, and the poster already on disk
// must survive untouched — the old code truncated the destination with
// os.Create before the first byte arrived, so a failure destroyed good artwork.
//
// Chunked deliberately: with no Content-Length there is nothing but the EOI
// marker to prove the body arrived whole, which is the case the trailer check
// exists for. A length-verified body missing its trailer is kept instead (see
// TestDownloadPosterKeepsVerifiedBodyWithTrailingPadding).
func TestDownloadPosterRejectsTruncatedBodyAndKeepsExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Complete JPEG header, no EOI marker: the shape of a cut-off download.
		w.Header().Set("Transfer-Encoding", "chunked")
		_, _ = w.Write(append([]byte{0xFF, 0xD8}, bytes.Repeat([]byte{0x42}, 512)...))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "1.jpg")
	existing := jpeg(256)
	if err := os.WriteFile(dest, existing, 0o644); err != nil {
		t.Fatalf("seeding poster: %v", err)
	}

	err := downloadPoster(srv.Client(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected an error for a truncated JPEG, got nil")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %v, want it to mention truncation", err)
	}

	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("reading poster: %v", readErr)
	}
	if !bytes.Equal(got, existing) {
		t.Errorf("existing poster was clobbered: got %d bytes, want %d", len(got), len(existing))
	}

	// The temp file must not be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".poster-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// A body shorter than its declared Content-Length is caught by the transport,
// which surfaces io.ErrUnexpectedEOF from io.Copy before the explicit
// length comparison is ever reached. Asserting the message keeps this test
// honest about which guard actually fires.
func TestDownloadPosterRejectsShortContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Claim more than we send; the handler's short write is what a dropped
		// connection looks like to the client.
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write(jpeg(64))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "1.jpg")
	err := downloadPoster(srv.Client(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected an error for a short body, got nil")
	}
	if !strings.Contains(err.Error(), "writing poster") {
		t.Errorf("error = %v, want the io.Copy guard to reject it", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination should not exist after a failed download, stat err = %v", err)
	}
}

// An oversized body that announces itself must be refused before a single byte
// is written to disk.
func TestDownloadPosterRejectsDeclaredOversizeWithoutWriting(t *testing.T) {
	var served bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.Header().Set("Content-Length", "20971520") // 20 MiB
		_, _ = w.Write(jpeg(64))
	}))
	defer srv.Close()

	dir := t.TempDir()
	err := downloadPoster(srv.Client(), srv.URL, filepath.Join(dir, "1.jpg"))
	if err == nil {
		t.Fatal("expected an error for a declared-oversize body, got nil")
	}
	if !strings.Contains(err.Error(), "declared") {
		t.Errorf("error = %v, want the pre-write size guard to reject it", err)
	}
	if !served {
		t.Error("handler was never reached")
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("reading dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("no file should have been written, found %d entries", len(entries))
	}
}

// A truncated PNG is the same defect as a truncated JPEG; it must not be
// installed just because it carries a different magic number.
func TestDownloadPosterRejectsTruncatedPNG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Transfer-Encoding", "chunked")
		_, _ = w.Write(append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x11}, 256)...))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "1.jpg")
	err := downloadPoster(srv.Client(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected an error for a truncated PNG, got nil")
	}
	if !strings.Contains(err.Error(), "PNG") {
		t.Errorf("error = %v, want it to name the PNG end marker", err)
	}
}

// A JPEG with padding after EOI is complete, just unusual. When Content-Length
// already proved every byte arrived, it must be kept rather than discarded.
func TestDownloadPosterKeepsVerifiedBodyWithTrailingPadding(t *testing.T) {
	padded := append(jpeg(256), 0x00, 0x00, 0x0A)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(padded)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "1.jpg")
	if err := downloadPoster(srv.Client(), srv.URL, dest); err != nil {
		t.Fatalf("padded but length-verified poster was rejected: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading poster: %v", err)
	}
	if !bytes.Equal(got, padded) {
		t.Errorf("poster contents differ: got %d bytes, want %d", len(got), len(padded))
	}
}

func TestDownloadPosterRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte{0x42}, 1<<20)
		for range 11 {
			_, _ = w.Write(chunk)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "1.jpg")
	err := downloadPoster(srv.Client(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected an error for an oversized body, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want it to mention the size cap", err)
	}
}

// The bug behind the partially rendered thumbnails: a reader that opens the
// poster while a download is in flight must never observe a partial file.
func TestDownloadPosterIsAtomicForConcurrentReaders(t *testing.T) {
	body := jpeg(512 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Dribble the body out so a reader has a wide window to catch a
		// partial write, exactly as a real CDN transfer does.
		for off := 0; off < len(body); off += 4096 {
			end := min(off+4096, len(body))
			_, _ = w.Write(body[off:end])
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "1.jpg")
	seed := jpeg(1024)
	if err := os.WriteFile(dest, seed, 0o644); err != nil {
		t.Fatalf("seeding poster: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	bad := make(chan int, 64)

	// Hammer the file the way PosterHandler would while the download runs.
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				got, err := os.ReadFile(dest)
				if err != nil {
					continue // mid-rename open is fine; a partial read is not
				}
				if !bytes.Equal(got, seed) && !bytes.Equal(got, body) {
					select {
					case bad <- len(got):
					default:
					}
					return
				}
			}
		})
	}

	if err := downloadPoster(srv.Client(), srv.URL, dest); err != nil {
		t.Fatalf("downloadPoster: %v", err)
	}
	close(stop)
	wg.Wait()
	close(bad)

	for n := range bad {
		t.Fatalf("reader observed a partial poster of %d bytes (want %d or %d)", n, len(seed), len(body))
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading poster: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("final poster differs: got %d bytes, want %d", len(got), len(body))
	}
}

// Non-JPEG artwork (TVDB serves some PNGs) must still install cleanly — the
// EOI check applies only to payloads that announce themselves as JPEG.
func TestDownloadPosterAcceptsNonJPEG(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x11}, 256)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(png)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "1.jpg")
	if err := downloadPoster(srv.Client(), srv.URL, dest); err != nil {
		t.Fatalf("downloadPoster: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading poster: %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Errorf("poster contents differ: got %d bytes, want %d", len(got), len(png))
	}
}

func TestDownloadPosterNonOKStatusKeepsExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "1.jpg")
	existing := jpeg(128)
	if err := os.WriteFile(dest, existing, 0o644); err != nil {
		t.Fatalf("seeding poster: %v", err)
	}

	if err := downloadPoster(srv.Client(), srv.URL, dest); err == nil {
		t.Fatal("expected an error for HTTP 429, got nil")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading poster: %v", err)
	}
	if !bytes.Equal(got, existing) {
		t.Errorf("existing poster was clobbered on a failed download")
	}
}

func TestCommittedRematchClearsOldPosterWhenReplacementUnavailable(t *testing.T) {
	for _, test := range []struct {
		name       string
		posterPath func(*httptest.Server) string
	}{
		{name: "missing URL", posterPath: func(*httptest.Server) string { return "" }},
		{name: "failed download", posterPath: func(server *httptest.Server) string { return server.URL }},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "failed", http.StatusBadGateway)
			}))
			defer server.Close()
			st := newMemStore()
			item := &store.MediaItem{LibraryID: 1, Title: "Poster", MediaType: "movie", Source: "disk"}
			if err := st.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			meta := &store.MediaMetadata{MediaItemID: item.ID, Source: "tvdb", ExternalID: 123, Title: item.Title, PosterPath: test.posterPath(server), UpdatedAt: time.Unix(10, 0)}
			if err := st.CreateMediaMetadata(meta); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			dest := filepath.Join(dir, "1.jpg")
			if err := os.WriteFile(dest, jpeg(128), 0o644); err != nil {
				t.Fatal(err)
			}

			NewService(st, nil, dir, server.Client()).downloadPosterForCommitted(item.ID, meta, "replacement")
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatalf("old poster remains after unavailable replacement: %v", err)
			}
		})
	}
}

func TestCommittedRematchDoesNotRemovePosterForStaleMetadata(t *testing.T) {
	st := newMemStore()
	item := &store.MediaItem{LibraryID: 1, Title: "Poster", MediaType: "movie", Source: "disk"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	expected := &store.MediaMetadata{MediaItemID: item.ID, Source: "tvdb", ExternalID: 123, Title: item.Title, UpdatedAt: time.Unix(10, 0)}
	if err := st.CreateMediaMetadata(expected); err != nil {
		t.Fatal(err)
	}
	current, _ := st.GetMediaMetadataByMediaItem(item.ID)
	current.UpdatedAt = current.UpdatedAt.Add(time.Second)
	if err := st.UpdateMediaMetadata(current); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "1.jpg")
	want := jpeg(128)
	if err := os.WriteFile(dest, want, 0o644); err != nil {
		t.Fatal(err)
	}

	NewService(st, nil, dir, http.DefaultClient).downloadPosterForCommitted(item.ID, expected, "stale")
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("stale rematch removed current poster: err=%v", err)
	}
}

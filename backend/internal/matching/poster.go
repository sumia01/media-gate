package matching

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxPosterBytes caps a poster download. A body larger than this is rejected
// outright rather than silently truncated — a half-written JPEG renders as a
// partially drawn image instead of failing visibly.
const maxPosterBytes = 10 << 20

// downloadPoster fetches url and installs it at destPath atomically: the body is
// streamed into a temp file in the same directory, validated, and only then
// renamed over the destination. PosterHandler therefore always observes either
// the previous complete poster or the new complete one, never the partially
// written bytes of an in-flight download — writing straight to destPath meant a
// concurrent request could be served (and the browser could then cache for a
// day) whatever prefix had landed so far. A failed or short download now leaves
// the existing poster untouched instead of destroying it.
func downloadPoster(httpClient *http.Client, url, destPath string) error {
	if url == "" {
		return nil
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating poster dir: %w", err)
	}

	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("downloading poster: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("poster download returned %d", resp.StatusCode)
	}

	// Bail before writing anything when the server already told us the body is
	// too big, instead of streaming 10 MiB to disk only to discard it.
	if resp.ContentLength > maxPosterBytes {
		return fmt.Errorf("poster exceeds %d bytes (declared %d)", maxPosterBytes, resp.ContentLength)
	}

	tmp, err := os.CreateTemp(dir, ".poster-*")
	if err != nil {
		return fmt.Errorf("creating poster temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op once the rename below has succeeded
	}()

	// Read one byte past the cap so an oversized body surfaces as an error
	// instead of a silently truncated image.
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxPosterBytes+1))
	if err != nil {
		return fmt.Errorf("writing poster: %w", err)
	}
	if n > maxPosterBytes {
		return fmt.Errorf("poster exceeds %d bytes", maxPosterBytes)
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return fmt.Errorf("poster truncated: got %d of %d bytes", n, resp.ContentLength)
	}

	// A matched Content-Length means the transport already proved every
	// announced byte arrived, so a missing format trailer is odd source data
	// (padding past the end marker), not truncation — log it and keep the
	// image. Without a Content-Length the trailer is the only completeness
	// signal there is, so treat a mismatch as fatal.
	lengthVerified := resp.ContentLength >= 0 && n == resp.ContentLength
	if err := verifyPosterComplete(tmp, n); err != nil {
		if !lengthVerified {
			return err
		}
		slog.Warn("poster trailer check failed but body length was verified; keeping image",
			"url", url, "bytes", n, "error", err)
	}

	// os.CreateTemp makes the file 0600; posters were 0644 under os.Create, so
	// restore that rather than silently tightening the mode on every new file.
	// Cosmetic — a filesystem that refuses chmod (some CIFS/SMB mounts) must not
	// cost us an otherwise good poster.
	if err := tmp.Chmod(0o644); err != nil {
		slog.Warn("could not set poster file mode", "path", destPath, "error", err)
	}

	// Flush before the rename so a crash cannot publish a poster whose contents
	// were never persisted.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("syncing poster: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing poster: %w", err)
	}

	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("installing poster: %w", err)
	}

	return nil
}

// CleanupPosterTemps removes ".poster-*" scratch files stranded in dir. The
// deferred cleanup in downloadPoster covers every normal return, but a SIGKILL
// or OOM kill mid-download leaves one behind, and on a long-lived self-hosted
// service those would accumulate. Call once at startup.
func CleanupPosterTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // no poster dir yet — nothing to sweep
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), ".poster-") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			slog.Warn("could not remove stale poster temp file", "path", path, "error", err)
		}
	}
}

// Trailers that mark a structurally complete image: JPEG's EOI marker and
// PNG's IEND chunk (type + its fixed CRC).
var imageTrailers = []struct {
	name    string
	magic   []byte
	trailer []byte
}{
	{"JPEG", []byte{0xFF, 0xD8}, []byte{0xFF, 0xD9}},
	{"PNG", []byte{0x89, 'P', 'N', 'G'}, []byte{'I', 'E', 'N', 'D', 0xAE, 0x42, 0x60, 0x82}},
}

// verifyPosterComplete reports whether the payload ends the way its format says
// a complete image should. Chunked and HTTP/2 responses carry no Content-Length,
// so the byte count alone cannot prove the body arrived whole. A format we do
// not recognise has no cheap trailer to check and passes.
func verifyPosterComplete(f *os.File, size int64) error {
	if size < 16 {
		return fmt.Errorf("poster too small: %d bytes", size)
	}

	head := make([]byte, 4)
	if _, err := f.ReadAt(head, 0); err != nil {
		return fmt.Errorf("reading poster header: %w", err)
	}

	for _, format := range imageTrailers {
		if !bytes.HasPrefix(head, format.magic) {
			continue
		}
		tail := make([]byte, len(format.trailer))
		if _, err := f.ReadAt(tail, size-int64(len(tail))); err != nil {
			return fmt.Errorf("reading poster trailer: %w", err)
		}
		if !bytes.Equal(tail, format.trailer) {
			return fmt.Errorf("poster truncated: %s end marker missing", format.name)
		}
		return nil
	}

	return nil // unrecognised format — nothing cheap left to verify
}

package sqlite

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustCreateMediaItem(t *testing.T, s *SQLiteStore) *store.MediaItem {
	t.Helper()
	lib := &store.Library{Name: "lib", Path: t.TempDir(), MediaType: "series"}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: "series", Status: "new", Source: "disk"}
	if err := s.CreateMediaItem(item); err != nil {
		t.Fatalf("CreateMediaItem: %v", err)
	}
	return item
}

// TestUpdateOnDeletedRowReturnsNotFound proves the save() helper returns
// store.ErrNotFound when updating a row that was concurrently deleted, and does
// NOT re-INSERT (resurrect) it. This is the regression guard for gorm's
// Save-as-upsert fallback.
func TestUpdateOnDeletedRowReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)

	// Caller holds a stale struct across a long operation; row gets deleted.
	stale := *item
	if err := s.DeleteMediaItem(item.ID); err != nil {
		t.Fatalf("DeleteMediaItem: %v", err)
	}

	stale.Title = "Renamed"
	err := s.UpdateMediaItem(&stale)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateMediaItem on deleted row: got err %v, want store.ErrNotFound", err)
	}

	// The row must NOT have been resurrected.
	if _, err := s.GetMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("row was resurrected: GetMediaItem returned %v, want store.ErrNotFound", err)
	}
}

// TestUpdateWritesZeroValueFields proves Select("*") preserves the full-write
// semantics of the old Save: zero-value fields (e.g. clearing a string column)
// are written, not skipped. Regression guard for download.Reconcile clearing
// ClientTorrentHash.
func TestUpdateWritesZeroValueFields(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)

	dl := &store.Download{
		MediaItemID:       item.ID,
		IndexerID:         1,
		IndexerName:       "idx",
		Title:             "Show S01E01",
		DownloadURL:       "magnet:?x",
		Status:            "downloading",
		ClientTorrentHash: "deadbeef",
	}
	if err := s.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload: %v", err)
	}

	got, err := s.GetDownload(dl.ID)
	if err != nil {
		t.Fatalf("GetDownload: %v", err)
	}
	got.ClientTorrentHash = "" // intentional field-clearing
	if err := s.UpdateDownload(got); err != nil {
		t.Fatalf("UpdateDownload: %v", err)
	}

	reread, err := s.GetDownload(dl.ID)
	if err != nil {
		t.Fatalf("GetDownload (reread): %v", err)
	}
	if reread.ClientTorrentHash != "" {
		t.Fatalf("zero-value field not written: ClientTorrentHash = %q, want empty", reread.ClientTorrentHash)
	}
}

// TestUpdateExistingRowSucceeds sanity-checks the happy path still updates.
func TestUpdateExistingRowSucceeds(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)

	item.Title = "Renamed"
	if err := s.UpdateMediaItem(item); err != nil {
		t.Fatalf("UpdateMediaItem: %v", err)
	}
	got, err := s.GetMediaItem(item.ID)
	if err != nil {
		t.Fatalf("GetMediaItem: %v", err)
	}
	if got.Title != "Renamed" {
		t.Fatalf("title not updated: got %q, want %q", got.Title, "Renamed")
	}
}

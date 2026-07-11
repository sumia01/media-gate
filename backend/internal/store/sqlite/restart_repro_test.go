package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// TestMediaItemFieldsSurviveRestart guards against the glebarez AutoMigrate
// rebuild dropping ALTER-added columns (monitor_new_seasons via V3,
// preferred_release via V9) on restart. Their values must persist across
// reopening the DB, not reset to the column default. Regression test for the
// data-loss bug fixed by normalizeMediaItemsSchema + DisableForeignKey.
func TestMediaItemFieldsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// --- Boot 1: create item, set non-default values on both ALTER-added columns. ---
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 1): %v", err)
	}
	lib := &store.Library{Name: "lib", Path: dir, MediaType: "series"}
	if err := s1.CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Silo", MediaType: "series", Status: "new", Source: "disk"}
	if err := s1.CreateMediaItem(item); err != nil {
		t.Fatalf("CreateMediaItem: %v", err)
	}
	item.PreferredRelease = "ETHEL"
	item.MonitorNewSeasons = false // non-default, so a reset-to-default is detectable
	if err := s1.UpdateMediaItem(item); err != nil {
		t.Fatalf("UpdateMediaItem: %v", err)
	}
	_ = s1.Close()

	// --- Boot 2 (restart): reopen the same DB file (AutoMigrate + migrations run again). ---
	s2, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 2): %v", err)
	}
	defer s2.Close()
	got, err := s2.GetMediaItem(item.ID)
	if err != nil {
		t.Fatalf("GetMediaItem (boot 2): %v", err)
	}
	if got.PreferredRelease != "ETHEL" {
		t.Errorf("preferred_release lost on restart: got %q, want %q", got.PreferredRelease, "ETHEL")
	}
	if got.MonitorNewSeasons != false {
		t.Errorf("monitor_new_seasons lost on restart: got %v, want false", got.MonitorNewSeasons)
	}
}

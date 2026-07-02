package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// fakeStore is a minimal in-memory store.Store used by the sync tests. It embeds
// the store.Store interface so unimplemented methods panic if unexpectedly called;
// only the methods exercised by SyncLibrary/findOrphanedMediaItems are provided.
type fakeStore struct {
	store.Store

	items map[uint]*store.MediaItem
	files []store.MediaFile

	deletedItems     []uint
	deletedMeta      []uint
	deletedEpisodes  []uint
	deletedFilePaths []string
	nextID           uint
}

func (f *fakeStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if it, ok := f.items[id]; ok {
		return it, nil
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) ListMediaFilesByLibrary(uint) ([]store.MediaFile, error) {
	return f.files, nil
}

func (f *fakeStore) DeleteMediaFilesByPaths(paths []string) error {
	f.deletedFilePaths = append(f.deletedFilePaths, paths...)
	rm := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		rm[p] = struct{}{}
	}
	kept := f.files[:0]
	for _, mf := range f.files {
		if _, drop := rm[mf.Path]; !drop {
			kept = append(kept, mf)
		}
	}
	f.files = kept
	return nil
}

func (f *fakeStore) DeleteMediaMetadataByMediaItem(id uint) error {
	f.deletedMeta = append(f.deletedMeta, id)
	return nil
}

func (f *fakeStore) DeleteEpisodesByMediaItem(id uint) error {
	f.deletedEpisodes = append(f.deletedEpisodes, id)
	return nil
}

func (f *fakeStore) DeleteMediaItem(id uint) error {
	f.deletedItems = append(f.deletedItems, id)
	delete(f.items, id)
	return nil
}

func (f *fakeStore) CreateMediaItem(item *store.MediaItem) error {
	f.nextID++
	item.ID = f.nextID
	if f.items == nil {
		f.items = make(map[uint]*store.MediaItem)
	}
	f.items[item.ID] = item
	return nil
}

func (f *fakeStore) CreateMediaFile(file *store.MediaFile) error {
	f.files = append(f.files, *file)
	return nil
}

// TestScanMediaFolderDistinguishesErrorFromEmpty verifies that a genuinely empty
// folder returns no error, while an unreadable/nonexistent folder returns an error
// (the two must be distinguishable so callers never treat an I/O error as "empty").
func TestScanMediaFolderDistinguishesErrorFromEmpty(t *testing.T) {
	empty := t.TempDir()
	files, err := scanMediaFolder(empty)
	if err != nil {
		t.Fatalf("empty folder should not return an error, got: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("empty folder should yield 0 files, got %d", len(files))
	}

	_, err = scanMediaFolder(filepath.Join(empty, "does-not-exist"))
	if err == nil {
		t.Fatal("nonexistent folder must return an error, not an empty scan")
	}
}

// TestFindOrphanedMediaItemsSkipsRequestAndPending verifies that only successfully
// scanned, genuinely-empty disk items are reported as orphans; request items and
// items in a not-yet-downloaded status are protected from deletion.
func TestFindOrphanedMediaItemsSkipsRequestAndPending(t *testing.T) {
	fs := &fakeStore{items: map[uint]*store.MediaItem{
		1: {ID: 1, Source: "disk", Status: "available"},    // real orphan → deletable
		2: {ID: 2, Source: "request", Status: "requested"}, // request → protected
		3: {ID: 3, Source: "disk", Status: "pending"},      // pending → protected
	}}
	svc := NewService(fs)

	allFiles := []store.MediaFile{
		{Path: "/lib/A/a.mkv", MediaItemID: 1},
		{Path: "/lib/B/b.mkv", MediaItemID: 2},
		{Path: "/lib/C/c.mkv", MediaItemID: 3},
	}
	removed := []string{"/lib/A/a.mkv", "/lib/B/b.mkv", "/lib/C/c.mkv"}
	pathToItem := map[string]uint{
		"/lib/A/a.mkv": 1,
		"/lib/B/b.mkv": 2,
		"/lib/C/c.mkv": 3,
	}

	orphans := svc.findOrphanedMediaItems(removed, allFiles, pathToItem)
	if len(orphans) != 1 || orphans[0] != 1 {
		t.Fatalf("expected only disk item 1 as orphan, got %v", orphans)
	}
}

// TestSyncLibraryDoesNotDeleteItemWhenFolderUnreadable is the regression test for
// the confirmed bug: a transient I/O error on a media folder must not be treated
// as "all files removed" and must never hard-delete the media item.
func TestSyncLibraryDoesNotDeleteItemWhenFolderUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod cannot make a directory unreadable")
	}

	libRoot := t.TempDir()
	showDir := filepath.Join(libRoot, "My Show")
	if err := os.Mkdir(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(showDir, "My Show S01E01.mkv")

	fs := &fakeStore{
		items: map[uint]*store.MediaItem{
			1: {ID: 1, LibraryID: 1, Source: "disk", Status: "available", MediaType: "series"},
		},
		files: []store.MediaFile{
			{ID: 1, MediaItemID: 1, Path: filePath, FileName: "My Show S01E01.mkv"},
		},
	}
	svc := NewService(fs)
	lib := &store.Library{ID: 1, Path: libRoot, MediaType: "series"}

	// Make the show folder unreadable so scanMediaFolder returns an error.
	if err := os.Chmod(showDir, 0o000); err != nil {
		t.Fatal(err)
	}
	// Restore perms before t.TempDir cleanup removes the tree (LIFO: this runs first).
	t.Cleanup(func() { _ = os.Chmod(showDir, 0o755) })

	_, removed, err := svc.SyncLibrary(lib)
	if err != nil {
		t.Fatalf("SyncLibrary returned error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("no files should be removed when the folder could not be scanned, got %d", removed)
	}
	if len(fs.deletedFilePaths) != 0 {
		t.Fatalf("no media files should be deleted on scan error, deleted=%v", fs.deletedFilePaths)
	}
	if len(fs.deletedItems) != 0 {
		t.Fatalf("media item must not be deleted when its folder could not be scanned, deleted=%v", fs.deletedItems)
	}
	if len(fs.deletedMeta) != 0 || len(fs.deletedEpisodes) != 0 {
		t.Fatalf("metadata/episodes must not be deleted on scan error")
	}
}

// TestSyncLibraryKeepsRequestItemWhenFilesGone verifies that when a request item's
// files are genuinely gone from disk, the stale file records are pruned but the
// request item itself (plus metadata/episodes) survives.
func TestSyncLibraryKeepsRequestItemWhenFilesGone(t *testing.T) {
	libRoot := t.TempDir()
	// The request item's folder exists but is genuinely empty on disk; a stale
	// MediaFile record still points inside it.
	showDir := filepath.Join(libRoot, "Requested Show")
	if err := os.Mkdir(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(showDir, "Requested Show S01E01.mkv") // not present on disk

	fs := &fakeStore{
		items: map[uint]*store.MediaItem{
			1: {ID: 1, LibraryID: 1, Source: "request", Status: "requested", MediaType: "series"},
		},
		files: []store.MediaFile{
			{ID: 1, MediaItemID: 1, Path: stalePath, FileName: "Requested Show S01E01.mkv"},
		},
	}
	svc := NewService(fs)
	lib := &store.Library{ID: 1, Path: libRoot, MediaType: "series"}

	_, removed, err := svc.SyncLibrary(lib)
	if err != nil {
		t.Fatalf("SyncLibrary returned error: %v", err)
	}
	// The stale file record IS removed (the folder was scanned and is genuinely empty)...
	if removed != 1 {
		t.Fatalf("expected the stale file record to be removed, got %d", removed)
	}
	// ...but the request item and its metadata/episodes must survive.
	if len(fs.deletedItems) != 0 {
		t.Fatalf("request item must not be deleted, deleted=%v", fs.deletedItems)
	}
	if len(fs.deletedMeta) != 0 || len(fs.deletedEpisodes) != 0 {
		t.Fatalf("request item metadata/episodes must not be deleted")
	}
}

package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

type librarySyncBoundaryStore struct {
	store.Store
	failAppend error
}

func (s *librarySyncBoundaryStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&librarySyncBoundaryStore{Store: tx, failAppend: s.failAppend})
	})
}

func (s *librarySyncBoundaryStore) AppendMediaActivity(activity *store.MediaActivity) error {
	if s.failAppend != nil && activity.Action == store.MediaActivityActionResyncCompleted {
		return s.failAppend
	}
	return s.Store.AppendMediaActivity(activity)
}

func TestSyncLibraryRecordsOneSystemActivityPerAffectedItem(t *testing.T) {
	st, library := newLibrarySyncActivityStore(t)
	firstFolder := filepath.Join(library.Path, "First")
	secondFolder := filepath.Join(library.Path, "Second")
	quietFolder := filepath.Join(library.Path, "Quiet")
	for _, folder := range []string{firstFolder, secondFolder, quietFolder} {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	first := createLibrarySyncItem(t, st, library.ID, "First")
	firstStalePath := filepath.Join(firstFolder, "First.Gone.720p.mkv")
	firstAddedPath := filepath.Join(firstFolder, "First.New.1080p.mkv")
	writeLibrarySyncFile(t, firstAddedPath, "first-added")
	createLibrarySyncFileRecord(t, st, first.ID, firstStalePath, 1, "720p")
	requester := &store.User{Email: "requester@example.com", PasswordHash: "hash"}
	if err := st.CreateUser(requester); err != nil {
		t.Fatal(err)
	}
	requesterID := requester.ID
	if err := st.CreateMediaRequest(&store.MediaRequest{
		MediaItemID: first.ID, UserID: &requesterID, Scope: store.MediaRequestScopeMedia,
	}); err != nil {
		t.Fatal(err)
	}

	second := createLibrarySyncItem(t, st, library.ID, "Second")
	secondExistingPath := filepath.Join(secondFolder, "Second.720p.mkv")
	secondAddedPath := filepath.Join(secondFolder, "Second.Extra.1080p.mkv")
	writeLibrarySyncFile(t, secondExistingPath, "second-existing")
	writeLibrarySyncFile(t, secondAddedPath, "second-added")
	createScannedLibrarySyncFileRecord(t, st, second.ID, secondExistingPath)

	quiet := createLibrarySyncItem(t, st, library.ID, "Quiet")
	quietPath := filepath.Join(quietFolder, "Quiet.720p.mkv")
	writeLibrarySyncFile(t, quietPath, "quiet")
	createScannedLibrarySyncFileRecord(t, st, quiet.ID, quietPath)

	bus := eventbus.New(8)
	var activityEvents atomic.Int32
	bus.Subscribe(eventbus.MediaActivityAdded, func(event eventbus.Event) {
		payload, ok := event.Payload.(eventbus.MediaActivityPayload)
		if !ok || payload.MediaItemID != first.ID && payload.MediaItemID != second.ID {
			t.Errorf("activity payload = %#v", event.Payload)
		}
		activityEvents.Add(1)
	})
	bus.Start()
	svc := NewService(st)
	svc.SetBus(bus)
	added, removed, err := svc.SyncLibrary(library)
	if err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	bus.Stop()
	if added != 2 || removed != 1 {
		t.Fatalf("counts = added %d removed %d, want 2/1", added, removed)
	}
	if activityEvents.Load() != 2 {
		t.Fatalf("activity events = %d, want 2", activityEvents.Load())
	}

	assertLibrarySyncActivity(t, st, first, 1, 1, []string{firstStalePath, firstAddedPath})
	assertLibrarySyncActivity(t, st, second, 1, 0, []string{secondAddedPath})
	if rows := librarySyncActivityRows(t, st, quiet.ID); len(rows) != 0 {
		t.Fatalf("no-op item activity = %+v", rows)
	}
}

func TestSyncLibraryActivityAppendFailureRollsBackItemFiles(t *testing.T) {
	st, library := newLibrarySyncActivityStore(t)
	folder := filepath.Join(library.Path, "Rollback")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	item := createLibrarySyncItem(t, st, library.ID, "Rollback")
	stalePath := filepath.Join(folder, "Rollback.Gone.720p.mkv")
	addedPath := filepath.Join(folder, "Rollback.New.1080p.mkv")
	createLibrarySyncFileRecord(t, st, item.ID, stalePath, 1, "720p")
	writeLibrarySyncFile(t, addedPath, "added")

	appendErr := errors.New("activity unavailable")
	wrapped := &librarySyncBoundaryStore{Store: st, failAppend: appendErr}
	bus := eventbus.New(4)
	var activityEvents atomic.Int32
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityEvents.Add(1) })
	bus.Start()
	svc := NewService(wrapped)
	svc.SetBus(bus)
	added, removed, err := svc.SyncLibrary(library)
	bus.Stop()
	if !errors.Is(err, appendErr) {
		t.Fatalf("error = %v, want append failure", err)
	}
	if added != 0 || removed != 0 {
		t.Fatalf("reported rolled-back counts = %d/%d", added, removed)
	}
	files, err := st.ListMediaFilesByMediaItem(item.ID)
	if err != nil || len(files) != 1 || files[0].Path != stalePath {
		t.Fatalf("files after rollback = %+v, err = %v", files, err)
	}
	if rows := librarySyncActivityRows(t, st, item.ID); len(rows) != 0 {
		t.Fatalf("activity after rollback = %+v", rows)
	}
	if activityEvents.Load() != 0 {
		t.Fatalf("activity events after rollback = %d", activityEvents.Load())
	}
}

func TestSyncLibraryExistingFileMetadataIsNotAnUpdateEffect(t *testing.T) {
	st, library := newLibrarySyncActivityStore(t)
	folder := filepath.Join(library.Path, "Unchanged")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	item := createLibrarySyncItem(t, st, library.ID, "Unchanged")
	path := filepath.Join(folder, "Unchanged.1080p.mkv")
	writeLibrarySyncFile(t, path, "larger-than-record")
	createLibrarySyncFileRecord(t, st, item.ID, path, 1, "720p")

	added, removed, err := NewService(st).SyncLibrary(library)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || removed != 0 {
		t.Fatalf("counts = %d/%d, want no file-record effects", added, removed)
	}
	files, err := st.ListMediaFilesByMediaItem(item.ID)
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %+v, err = %v", files, err)
	}
	if files[0].Size != 1 || files[0].Resolution != "720p" {
		t.Fatalf("SyncLibrary unexpectedly updated existing metadata: %+v", files[0])
	}
	if rows := librarySyncActivityRows(t, st, item.ID); len(rows) != 0 {
		t.Fatalf("metadata-only scan activity = %+v", rows)
	}
}

func newLibrarySyncActivityStore(t *testing.T) (*sqlite.SQLiteStore, *store.Library) {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	library := &store.Library{Name: "Movies", Path: filepath.Join(t.TempDir(), "library"), MediaType: "movie"}
	if err := os.MkdirAll(library.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateLibrary(library); err != nil {
		t.Fatal(err)
	}
	return st, library
}

func createLibrarySyncItem(t *testing.T, st store.Store, libraryID uint, title string) *store.MediaItem {
	t.Helper()
	item := &store.MediaItem{LibraryID: libraryID, Title: title, MediaType: "movie", Status: "new", Source: "disk"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	return item
}

func writeLibrarySyncFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func createScannedLibrarySyncFileRecord(t *testing.T, st store.Store, itemID uint, path string) {
	t.Helper()
	scanned := buildScannedFile(path, filepath.Base(path), nil)
	file := &store.MediaFile{
		MediaItemID: itemID, Path: path, FileName: scanned.fileName, Size: scanned.size,
		Resolution: scanned.resolution, SourceType: scanned.sourceType,
		SeasonNumber: scanned.seasonNumber, EpisodeNumber: scanned.episodeNumber,
	}
	if err := st.CreateMediaFile(file); err != nil {
		t.Fatal(err)
	}
}

func createLibrarySyncFileRecord(t *testing.T, st store.Store, itemID uint, path string, size int64, resolution string) {
	t.Helper()
	file := &store.MediaFile{MediaItemID: itemID, Path: path, FileName: filepath.Base(path), Size: size, Resolution: resolution}
	if err := st.CreateMediaFile(file); err != nil {
		t.Fatal(err)
	}
}

func assertLibrarySyncActivity(t *testing.T, st store.Store, item *store.MediaItem, added, removed int, paths []string) {
	t.Helper()
	rows := librarySyncActivityRows(t, st, item.ID)
	if len(rows) != 1 {
		t.Fatalf("activity for item %d = %+v", item.ID, rows)
	}
	row := rows[0]
	if row.Action != store.MediaActivityActionResyncCompleted || row.ActorKind != store.MediaActivityActorSystem ||
		row.ActorComponent != "sync" || row.ActorUserID != nil || row.Visibility != store.MediaActivityVisibilityShared {
		t.Fatalf("activity attribution for item %d = %+v", item.ID, row)
	}
	details := resyncActivityDetails(t, row)
	if details.Reason != "library_sync" || details.Added != added || details.Updated != 0 || details.Removed != removed {
		t.Fatalf("activity details for item %d = %+v", item.ID, details)
	}
	for _, path := range paths {
		if strings.Contains(row.Details, path) {
			t.Fatalf("activity leaked path %q", path)
		}
	}
}

func librarySyncActivityRows(t *testing.T, st store.Store, itemID uint) []store.MediaActivityAttribution {
	t.Helper()
	rows, _, err := st.ListMediaActivityPage(itemID, 0, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

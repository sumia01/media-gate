package sync

import (
	"encoding/json"
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

type resyncBoundaryStore struct {
	store.Store
	txCount    *int
	beforeTx   func(int)
	failAction string
	failErr    error
}

func (s *resyncBoundaryStore) WithTx(fn func(store.Store) error) error {
	(*s.txCount)++
	if s.beforeTx != nil {
		s.beforeTx(*s.txCount)
	}
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&resyncBoundaryStore{
			Store: tx, txCount: s.txCount, beforeTx: s.beforeTx,
			failAction: s.failAction, failErr: s.failErr,
		})
	})
}

func (s *resyncBoundaryStore) AppendMediaActivity(entry *store.MediaActivity) error {
	if entry.Action == s.failAction {
		return s.failErr
	}
	return s.Store.AppendMediaActivity(entry)
}

func TestExplicitResyncCorrelatesRequestAndCommittedResult(t *testing.T) {
	st, item, user, existingPath, stalePath, addedPath := newResyncActivityFixture(t)
	bus := eventbus.New(8)
	var activityEvents, completedEvents atomic.Int32
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityEvents.Add(1) })
	bus.Subscribe(eventbus.ResyncCompleted, func(event eventbus.Event) {
		payload, ok := event.Payload.(eventbus.ResyncPayload)
		if !ok || payload.MediaItemID != item.ID || payload.Updated != 1 || payload.Added != 1 || payload.Removed != 1 {
			t.Errorf("resync payload = %#v", event.Payload)
		}
		completedEvents.Add(1)
	})
	bus.Start()
	svc := NewService(st)
	svc.SetBus(bus)

	updated, added, removed, err := svc.ResyncMediaItemForUser(user.ID, item.ID)
	if err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	bus.Stop()
	if updated != 1 || added != 1 || removed != 1 {
		t.Fatalf("counts = updated %d added %d removed %d", updated, added, removed)
	}
	if activityEvents.Load() != 2 || completedEvents.Load() != 1 {
		t.Fatalf("events = activity %d completed %d", activityEvents.Load(), completedEvents.Load())
	}

	files, err := st.ListMediaFilesByMediaItem(item.ID)
	if err != nil || len(files) != 2 {
		t.Fatalf("files = %+v, err = %v", files, err)
	}
	paths := map[string]store.MediaFile{}
	for _, file := range files {
		paths[file.Path] = file
	}
	if paths[existingPath].Resolution != "1080p" || paths[existingPath].Size != int64(len("existing")) {
		t.Fatalf("existing file not refreshed: %+v", paths[existingPath])
	}
	if _, ok := paths[addedPath]; !ok {
		t.Fatalf("added path missing: %+v", paths)
	}
	if _, ok := paths[stalePath]; ok {
		t.Fatalf("stale path retained: %+v", paths[stalePath])
	}

	rows := resyncActivityRows(t, st, item.ID, user.ID)
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionResyncCompleted || rows[1].Action != store.MediaActivityActionResyncRequested {
		t.Fatalf("activity = %+v", rows)
	}
	if rows[0].OperationID == "" || rows[0].OperationID != rows[1].OperationID {
		t.Fatalf("operation IDs = %q, %q", rows[0].OperationID, rows[1].OperationID)
	}
	if rows[0].ActorKind != store.MediaActivityActorSystem || rows[0].ActorComponent != "sync" || rows[1].ActorUserID == nil || *rows[1].ActorUserID != user.ID {
		t.Fatalf("actors = %+v / %+v", rows[0], rows[1])
	}
	details := resyncActivityDetails(t, rows[0])
	if details.Updated != 1 || details.Added != 1 || details.Removed != 1 {
		t.Fatalf("result details = %+v", details)
	}
	for _, path := range []string{existingPath, stalePath, addedPath} {
		if strings.Contains(rows[0].Details, path) || strings.Contains(rows[1].Details, path) {
			t.Fatalf("activity leaked path %q", path)
		}
	}
}

func TestExplicitResyncRecordsPartialScanResult(t *testing.T) {
	st, item, user, existingPath, stalePath, addedPath := newResyncActivityFixture(t)
	libraryRoot := filepath.Dir(filepath.Dir(existingPath))
	failedFolder := filepath.Join(libraryRoot, "Unavailable")
	if err := os.Symlink(filepath.Base(failedFolder), failedFolder); err != nil {
		t.Fatal(err)
	}
	failedPath := filepath.Join(failedFolder, "Unavailable.720p.mkv")
	if err := st.CreateMediaFile(&store.MediaFile{
		MediaItemID: item.ID, Path: failedPath, FileName: filepath.Base(failedPath), Size: 1, Resolution: "720p",
	}); err != nil {
		t.Fatal(err)
	}
	deepFolder := filepath.Join(filepath.Dir(existingPath), "Release", "Deep")
	if err := os.MkdirAll(deepFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	failedFileParent := filepath.Join(deepFolder, "Loop")
	if err := os.Symlink(filepath.Base(failedFileParent), failedFileParent); err != nil {
		t.Fatal(err)
	}
	failedFilePath := filepath.Join(failedFileParent, "Hidden.720p.mkv")
	if err := st.CreateMediaFile(&store.MediaFile{
		MediaItemID: item.ID, Path: failedFilePath, FileName: filepath.Base(failedFilePath), Size: 1, Resolution: "720p",
	}); err != nil {
		t.Fatal(err)
	}

	updated, added, removed, err := NewService(st).ResyncMediaItemForUser(user.ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 || added != 1 || removed != 1 {
		t.Fatalf("partial resync counts = %d, %d, %d", updated, added, removed)
	}
	files, err := st.ListMediaFilesByMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]struct{}, len(files))
	for _, file := range files {
		paths[file.Path] = struct{}{}
	}
	if _, ok := paths[failedPath]; !ok {
		t.Fatal("partial resync removed a file from the failed scan window")
	}
	if _, ok := paths[failedFilePath]; !ok {
		t.Fatal("partial resync removed a file after its stat failed")
	}
	if _, ok := paths[stalePath]; ok {
		t.Fatal("partial resync retained a missing file from a successful scan window")
	}
	if _, ok := paths[addedPath]; !ok {
		t.Fatal("partial resync missed a file from a successful scan window")
	}

	rows := resyncActivityRows(t, st, item.ID, user.ID)
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionResyncCompleted {
		t.Fatalf("partial activity = %+v", rows)
	}
	details := resyncActivityDetails(t, rows[0])
	if !details.Partial || details.Reason != "partial_scan" || details.CleanupOutcome != "partial_scan" || details.Total != 2 {
		t.Fatalf("partial details = %+v", details)
	}
	if details.Updated != 1 || details.Added != 1 || details.Removed != 1 {
		t.Fatalf("partial committed counts = %+v", details)
	}
	for _, path := range []string{existingPath, stalePath, addedPath, failedFolder, failedPath, failedFileParent, failedFilePath} {
		if strings.Contains(rows[0].Details, path) || strings.Contains(rows[1].Details, path) {
			t.Fatalf("partial activity leaked path %q", path)
		}
	}
}

func TestExplicitResyncRecordsCompleteZeroChangeResult(t *testing.T) {
	st, item, user, _, stalePath, addedPath := newResyncActivityFixture(t)
	if err := os.Remove(addedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := st.ListMediaFilesByMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range files {
		info, err := os.Stat(files[i].Path)
		if err != nil {
			t.Fatal(err)
		}
		parsed := buildScannedFile(files[i].Path, files[i].FileName, nil)
		files[i].Size = info.Size()
		files[i].Resolution = parsed.resolution
		files[i].SourceType = parsed.sourceType
		files[i].SeasonNumber = parsed.seasonNumber
		files[i].EpisodeNumber = parsed.episodeNumber
		if err := st.UpdateMediaFile(&files[i]); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewService(st)
	updated, added, removed, err := svc.ResyncMediaItemForUser(user.ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 0 || added != 0 || removed != 0 {
		t.Fatalf("zero resync counts = %d, %d, %d", updated, added, removed)
	}
	rows := resyncActivityRows(t, st, item.ID, user.ID)
	if len(rows) != 2 || rows[0].OperationID != rows[1].OperationID {
		t.Fatalf("zero-count activity = %+v", rows)
	}
	details := resyncActivityDetails(t, rows[0])
	if details.Added != 0 || details.Updated != 0 || details.Removed != 0 || details.Partial || details.Total != 0 || details.Reason != "" || details.CleanupOutcome != "" {
		t.Fatalf("zero-count result = %+v", details)
	}
}

func TestResyncRejectsStaleCandidateAndRollsBackResult(t *testing.T) {
	t.Run("stale preimage", func(t *testing.T) {
		st, item, user, existingPath, stalePath, addedPath := newResyncActivityFixture(t)
		txCount := 0
		wrapped := &resyncBoundaryStore{Store: st, txCount: &txCount}
		wrapped.beforeTx = func(number int) {
			if number != 2 {
				return
			}
			files, err := st.ListMediaFilesByMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			for i := range files {
				if files[i].Path == existingPath {
					files[i].Resolution = "concurrent"
					if err := st.UpdateMediaFile(&files[i]); err != nil {
						t.Fatal(err)
					}
				}
			}
		}

		_, _, _, err := NewService(wrapped).ResyncMediaItemForUser(user.ID, item.ID)
		if !errors.Is(err, ErrResyncStale) {
			t.Fatalf("error = %v, want ErrResyncStale", err)
		}
		files, err := st.ListMediaFilesByMediaItem(item.ID)
		if err != nil || len(files) != 2 {
			t.Fatalf("stale apply files = %+v, err = %v", files, err)
		}
		byPath := map[string]store.MediaFile{}
		for _, file := range files {
			byPath[file.Path] = file
		}
		if byPath[existingPath].Resolution != "concurrent" {
			t.Fatalf("concurrent update overwritten: %+v", byPath[existingPath])
		}
		if _, ok := byPath[stalePath]; !ok {
			t.Fatal("stale candidate removed a current file")
		}
		if _, ok := byPath[addedPath]; ok {
			t.Fatal("stale candidate added a file")
		}
		rows := resyncActivityRows(t, st, item.ID, user.ID)
		if len(rows) != 1 || rows[0].Action != store.MediaActivityActionResyncRequested {
			t.Fatalf("stale activity = %+v", rows)
		}
	})

	t.Run("result append", func(t *testing.T) {
		st, item, user, existingPath, stalePath, addedPath := newResyncActivityFixture(t)
		appendErr := errors.New("activity unavailable")
		txCount := 0
		wrapped := &resyncBoundaryStore{
			Store: st, txCount: &txCount,
			failAction: store.MediaActivityActionResyncCompleted, failErr: appendErr,
		}
		_, _, _, err := NewService(wrapped).ResyncMediaItemForUser(user.ID, item.ID)
		if !errors.Is(err, appendErr) {
			t.Fatalf("error = %v, want append failure", err)
		}
		files, err := st.ListMediaFilesByMediaItem(item.ID)
		if err != nil || len(files) != 2 {
			t.Fatalf("rolled-back files = %+v, err = %v", files, err)
		}
		byPath := map[string]store.MediaFile{}
		for _, file := range files {
			byPath[file.Path] = file
		}
		if byPath[existingPath].Resolution != "720p" {
			t.Fatalf("update committed before append failure: %+v", byPath[existingPath])
		}
		if _, ok := byPath[stalePath]; !ok {
			t.Fatal("removal committed before append failure")
		}
		if _, ok := byPath[addedPath]; ok {
			t.Fatal("addition committed before append failure")
		}
		rows := resyncActivityRows(t, st, item.ID, user.ID)
		if len(rows) != 1 || rows[0].Action != store.MediaActivityActionResyncRequested {
			t.Fatalf("append failure activity = %+v", rows)
		}
	})
}

func TestResyncFinalApplyRejectsDeletionPending(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "automatic"
		if explicit {
			name = "explicit"
		}
		t.Run(name, func(t *testing.T) {
			st, item, user, _, _, addedPath := newResyncActivityFixture(t)
			txCount := 0
			claimAt := 1
			if explicit {
				claimAt = 2
			}
			wrapped := &resyncBoundaryStore{Store: st, txCount: &txCount}
			wrapped.beforeTx = func(number int) {
				if number != claimAt {
					return
				}
				current, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				current.DeletionPending = true
				if err := st.UpdateMediaItem(current); err != nil {
					t.Fatal(err)
				}
			}
			svc := NewService(wrapped)
			var err error
			if explicit {
				_, _, _, err = svc.ResyncMediaItemForUser(user.ID, item.ID)
			} else {
				_, _, _, err = svc.ResyncMediaItem(item.ID)
			}
			if !errors.Is(err, store.ErrMediaDeletionPending) {
				t.Fatalf("resync error = %v, want ErrMediaDeletionPending", err)
			}
			files, listErr := st.ListMediaFilesByMediaItem(item.ID)
			if listErr != nil || len(files) != 2 {
				t.Fatalf("claimed resync files = %+v, %v", files, listErr)
			}
			for _, file := range files {
				if file.Path == addedPath {
					t.Fatal("claimed resync added a file record")
				}
			}
			rows := resyncActivityRows(t, st, item.ID, user.ID)
			wantRows := 0
			if explicit {
				wantRows = 1
			}
			if len(rows) != wantRows || wantRows == 1 && rows[0].Action != store.MediaActivityActionResyncRequested {
				t.Fatalf("claimed resync activity = %+v", rows)
			}
		})
	}
}

func TestAutomaticResyncDoesNotAppendOrInvalidateActivity(t *testing.T) {
	st, item, user, _, _, _ := newResyncActivityFixture(t)
	bus := eventbus.New(4)
	var activityEvents atomic.Int32
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { activityEvents.Add(1) })
	bus.Start()
	svc := NewService(st)
	svc.SetBus(bus)
	if _, _, _, err := svc.ResyncMediaItem(item.ID); err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	bus.Stop()
	if activityEvents.Load() != 0 {
		t.Fatalf("automatic resync invalidations = %d", activityEvents.Load())
	}
	if rows := resyncActivityRows(t, st, item.ID, user.ID); len(rows) != 0 {
		t.Fatalf("automatic resync activity = %+v", rows)
	}
}

func newResyncActivityFixture(t *testing.T) (*sqlite.SQLiteStore, *store.MediaItem, *store.User, string, string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "library")
	folder := filepath.Join(root, "Movie")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.New(filepath.Join(t.TempDir(), "resync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	library := &store.Library{Name: "Movies", Path: root, MediaType: "movie"}
	if err := st.CreateLibrary(library); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: library.ID, Title: "Movie", MediaType: "movie", Status: "new", Source: "disk"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	user := &store.User{Email: "resync@example.com", PasswordHash: "hash"}
	if err := st.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	existingPath := filepath.Join(folder, "Movie.1080p.mkv")
	stalePath := filepath.Join(folder, "Movie.Gone.720p.mkv")
	addedPath := filepath.Join(folder, "Movie.Extra.720p.mkv")
	if err := os.WriteFile(existingPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addedPath, []byte("added"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []*store.MediaFile{
		{MediaItemID: item.ID, Path: existingPath, FileName: filepath.Base(existingPath), Size: 1, Resolution: "720p"},
		{MediaItemID: item.ID, Path: stalePath, FileName: filepath.Base(stalePath), Size: 1, Resolution: "720p"},
	} {
		if err := st.CreateMediaFile(file); err != nil {
			t.Fatal(err)
		}
	}
	return st, item, user, existingPath, stalePath, addedPath
}

func resyncActivityRows(t *testing.T, st store.Store, itemID, userID uint) []store.MediaActivityAttribution {
	t.Helper()
	rows, _, err := st.ListMediaActivityPage(itemID, userID, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func resyncActivityDetails(t *testing.T, row store.MediaActivityAttribution) store.MediaActivityDetails {
	t.Helper()
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
		t.Fatal(err)
	}
	return details
}

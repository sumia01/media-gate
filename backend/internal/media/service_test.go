package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

type deletionStore struct {
	store.Store
	updateErr      error
	deleteErr      error
	deleteMediaErr error
}

type activityFailingDeletionStore struct {
	store.Store
	appendCount *int
	failAt      int
	appendErr   error
}

func (s *activityFailingDeletionStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&activityFailingDeletionStore{
			Store: tx, appendCount: s.appendCount, failAt: s.failAt, appendErr: s.appendErr,
		})
	})
}

func (s *activityFailingDeletionStore) AppendMediaActivity(entry *store.MediaActivity) error {
	(*s.appendCount)++
	if *s.appendCount == s.failAt {
		return s.appendErr
	}
	return s.Store.AppendMediaActivity(entry)
}

func (s *deletionStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&deletionStore{Store: tx, updateErr: s.updateErr, deleteErr: s.deleteErr, deleteMediaErr: s.deleteMediaErr})
	})
}

func (s *deletionStore) UpdateDownload(dl *store.Download) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.Store.UpdateDownload(dl)
}

func (s *deletionStore) DeleteDownload(id uint) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.Store.DeleteDownload(id)
}

func (s *deletionStore) DeleteMediaItem(id uint) error {
	if s.deleteMediaErr != nil {
		return s.deleteMediaErr
	}
	return s.Store.DeleteMediaItem(id)
}

func TestDeleteDownloadCancellationAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deleteFiles  bool
		cancelFails  bool
		deleteFails  bool
		qbitFails    bool
		unconfigured bool
	}{
		{name: "keep imported files"},
		{name: "remove imported files", deleteFiles: true},
		{name: "cancellation save fails", deleteFiles: true, cancelFails: true},
		{name: "database delete fails", deleteFails: true},
		{name: "qbit failure remains best effort", deleteFiles: true, qbitFails: true},
		{name: "qbit unconfigured remains best effort", deleteFiles: true, unconfigured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := sqlite.New(filepath.Join(t.TempDir(), "media.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			lib := &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
			if err := s.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Movie", MediaType: "movie", Status: "ready", Source: "disk"}
			if err := s.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			user := &store.User{Email: "actor@example.com", PasswordHash: "hash"}
			if err := s.CreateUser(user); err != nil {
				t.Fatal(err)
			}
			downloadedAt := time.Now().Add(-time.Hour)
			dl := &store.Download{
				MediaItemID: item.ID, Title: "Movie.1080p", Status: "seeding", ClientTorrentHash: "abc",
				LinkedToLibrary: true, LastError: "previous failure", DownloadedAt: &downloadedAt,
			}
			if err := s.CreateDownload(dl); err != nil {
				t.Fatal(err)
			}
			releaseDir := filepath.Join(importer.BuildTargetDir(lib, item, nil, nil), importer.BuildReleaseFolderName(dl.Title))
			if err := os.MkdirAll(releaseDir, 0755); err != nil {
				t.Fatal(err)
			}
			video := filepath.Join(releaseDir, "Movie.mkv")
			if err := os.WriteFile(video, []byte("video"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := s.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: video, FileName: "Movie.mkv", Size: 5}); err != nil {
				t.Fatal(err)
			}
			var deleteCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/auth/login":
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
					_, _ = w.Write([]byte("Ok."))
				case "/api/v2/torrents/delete":
					deleteCalls.Add(1)
					got, err := s.GetDownload(dl.ID)
					if err != nil || got.Status != "cancelled" || got.LastError != dl.LastError || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) {
						t.Errorf("cleanup started without preserved cancellation: %+v, %v", got, err)
					}
					wantDeleteFiles := "false"
					if tc.deleteFiles {
						wantDeleteFiles = "true"
					}
					if r.FormValue("hashes") != "abc" || r.FormValue("deleteFiles") != wantDeleteFiles {
						t.Errorf("wrong torrent cleanup options: %v", r.Form)
					}
					if tc.qbitFails {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			config := map[string]string{settings.KeyQBitURL: srv.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p"}
			if tc.unconfigured {
				config = nil
			}
			settingsSvc := settings.NewService(s, "", config, "test-secret", srv.Client())
			provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
			bus := eventbus.New(16)
			var deletedEvents, failureEvents atomic.Int32
			bus.SubscribeAll(func(e eventbus.Event) {
				if e.Type == eventbus.DownloadDeleted {
					deletedEvents.Add(1)
				}
				if e.Type == eventbus.DownloadFailed || e.Type == eventbus.ImportFailed {
					failureEvents.Add(1)
				}
			})
			bus.Start()
			t.Cleanup(bus.Stop)
			st := &deletionStore{Store: s}
			writeErr := errors.New("database unavailable")
			if tc.cancelFails {
				st.updateErr = writeErr
			}
			if tc.deleteFails {
				st.deleteErr = writeErr
			}
			svc := NewService(st, mediasync.NewService(s), bus, provider, "")
			err = svc.DeleteDownload(user.ID, dl.ID, tc.deleteFiles)
			if tc.cancelFails || tc.deleteFails {
				if !errors.Is(err, writeErr) {
					t.Fatalf("expected DB error, got %v", err)
				}
				got, err := s.GetDownload(dl.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := "cancelled"
				if tc.cancelFails {
					wantStatus = "seeding"
				}
				if got.Status != wantStatus || got.LastError != dl.LastError || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) || got.ClientTorrentHash != "abc" || !got.LinkedToLibrary {
					t.Errorf("failed deletion lost recoverable cleanup state: %+v", got)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.GetDownload(dl.ID); !errors.Is(err, store.ErrNotFound) {
					t.Errorf("download not deleted: %v", err)
				}
			}
			wantCalls := int32(1)
			if tc.cancelFails || tc.unconfigured {
				wantCalls = 0
			}
			if deleteCalls.Load() != wantCalls {
				t.Errorf("qBit cleanup calls=%d, want %d", deleteCalls.Load(), wantCalls)
			}
			removed := tc.deleteFiles && !tc.cancelFails
			_, statErr := os.Stat(video)
			files, err := s.ListMediaFilesByMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if removed {
				if !os.IsNotExist(statErr) || len(files) != 0 {
					t.Errorf("imported file cleanup changed: stat=%v, rows=%d", statErr, len(files))
				}
			} else if statErr != nil || len(files) != 1 {
				t.Errorf("imported files unexpectedly removed: stat=%v, rows=%d", statErr, len(files))
			}
			if tc.deleteFails {
				// Retrying deletion of the retained cancelled row must still work.
				st.deleteErr = nil
				if err := svc.DeleteDownload(user.ID, dl.ID, false); err != nil {
					t.Fatalf("retry deletion: %v", err)
				}
			}
			bus.Stop()
			wantDeletedEvents := int32(0)
			if removed && !tc.deleteFails {
				wantDeletedEvents = 1
			}
			if deletedEvents.Load() != wantDeletedEvents || failureEvents.Load() != 0 {
				t.Errorf("unexpected events: deleted=%d failures=%d", deletedEvents.Load(), failureEvents.Load())
			}
		})
	}
}

func TestDeleteDownloadActivityCorrelatesRequestAndObservedResult(t *testing.T) {
	st, lib, item, user, dl := newRemovalFixture(t, true, "torrent-secret")
	releaseDir := filepath.Join(importer.BuildTargetDir(lib, item, nil, nil), importer.BuildReleaseFolderName(dl.Title))
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(releaseDir, "Movie.mkv")
	subtitlePath := filepath.Join(releaseDir, "Movie.srt")
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subtitlePath, []byte("subtitle"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: videoPath, FileName: "Movie.mkv"}); err != nil {
		t.Fatal(err)
	}
	subtitle := &store.Subtitle{MediaItemID: item.ID, FilePath: subtitlePath, FileName: "Movie.srt", Language: "en", Provider: "manual"}
	if err := st.CreateSubtitle(subtitle); err != nil {
		t.Fatal(err)
	}

	var torrentCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
		case "/api/v2/torrents/delete":
			torrentCalls.Add(1)
			if r.FormValue("hashes") != dl.ClientTorrentHash || r.FormValue("deleteFiles") != "true" {
				t.Errorf("unexpected torrent delete request: %v", r.Form)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	settingsSvc := settings.NewService(st, "", map[string]string{
		settings.KeyQBitURL: srv.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p",
	}, "test-secret", srv.Client())
	provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
	bus := eventbus.New(16)
	var invalidations atomic.Int32
	bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { invalidations.Add(1) })
	bus.Start()
	svc := NewService(st, mediasync.NewService(st), bus, provider, "")
	if err := svc.DeleteDownload(user.ID, dl.ID, true); err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	bus.Stop()

	if torrentCalls.Load() != 1 || invalidations.Load() != 2 {
		t.Fatalf("torrent calls=%d invalidations=%d", torrentCalls.Load(), invalidations.Load())
	}
	if _, err := st.GetDownload(dl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("download still present: %v", err)
	}
	if files, err := st.ListMediaFilesByMediaItem(item.ID); err != nil || len(files) != 0 {
		t.Fatalf("media files = %+v, err = %v", files, err)
	}
	if _, err := st.GetSubtitle(subtitle.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("subtitle still present: %v", err)
	}

	rows := removalActivityRows(t, st, item.ID, user.ID)
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionDownloadRemoved || rows[1].Action != store.MediaActivityActionDownloadRemovalRequest {
		t.Fatalf("removal activity = %+v", rows)
	}
	if rows[0].OperationID == "" || rows[0].OperationID != rows[1].OperationID {
		t.Fatalf("operation IDs = %q, %q", rows[0].OperationID, rows[1].OperationID)
	}
	if rows[0].ActorKind != store.MediaActivityActorSystem || rows[0].ActorComponent != "media" || rows[1].ActorUserID == nil || *rows[1].ActorUserID != user.ID {
		t.Fatalf("removal actors = %+v / %+v", rows[0], rows[1])
	}
	request := removalActivityDetails(t, rows[1])
	if request.RequestedDeleteFiles == nil || !*request.RequestedDeleteFiles || request.OldStatus != "seeding" || request.NewStatus != "cancelled" {
		t.Fatalf("request details = %+v", request)
	}
	result := removalActivityDetails(t, rows[0])
	if result.RecordRemoved == nil || !*result.RecordRemoved || result.TorrentCleanupOutcome != "requested" || result.FileCleanupOutcome != "completed" || result.PhysicalFilesRemoved != 2 || result.PhysicalFilesFailed != 0 || result.DatabaseRecordsRemoved != 2 {
		t.Fatalf("result details = %+v", result)
	}
	if result.Target == nil || result.Target.Scope != store.MediaActivityScopeMedia || result.DownloadID == nil || *result.DownloadID != dl.ID {
		t.Fatalf("result target = %+v", result)
	}
	for _, secret := range []string{dl.DownloadURL, dl.ClientTorrentHash, videoPath, dl.LastError} {
		if strings.Contains(rows[0].Details, secret) || strings.Contains(rows[1].Details, secret) {
			t.Fatalf("activity leaked secret %q", secret)
		}
	}
}

func TestDeleteDownloadFailedPhysicalCleanupRetainsChildSnapshot(t *testing.T) {
	st, lib, item, user, dl := newRemovalFixture(t, true, "")
	releaseDir := filepath.Join(importer.BuildTargetDir(lib, item, nil, nil), importer.BuildReleaseFolderName(dl.Title))
	blockedPath := filepath.Join(releaseDir, "Blocked.mkv")
	if err := os.MkdirAll(blockedPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blockedPath, "child"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	mediaFile := &store.MediaFile{MediaItemID: item.ID, Path: blockedPath, FileName: "Blocked.mkv"}
	if err := st.CreateMediaFile(mediaFile); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New(8)
	bus.Start()
	svc := NewService(st, mediasync.NewService(st), bus, nil, "")
	if err := svc.DeleteDownload(user.ID, dl.ID, true); err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	bus.Stop()

	if _, err := os.Stat(blockedPath); err != nil {
		t.Fatalf("failed path was removed: %v", err)
	}
	files, err := st.ListMediaFilesByMediaItem(item.ID)
	if err != nil || len(files) != 1 || files[0].ID != mediaFile.ID {
		t.Fatalf("failed child snapshot = %+v, err = %v", files, err)
	}
	if _, err := st.GetDownload(dl.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("download record not removed: %v", err)
	}
	rows := removalActivityRows(t, st, item.ID, user.ID)
	result := removalActivityDetails(t, rows[0])
	if result.TorrentCleanupOutcome != "not_applicable" || result.FileCleanupOutcome != "failed" || result.PhysicalFilesRemoved != 0 || result.PhysicalFilesFailed != 1 || result.DatabaseRecordsRemoved != 0 {
		t.Fatalf("failed cleanup was described incorrectly: %+v", result)
	}
}

func TestDeleteDownloadActivityFailuresPreserveAtomicFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{name: "request append", failAt: 1},
		{name: "result append", failAt: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, lib, item, user, dl := newRemovalFixture(t, true, "")
			releaseDir := filepath.Join(importer.BuildTargetDir(lib, item, nil, nil), importer.BuildReleaseFolderName(dl.Title))
			if err := os.MkdirAll(releaseDir, 0755); err != nil {
				t.Fatal(err)
			}
			videoPath := filepath.Join(releaseDir, "Movie.mkv")
			if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
				t.Fatal(err)
			}
			mediaFile := &store.MediaFile{MediaItemID: item.ID, Path: videoPath, FileName: "Movie.mkv"}
			if err := st.CreateMediaFile(mediaFile); err != nil {
				t.Fatal(err)
			}
			appendErr := errors.New("activity unavailable")
			appendCount := 0
			wrapped := &activityFailingDeletionStore{Store: st, appendCount: &appendCount, failAt: tc.failAt, appendErr: appendErr}
			bus := eventbus.New(8)
			var invalidations atomic.Int32
			bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { invalidations.Add(1) })
			bus.Start()
			svc := NewService(wrapped, mediasync.NewService(st), bus, nil, "")
			if err := svc.DeleteDownload(user.ID, dl.ID, true); !errors.Is(err, appendErr) {
				bus.Stop()
				t.Fatalf("error = %v, want append failure", err)
			}
			bus.Stop()

			current, err := st.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			rows := removalActivityRows(t, st, item.ID, user.ID)
			files, err := st.ListMediaFilesByMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.failAt == 1 {
				if current.Status != "seeding" || len(rows) != 0 || len(files) != 1 || invalidations.Load() != 0 {
					t.Fatalf("request append failure committed facts: status=%s rows=%d files=%d invalidations=%d", current.Status, len(rows), len(files), invalidations.Load())
				}
				if _, err := os.Stat(videoPath); err != nil {
					t.Fatalf("request append failure performed I/O: %v", err)
				}
				return
			}
			if current.Status != "cancelled" || len(rows) != 1 || rows[0].Action != store.MediaActivityActionDownloadRemovalRequest || len(files) != 1 || files[0].ID != mediaFile.ID || invalidations.Load() != 1 {
				t.Fatalf("result append failure lost request/snapshots: status=%s rows=%+v files=%+v invalidations=%d", current.Status, rows, files, invalidations.Load())
			}
			if _, err := os.Stat(videoPath); !os.IsNotExist(err) {
				t.Fatalf("expected observed physical cleanup before final rollback, stat=%v", err)
			}
		})
	}
}

func TestDeleteDownloadValidatesLiveActor(t *testing.T) {
	st, _, item, user, dl := newRemovalFixture(t, false, "")
	if err := st.DeleteUser(user.ID); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New(2)
	svc := NewService(st, nil, bus, nil, "")
	if err := svc.DeleteDownload(user.ID, dl.ID, false); !errors.Is(err, store.ErrActivityActorNotFound) {
		t.Fatalf("error = %v, want ErrActivityActorNotFound", err)
	}
	current, err := st.GetDownload(dl.ID)
	if err != nil || current.Status != "seeding" {
		t.Fatalf("invalid actor changed download: %+v, %v", current, err)
	}
	if rows := removalActivityRows(t, st, item.ID, user.ID); len(rows) != 0 {
		t.Fatalf("invalid actor activity = %+v", rows)
	}
}

func TestDeleteDownloadRejectsConcurrentRetryBeforeFinalDelete(t *testing.T) {
	st, _, item, user, dl := newRemovalFixture(t, false, "torrent-secret")
	var torrentCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
		case "/api/v2/torrents/delete":
			torrentCalls.Add(1)
			current, err := st.GetDownload(dl.ID)
			if err != nil {
				t.Errorf("load cancellation during retry: %v", err)
				return
			}
			if current.Status != "cancelled" {
				t.Errorf("retry raced before cancellation committed: %s", current.Status)
			}
			current.Status = "pending"
			current.RetryCount++
			if err := st.UpdateDownload(current); err != nil {
				t.Errorf("persist concurrent retry: %v", err)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	settingsSvc := settings.NewService(st, "", map[string]string{
		settings.KeyQBitURL: server.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p",
	}, "test-secret", server.Client())
	provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, server.Client())
	b := eventbus.New(8)
	var invalidations, deletedEvents atomic.Int32
	b.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { invalidations.Add(1) })
	b.Subscribe(eventbus.DownloadDeleted, func(eventbus.Event) { deletedEvents.Add(1) })
	b.Start()
	err := NewService(st, nil, b, provider, "").DeleteDownload(user.ID, dl.ID, false)
	b.Stop()
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, want stale preimage rejection", err)
	}
	if torrentCalls.Load() != 1 {
		t.Fatalf("torrent cleanup calls = %d", torrentCalls.Load())
	}
	current, err := st.GetDownload(dl.ID)
	if err != nil || current.Status != "pending" || current.RetryCount != 1 {
		t.Fatalf("concurrent retry was deleted: %+v, %v", current, err)
	}
	rows := removalActivityRows(t, st, item.ID, user.ID)
	if len(rows) != 1 || rows[0].Action != store.MediaActivityActionDownloadRemovalRequest {
		t.Fatalf("stale final activity = %+v", rows)
	}
	if invalidations.Load() != 1 || deletedEvents.Load() != 0 {
		t.Fatalf("stale final events: invalidations=%d deleted=%d", invalidations.Load(), deletedEvents.Load())
	}
}

func TestCleanupImportedFilesUsesTrackedReleasePathsAfterRematch(t *testing.T) {
	t.Run("rematched item", func(t *testing.T) {
		st, lib, item, user, dl := newRemovalFixture(t, true, "")
		releaseDir := filepath.Join(importer.BuildTargetDir(lib, item, nil, nil), importer.BuildReleaseFolderName(dl.Title))
		if err := os.MkdirAll(releaseDir, 0o755); err != nil {
			t.Fatal(err)
		}
		videoPath := filepath.Join(releaseDir, "Movie.mkv")
		if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: videoPath, FileName: "Movie.mkv"}); err != nil {
			t.Fatal(err)
		}

		current, err := st.GetMediaItem(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		current.Title = "Rematched Movie"
		if err := st.UpdateMediaItem(current); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateMediaMetadata(&store.MediaMetadata{
			MediaItemID: item.ID, Source: "tmdb", ExternalID: 99, Title: "Provider Rematch",
		}); err != nil {
			t.Fatal(err)
		}

		b := eventbus.New(8)
		b.Start()
		if err := NewService(st, mediasync.NewService(st), b, nil, "").DeleteDownload(user.ID, dl.ID, true); err != nil {
			b.Stop()
			t.Fatal(err)
		}
		b.Stop()
		if _, err := os.Stat(videoPath); !os.IsNotExist(err) {
			t.Fatalf("old rematched file remains: %v", err)
		}
		if files, err := st.ListMediaFilesByMediaItem(item.ID); err != nil || len(files) != 0 {
			t.Fatalf("old tracked records = %+v, %v", files, err)
		}
		rows := removalActivityRows(t, st, item.ID, user.ID)
		result := removalActivityDetails(t, rows[0])
		if result.FileCleanupOutcome != "completed" || result.PhysicalFilesRemoved != 1 || result.DatabaseRecordsRemoved != 1 {
			t.Fatalf("rematch cleanup result = %+v", result)
		}
		if strings.Contains(rows[0].Details, videoPath) {
			t.Fatalf("cleanup activity leaked path: %s", rows[0].Details)
		}
	})

	t.Run("linked download without tracked candidates", func(t *testing.T) {
		st, _, item, user, dl := newRemovalFixture(t, true, "")
		b := eventbus.New(8)
		b.Start()
		if err := NewService(st, mediasync.NewService(st), b, nil, "").DeleteDownload(user.ID, dl.ID, true); err != nil {
			b.Stop()
			t.Fatal(err)
		}
		b.Stop()
		rows := removalActivityRows(t, st, item.ID, user.ID)
		result := removalActivityDetails(t, rows[0])
		if result.FileCleanupOutcome != "unavailable" || result.PhysicalFilesRemoved != 0 || result.DatabaseRecordsRemoved != 0 {
			t.Fatalf("candidate-free cleanup result = %+v", result)
		}
	})
}

type cancellationFailureStore struct {
	store.Store
	writes    *int
	parentErr bool
}

func (s *cancellationFailureStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&cancellationFailureStore{Store: tx, writes: s.writes, parentErr: s.parentErr})
	})
}

func (s *cancellationFailureStore) UpdateMediaItem(item *store.MediaItem) error {
	if s.parentErr {
		return errors.New("disable monitoring failed")
	}
	return s.Store.UpdateMediaItem(item)
}

func (s *cancellationFailureStore) UpdateDownload(dl *store.Download) error {
	(*s.writes)++
	if *s.writes == 2 {
		return errors.New("second cancellation failed")
	}
	return s.Store.UpdateDownload(dl)
}

func TestDeleteMediaItemCancellationRollbackHasNoSideEffects(t *testing.T) {
	for _, parentErr := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent_failure=%t", parentErr), func(t *testing.T) {
			s, err := sqlite.New(filepath.Join(t.TempDir(), "media.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			lib := &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
			if err := s.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			startedAt := time.Now().Add(-time.Hour)
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Movie", MediaType: "movie", Source: "disk", Monitored: true, MonitorSearchStartedAt: &startedAt}
			if err := s.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			user := &store.User{Email: fmt.Sprintf("actor-%t@example.com", parentErr), PasswordHash: "hash"}
			if err := s.CreateUser(user); err != nil {
				t.Fatal(err)
			}
			downloadedAt := time.Now().Add(-time.Hour)
			children := []store.Download{
				{MediaItemID: item.ID, Status: "downloading", ClientTorrentHash: "one", LastError: "one", SavePath: "/downloads/one", DownloadedAt: &downloadedAt},
				{MediaItemID: item.ID, Status: "importing", ClientTorrentHash: "two", LastError: "two", SavePath: "/downloads/two", DownloadedAt: &downloadedAt},
			}
			for i := range children {
				if err := s.CreateDownload(&children[i]); err != nil {
					t.Fatal(err)
				}
			}
			video := filepath.Join(lib.Path, "Movie.mkv")
			posterDir := t.TempDir()
			poster := filepath.Join(posterDir, fmt.Sprintf("%d.jpg", item.ID))
			for _, path := range []string{video, poster} {
				if err := os.WriteFile(path, []byte("preserve"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: video, FileName: "Movie.mkv"}); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("external request before all cancellations committed")
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer srv.Close()
			settingsSvc := settings.NewService(s, "", map[string]string{
				settings.KeyQBitURL: srv.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p",
			}, "test-secret", srv.Client())
			provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
			writes := 0
			st := &cancellationFailureStore{Store: s, writes: &writes, parentErr: parentErr}
			bus := eventbus.New(8)
			bus.SubscribeAll(func(e eventbus.Event) { t.Errorf("event on aborted deletion: %s", e.Type) })
			bus.Start()
			defer bus.Stop()
			if err := NewService(st, nil, bus, provider, posterDir).DeleteMediaItem(user.ID, item.ID); err == nil {
				t.Fatal("expected cancellation error")
			}
			wantWrites := 2
			if parentErr {
				wantWrites = 0
			}
			if writes != wantWrites {
				t.Fatalf("cancellation writes=%d, want %d", writes, wantWrites)
			}
			for _, child := range children {
				got, err := s.GetDownload(child.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != child.Status || !got.UpdatedAt.Equal(child.UpdatedAt) || got.ClientTorrentHash != child.ClientTorrentHash || got.SavePath != child.SavePath || got.LastError != child.LastError || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) {
					t.Errorf("child cancellation did not roll back: %+v", got)
				}
			}
			parent, err := s.GetMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !parent.Monitored || parent.DeletionPending || parent.MonitorSearchStartedAt == nil || !parent.MonitorSearchStartedAt.Equal(startedAt) || !parent.UpdatedAt.Equal(item.UpdatedAt) {
				t.Errorf("parent disable did not roll back: %+v", parent)
			}
			for _, path := range []string{video, poster} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("file removed on cancellation failure: %s: %v", path, err)
				}
			}
		})
	}
}

func TestDeleteMediaItemPreparationActivityAndFailureBoundaries(t *testing.T) {
	t.Run("actor and append failures roll back before I/O", func(t *testing.T) {
		for _, invalidActor := range []bool{true, false} {
			t.Run(fmt.Sprintf("invalid_actor=%t", invalidActor), func(t *testing.T) {
				st, lib, item, user, dl := newRemovalFixture(t, true, "torrent-secret")
				fresh, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh.Monitored = true
				if err := st.UpdateMediaItem(fresh); err != nil {
					t.Fatal(err)
				}
				if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{
					MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true,
				}); err != nil {
					t.Fatal(err)
				}
				video := filepath.Join(lib.Path, "Movie.mkv")
				if err := os.WriteFile(video, []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: video, FileName: "Movie.mkv"}); err != nil {
					t.Fatal(err)
				}
				var externalCalls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { externalCalls.Add(1) }))
				defer server.Close()
				settingsSvc := settings.NewService(st, "", map[string]string{
					settings.KeyQBitURL: server.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p",
				}, "test-secret", server.Client())
				provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, server.Client())

				serviceStore := store.Store(st)
				wantErr := store.ErrActivityActorNotFound
				actorID := user.ID + 1000
				if !invalidActor {
					appendErr := errors.New("activity unavailable")
					appendCount := 0
					serviceStore = &activityFailingDeletionStore{Store: st, appendCount: &appendCount, failAt: 1, appendErr: appendErr}
					wantErr = appendErr
					actorID = user.ID
				}
				bus := eventbus.New(4)
				var invalidations atomic.Int32
				bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { invalidations.Add(1) })
				bus.Start()
				err = NewService(serviceStore, nil, bus, provider, t.TempDir()).DeleteMediaItem(actorID, item.ID)
				bus.Stop()
				if !errors.Is(err, wantErr) {
					t.Fatalf("error = %v, want %v", err, wantErr)
				}
				parent, err := st.GetMediaItem(item.ID)
				if err != nil || !parent.Monitored || parent.DeletionPending {
					t.Fatalf("parent changed: %+v, %v", parent, err)
				}
				current, err := st.GetDownload(dl.ID)
				if err != nil || current.Status != "seeding" {
					t.Fatalf("download changed: %+v, %v", current, err)
				}
				if _, err := os.Stat(video); err != nil {
					t.Fatalf("file touched before prep commit: %v", err)
				}
				if externalCalls.Load() != 0 || invalidations.Load() != 0 {
					t.Fatalf("side effects = external %d invalidations %d", externalCalls.Load(), invalidations.Load())
				}
				if rows := removalActivityRows(t, st, item.ID, user.ID); len(rows) != 0 {
					t.Fatalf("failed prep activity = %+v", rows)
				}
				overrides, err := st.ListEpisodeMonitorsByMediaItem(item.ID)
				if err != nil || len(overrides) != 1 {
					t.Fatalf("failed prep did not roll back override removal: %+v, %v", overrides, err)
				}
			})
		}
	})

	t.Run("episode override removals are transactional activity", func(t *testing.T) {
		st, _, item, user, _ := newRemovalFixture(t, false, "")
		current, err := st.GetMediaItem(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		current.MediaType = "series"
		current.Monitored = true
		current.MonitorNewSeasons = false
		if err := st.UpdateMediaItem(current); err != nil {
			t.Fatal(err)
		}
		for _, override := range []store.EpisodeMonitor{
			{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true},
			{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 2, Monitored: false},
		} {
			if err := st.UpsertEpisodeMonitor(&override); err != nil {
				t.Fatal(err)
			}
		}
		deleteErr := errors.New("final delete unavailable")
		wrapped := &deletionStore{Store: st, deleteMediaErr: deleteErr}
		b := eventbus.New(4)
		b.Start()
		err = NewService(wrapped, nil, b, nil, t.TempDir()).DeleteMediaItem(user.ID, item.ID)
		b.Stop()
		if !errors.Is(err, deleteErr) {
			t.Fatalf("error = %v, want final delete failure", err)
		}
		overrides, err := st.ListEpisodeMonitorsByMediaItem(item.ID)
		if err != nil || len(overrides) != 0 {
			t.Fatalf("episode overrides remain after committed preparation: %+v, %v", overrides, err)
		}
		rows := removalActivityRows(t, st, item.ID, user.ID)
		if len(rows) != 1 || rows[0].Action != store.MediaActivityActionRemovalRequested {
			t.Fatalf("override removal activity = %+v", rows)
		}
		details := removalActivityDetails(t, rows[0])
		removedOverrides := 0
		for _, change := range details.MonitoringChanges {
			if change.Target.Scope != store.MediaActivityScopeEpisode {
				continue
			}
			removedOverrides++
			if change.Before == nil || change.After != nil {
				t.Fatalf("override removal change = %+v", change)
			}
		}
		if removedOverrides != 2 {
			t.Fatalf("recorded override removals = %d, details = %+v", removedOverrides, details)
		}
	})

	t.Run("final delete failure retains preparation", func(t *testing.T) {
		st, _, item, user, dl := newRemovalFixture(t, true, "torrent-secret")
		fresh, err := st.GetMediaItem(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		fresh.Monitored = true
		if err := st.UpdateMediaItem(fresh); err != nil {
			t.Fatal(err)
		}
		alreadyCancelled := &store.Download{
			MediaItemID: item.ID, Title: "Movie.Already.Cancelled", DownloadURL: "https://private.invalid/two",
			Status: "cancelled", ClientTorrentHash: "second-secret", LastError: "private-error",
		}
		if err := st.CreateDownload(alreadyCancelled); err != nil {
			t.Fatal(err)
		}
		mediaItemID := item.ID
		watched := &store.WatchedItem{
			UserID: user.ID, Source: "tmdb", ExternalID: 42, MediaType: "movie", Title: "Movie", MediaItemID: &mediaItemID,
		}
		if err := st.CreateWatchedItem(watched); err != nil {
			t.Fatal(err)
		}
		deleteErr := errors.New("final delete unavailable")
		wrapped := &deletionStore{Store: st, deleteMediaErr: deleteErr}
		bus := eventbus.New(4)
		var invalidations atomic.Int32
		bus.Subscribe(eventbus.MediaActivityAdded, func(eventbus.Event) { invalidations.Add(1) })
		bus.Start()
		err = NewService(wrapped, nil, bus, nil, t.TempDir()).DeleteMediaItem(user.ID, item.ID)
		bus.Stop()
		if !errors.Is(err, deleteErr) {
			t.Fatalf("error = %v, want final delete failure", err)
		}
		parent, err := st.GetMediaItem(item.ID)
		if err != nil || parent.Monitored || !parent.DeletionPending {
			t.Fatalf("preparation parent = %+v, %v", parent, err)
		}
		current, err := st.GetDownload(dl.ID)
		if err != nil || current.Status != "cancelled" {
			t.Fatalf("preparation download = %+v, %v", current, err)
		}
		rows := removalActivityRows(t, st, item.ID, user.ID)
		if len(rows) != 1 || rows[0].Action != store.MediaActivityActionRemovalRequested || rows[0].ActorUserID == nil || *rows[0].ActorUserID != user.ID || rows[0].OperationID == "" {
			t.Fatalf("preparation activity = %+v", rows)
		}
		details := removalActivityDetails(t, rows[0])
		if details.Total != 2 || len(details.MonitoringChanges) != 1 || len(details.FieldChanges) != 1 {
			t.Fatalf("preparation details = %+v", details)
		}
		monitoring := details.MonitoringChanges[0]
		if monitoring.Target.Scope != store.MediaActivityScopeMedia || monitoring.Before == nil || !*monitoring.Before || monitoring.After == nil || *monitoring.After || !monitoring.EffectiveBefore || monitoring.EffectiveAfter {
			t.Fatalf("monitoring diff = %+v", monitoring)
		}
		change := details.FieldChanges[0]
		if change.Field != "download.status" || change.Before == nil || *change.Before != "seeding" || change.After == nil || *change.After != "cancelled" || change.Target == nil || change.Target.ObjectID == nil || *change.Target.ObjectID != dl.ID {
			t.Fatalf("download diff = %+v", change)
		}
		for _, secret := range []string{dl.DownloadURL, dl.ClientTorrentHash, dl.LastError, alreadyCancelled.DownloadURL, alreadyCancelled.ClientTorrentHash, alreadyCancelled.LastError} {
			if strings.Contains(rows[0].Details, secret) {
				t.Fatalf("preparation activity leaked %q", secret)
			}
		}
		if invalidations.Load() != 1 {
			t.Fatalf("preparation invalidations = %d", invalidations.Load())
		}
		currentWatched, err := st.GetWatchedItem(watched.ID)
		if err != nil || currentWatched.MediaItemID == nil || *currentWatched.MediaItemID != item.ID {
			t.Fatalf("failed final deletion detached watched link: %+v, %v", currentWatched, err)
		}

		appendCount := 1
		retryStore := &activityFailingDeletionStore{
			Store: st, appendCount: &appendCount, failAt: 2, appendErr: errors.New("duplicate preparation activity"),
		}
		retryBus := eventbus.New(4)
		retryBus.Start()
		if err := NewService(retryStore, nil, retryBus, nil, t.TempDir()).DeleteMediaItem(user.ID, item.ID); err != nil {
			retryBus.Stop()
			t.Fatalf("retrying claimed deletion: %v", err)
		}
		retryBus.Stop()
		if appendCount != 1 {
			t.Fatalf("retry appended preparation activity %d times", appendCount-1)
		}
		if _, err := st.GetMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("retry did not finish claimed deletion: %v", err)
		}
	})

	t.Run("successful delete cascades preparation", func(t *testing.T) {
		st, _, item, user, _ := newRemovalFixture(t, false, "")
		bus := eventbus.New(4)
		bus.Start()
		if err := NewService(st, nil, bus, nil, t.TempDir()).DeleteMediaItem(user.ID, item.ID); err != nil {
			bus.Stop()
			t.Fatal(err)
		}
		bus.Stop()
		if _, err := st.GetMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("media item remains: %v", err)
		}
		if rows := removalActivityRows(t, st, item.ID, user.ID); len(rows) != 0 {
			t.Fatalf("successful delete retained activity: %+v", rows)
		}
	})
}

func newRemovalFixture(t *testing.T, linked bool, hash string) (*sqlite.SQLiteStore, *store.Library, *store.MediaItem, *store.User, *store.Download) {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "removal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	lib := &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Movie", MediaType: "movie", Status: "ready", Source: "disk"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	user := &store.User{Email: "remover@example.com", PasswordHash: "hash"}
	if err := st.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	dl := &store.Download{
		MediaItemID: item.ID, IndexerID: 7, IndexerName: "Safe Indexer", Title: "Movie.1080p",
		DownloadURL: "https://tracker.invalid/private", Status: "seeding", ClientTorrentHash: hash,
		LinkedToLibrary: linked, LastError: "raw-secret-error",
	}
	if err := st.CreateDownload(dl); err != nil {
		t.Fatal(err)
	}
	return st, lib, item, user, dl
}

func removalActivityRows(t *testing.T, st store.Store, itemID, userID uint) []store.MediaActivityAttribution {
	t.Helper()
	rows, hasMore, err := st.ListMediaActivityPage(itemID, userID, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore {
		t.Fatal("unexpected activity pagination")
	}
	return rows
}

func removalActivityDetails(t *testing.T, row store.MediaActivityAttribution) store.MediaActivityDetails {
	t.Helper()
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
		t.Fatal(err)
	}
	return details
}

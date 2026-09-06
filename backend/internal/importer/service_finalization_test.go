package importer

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sumia01/media-gate/internal/download"
	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

// Arm faults only AFTER the real SQLite completion CAS. A second cleanup claim
// would encounter these failures; finalization must no longer depend on it.
type cleanupFailureStore struct {
	store.Store
	fault     string
	committed bool
	claims    int
}

func (s *cleanupFailureStore) GetDownload(id uint) (*store.Download, error) {
	if s.committed && s.fault == "read" {
		return nil, errors.New("SQLITE_BUSY: cleanup GetDownload")
	}
	return s.Store.GetDownload(id)
}

func (s *cleanupFailureStore) UpdateDownload(dl *store.Download) error {
	if s.committed && s.fault == "update" {
		return errors.New("SQLITE_BUSY: cleanup UpdateDownload")
	}
	if err := s.Store.UpdateDownload(dl); err != nil {
		return err
	}
	if dl.Status == "completed" {
		s.committed = true
	}
	return nil
}

func (s *cleanupFailureStore) WithTx(fn func(store.Store) error) error {
	s.claims++
	if s.committed && s.fault == "begin" {
		return errors.New("SQLITE_BUSY: cleanup WithTx")
	}
	return s.Store.WithTx(func(tx store.Store) error {
		if err := fn(&cleanupFailureStore{Store: tx, fault: s.fault, committed: s.committed}); err != nil {
			return err
		}
		if s.committed && s.fault == "commit" {
			// Roll back the actual SQLite write at the commit boundary, rather
			// than report a failure after secretly committing the cleanup claim.
			return errors.New("SQLITE_BUSY: cleanup commit")
		}
		return nil
	})
}

func TestImportSQLiteFinalizationNeedsNoSecondCleanupClaim(t *testing.T) {
	for _, seeding := range []bool{false, true} {
		name := "import"
		if seeding {
			name = "seeding completion"
		}
		for _, fault := range []string{"begin", "read", "update", "commit"} {
			t.Run(name+"/"+fault, func(t *testing.T) {
				s, dl := newImportFixture(t, seeding)
				downloadedAt := *dl.DownloadedAt
				st := &cleanupFailureStore{Store: s, fault: fault}
				var deletes atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/auth/login":
						http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
						_, _ = w.Write([]byte("Ok."))
					case "/api/v2/app/version":
						_, _ = w.Write([]byte("v5.0.0"))
					case "/api/v2/torrents/files":
						// Deliberately wrong size: only the post-import resync fixes it.
						_, _ = w.Write([]byte(`[{"name":"Show.S01E01.mkv","size":99}]`))
					case "/api/v2/torrents/info":
						_, _ = w.Write([]byte(`[{"hash":"abc","ratio":2,"seeding_time":600}]`))
					case "/api/v2/torrents/delete":
						deletes.Add(1)
						current, err := s.GetDownload(dl.ID)
						if err != nil || current.Status != "completed" || current.CompletedAt == nil || !current.LinkedToLibrary {
							t.Errorf("cleanup without committed completion: %+v, %v", current, err)
						}
					default:
						t.Errorf("unexpected request: %s", r.URL.Path)
					}
				}))
				defer srv.Close()
				settingsSvc := settings.NewService(s, "", map[string]string{
					settings.KeyQBitURL: srv.URL, settings.KeyQBitUsername: "u", settings.KeyQBitPassword: "p",
				}, "test-secret", srv.Client())
				provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
				bus := eventbus.New(16)
				rec := newRecorder()
				bus.SubscribeAll(rec.handle)
				bus.Subscribe(eventbus.ImportCompleted, func(e eventbus.Event) {
					p, ok := e.Payload.(eventbus.ImportPayload)
					if !ok || p.DownloadID != dl.ID || p.MediaItemID != dl.MediaItemID || p.FilesCount != 1 {
						t.Errorf("invalid finalization payload: %+v", e.Payload)
					}
					current, err := s.GetDownload(dl.ID)
					if err != nil || !current.LinkedToLibrary || (current.Status != "completed" && current.Status != "seeding") {
						t.Errorf("event without persisted import: %+v, %v", current, err)
					}
				})
				bus.Start()
				defer bus.Stop()
				syncSvc := mediasync.NewService(s)
				syncSvc.SetBus(bus)
				svc := NewService(st, settingsSvc, syncSvc, bus, provider)
				svc.processOnce()
				current, err := s.GetDownload(dl.ID)
				if err != nil || current.Status != "completed" || current.CompletedAt == nil || !current.LinkedToLibrary || current.LastError != "previous error" || current.DownloadedAt == nil || !current.DownloadedAt.Equal(downloadedAt) {
					t.Fatalf("completion was not preserved: %+v, %v", current, err)
				}
				version := current.UpdatedAt
				// Normal later ticks, including a fresh service, must not repeat finalization.
				svc.processOnce()
				NewService(st, settingsSvc, syncSvc, bus, provider).processOnce()
				bus.Stop()
				wantSeedingEvents := 0
				if seeding {
					wantSeedingEvents = 1
				}
				if !st.committed || st.claims != 0 || deletes.Load() != 1 || rec.count(eventbus.ImportCompleted) != 1 || rec.count(eventbus.SeedingCompleted) != wantSeedingEvents || rec.count(eventbus.ResyncCompleted) != 1 || rec.count(eventbus.ImportFailed) != 0 {
					t.Fatalf("finalization lost or repeated: committed=%v claims=%d deletes=%d events=%+v", st.committed, st.claims, deletes.Load(), rec.events)
				}
				files, err := s.ListMediaFilesByMediaItem(dl.MediaItemID)
				if err != nil || len(files) != 1 || files[0].Size != 5 {
					t.Fatalf("post-import resync missing: %+v, %v", files, err)
				}
				item, err := s.GetMediaItem(dl.MediaItemID)
				if err != nil || item.Status != "available" {
					t.Fatalf("status was not recalculated: %+v, %v", item, err)
				}
				current, err = s.GetDownload(dl.ID)
				if err != nil || !current.UpdatedAt.Equal(version) {
					t.Fatalf("later ticks changed completed row: %+v, %v", current, err)
				}
				// Verify each armed fault really rejects the old read/update claim.
				err = st.WithTx(func(tx store.Store) error {
					row, err := tx.GetDownload(dl.ID)
					if err != nil {
						return err
					}
					return tx.UpdateDownload(row)
				})
				if err == nil {
					t.Fatal("cleanup fault was not injected")
				}
				current, err = s.GetDownload(dl.ID)
				if err != nil || current.Status != "completed" || !current.UpdatedAt.Equal(version) {
					t.Fatalf("failed cleanup claim did not roll back: %+v, %v", current, err)
				}
			})
		}
	}
}

func TestImportSQLiteCancellationAfterCompletionDoesNotRevokeFinalization(t *testing.T) {
	s, dl := newImportFixture(t, false)
	st := &importCASStore{Store: s, afterUpdate: func(update *store.Download) {
		if update.Status == "completed" {
			cancelled := *update
			cancelled.Status = "cancelled"
			if err := s.UpdateDownload(&cancelled); err != nil {
				t.Fatal(err)
			}
		}
	}}
	fq := &fakeQBit{files: []qbittorrent.TorrentFile{{Name: "Show.S01E01.mkv", Size: 5}}}
	srv := fq.server(t)
	defer srv.Close()
	bus := eventbus.New(16)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()
	defer bus.Stop()
	syncSvc := mediasync.NewService(s)
	syncSvc.SetBus(bus)
	svc := &Service{store: st, syncSvc: syncSvc, bus: bus}
	client := qbittorrent.NewClient(srv.URL, "u", "p", srv.Client())
	svc.importOne(client, dl)
	svc.importDownloaded(client)
	svc.cleanupSeeding(client)
	bus.Stop()
	// Completion CAS, not a redundant later claim, now commits authorization.
	// This is a persisted success event even though a later cancellation won.
	current, err := s.GetDownload(dl.ID)
	if err != nil || current.Status != "cancelled" || !current.LinkedToLibrary || current.CompletedAt == nil || current.DownloadedAt == nil || !current.DownloadedAt.Equal(*dl.DownloadedAt) || current.LastError != "previous error" {
		t.Fatalf("later cancellation was overwritten: %+v, %v", current, err)
	}
	if !fq.deleteCalled || rec.count(eventbus.ImportCompleted) != 1 || rec.count(eventbus.ResyncCompleted) != 1 || rec.count(eventbus.ImportFailed) != 0 {
		t.Fatal("committed import lost its authorized finalization")
	}
}

func TestImportSQLiteRejectedCompletionRemainsStartupRecoverable(t *testing.T) {
	s, dl := newImportFixture(t, false)
	st := &importCASStore{Store: s, beforeUpdate: func(update *store.Download) error {
		if update.Status == "completed" {
			return errors.New("SQLITE_BUSY: completion CAS")
		}
		return nil
	}}
	fq := &fakeQBit{files: []qbittorrent.TorrentFile{{Name: "Show.S01E01.mkv", Size: 5}}}
	srv := fq.server(t)
	defer srv.Close()
	bus := eventbus.New(16)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()
	defer bus.Stop()
	svc := &Service{store: st, syncSvc: mediasync.NewService(s), bus: bus}
	client := qbittorrent.NewClient(srv.URL, "u", "p", srv.Client())
	svc.importOne(client, dl)
	current, err := s.GetDownload(dl.ID)
	if err != nil || current.Status != "importing" || current.LinkedToLibrary || current.CompletedAt != nil || fq.deleteCalled {
		t.Fatalf("rejected completion authorized cleanup: %+v, %v", current, err)
	}
	// Use the actual startup recovery path, without requiring a configured qBit.
	settingsSvc := settings.NewService(s, "", nil, "test-secret", srv.Client())
	provider := qbittorrent.NewProvider(settingsSvc, settings.KeyQBitURL, settings.KeyQBitUsername, settings.KeyQBitPassword, srv.Client())
	download.NewService(s, settingsSvc, nil, bus, provider).Reconcile()
	current, err = s.GetDownload(dl.ID)
	if err != nil || current.Status != "downloaded" || current.DownloadedAt == nil || !current.DownloadedAt.Equal(*dl.DownloadedAt) {
		t.Fatalf("startup could not recover uncommitted import: %+v, %v", current, err)
	}
	st.beforeUpdate = nil
	svc.importDownloaded(client)
	svc.importDownloaded(client)
	bus.Stop()
	current, err = s.GetDownload(dl.ID)
	if err != nil || current.Status != "completed" || !current.LinkedToLibrary || current.CompletedAt == nil || current.DownloadedAt == nil || !current.DownloadedAt.Equal(*dl.DownloadedAt) || current.LastError != "previous error" {
		t.Fatalf("recovered import did not complete: %+v, %v", current, err)
	}
	files, err := s.ListMediaFilesByMediaItem(dl.MediaItemID)
	if err != nil || len(files) != 1 || !fq.deleteCalled || rec.count(eventbus.ImportCompleted) != 1 || rec.count(eventbus.ImportFailed) != 0 {
		t.Fatalf("recovery duplicated files/events or failed cleanup: files=%+v err=%v events=%+v", files, err, rec.events)
	}
}

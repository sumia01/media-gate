package monitor

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/media"
	"github.com/sumia01/media-gate/internal/store"
)

type deletionQBitSettings map[string]string

func (s deletionQBitSettings) Get(key string) (string, error) { return s[key], nil }

func newMonitorDeletionService(t *testing.T, st store.Store, bus *eventbus.Bus, cleanup func()) *media.Service {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/delete":
			cleanup()
		default:
			t.Errorf("unexpected qBit request: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second
	provider := qbittorrent.NewProvider(deletionQBitSettings{"url": server.URL}, "url", "user", "pass", client)
	return media.NewService(st, nil, bus, provider, t.TempDir())
}

func TestMediaDeletionPreventsMonitorRegrab(t *testing.T) {
	for _, mediaType := range []string{"movie", "series"} {
		for _, grabFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/grab_first=%t", mediaType, grabFirst), func(t *testing.T) {
				svc, st, search, item := newDecisionTest(t, mediaType)
				search.results = []indexer.TorrentResult{{Title: "Show.S01.Complete.1080p", DownloadURL: "new"}}
				if grabFirst {
					svc.processItem(item)
				} else if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Status: "downloading", DownloadURL: "old"}); err != nil {
					t.Fatal(err)
				}
				downloads, err := st.ListDownloads(&item.ID, nil)
				if err != nil || len(downloads) != 1 {
					t.Fatalf("initial downloads=%+v, err=%v", downloads, err)
				}
				downloads[0].ClientTorrentHash = "old"
				if err := st.UpdateDownload(&downloads[0]); err != nil {
					t.Fatal(err)
				}
				cleanupCalls := 0
				cleanupDone := make(chan struct{})
				deletion := newMonitorDeletionService(t, st.Store, svc.bus, func() {
					defer close(cleanupDone)
					cleanupCalls++
					parent, err := st.GetMediaItem(item.ID)
					if err != nil || parent.Monitored || parent.MonitorSearchStartedAt != nil {
						t.Errorf("cleanup started without disabled parent: %+v, %v", parent, err)
						return
					}
					before := search.calls
					svc.processOnce()
					if search.calls != before {
						t.Error("monitor searched during external deletion")
					}
					// A previously listed item must also fail the final insert gate.
					svc.processItem(item)
					during, err := st.ListDownloads(&item.ID, nil)
					if err != nil || len(during) != 1 || during[0].ID != downloads[0].ID || during[0].Status != "cancelled" {
						t.Errorf("regrab during cleanup: %+v, %v", during, err)
					}
					parent, err = st.GetMediaItem(item.ID)
					if err != nil || parent.Monitored || parent.MonitorSearchStartedAt != nil {
						t.Errorf("stale monitor restored parent settings: %+v, %v", parent, err)
					}
				})
				if err := deletion.DeleteMediaItem(item.ID); err != nil {
					t.Fatal(err)
				}
				waitForDeletionStep(t, cleanupDone)
				if cleanupCalls != 1 {
					t.Fatalf("cleanup calls=%d, want 1", cleanupCalls)
				}
				if _, err := st.GetMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("parent not deleted: %v", err)
				}
				remaining, err := st.ListDownloads(&item.ID, nil)
				if err != nil || len(remaining) != 0 {
					t.Fatalf("orphan downloads=%+v, err=%v", remaining, err)
				}
			})
		}
	}
}

func waitForDeletionStep(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for deletion/search interleaving")
	}
}

func TestInflightSearchCannotQueueAfterMediaDeletion(t *testing.T) {
	for _, mediaType := range []string{"movie", "series"} {
		for _, finishDeletion := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deleted=%t", mediaType, finishDeletion), func(t *testing.T) {
				svc, st, search, item := newDecisionTest(t, mediaType)
				search.results = []indexer.TorrentResult{{Title: "Show.S01.Complete.1080p", DownloadURL: "new"}}
				if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Status: "cancelled", ClientTorrentHash: "old"}); err != nil {
					t.Fatal(err)
				}
				searchStarted, resumeSearch, searchDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
				cleanupStarted, resumeCleanup, deletionDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
				releaseSearch := sync.OnceFunc(func() { close(resumeSearch) })
				releaseCleanup := sync.OnceFunc(func() { close(resumeCleanup) })
				deletion := newMonitorDeletionService(t, st.Store, svc.bus, func() {
					close(cleanupStarted)
					<-resumeCleanup
				})
				t.Cleanup(releaseSearch)
				t.Cleanup(releaseCleanup)
				search.after = func() {
					close(searchStarted)
					<-resumeSearch
				}
				var grabbed atomic.Int32
				svc.bus.Subscribe(eventbus.MonitorGrabbed, func(eventbus.Event) { grabbed.Add(1) })
				svc.bus.Start()
				t.Cleanup(svc.bus.Stop)
				go func() {
					defer close(searchDone)
					svc.processItem(item)
				}()
				waitForDeletionStep(t, searchStarted)
				var deleteErr error
				go func() {
					defer close(deletionDone)
					deleteErr = deletion.DeleteMediaItem(item.ID)
				}()
				waitForDeletionStep(t, cleanupStarted)
				if finishDeletion {
					releaseCleanup()
					waitForDeletionStep(t, deletionDone)
				}
				releaseSearch()
				waitForDeletionStep(t, searchDone)
				if !finishDeletion {
					parent, err := st.GetMediaItem(item.ID)
					if err != nil || parent.Monitored || parent.MonitorSearchStartedAt != nil {
						t.Errorf("search re-enabled disabled parent: %+v, %v", parent, err)
					}
					children, err := st.ListDownloads(&item.ID, nil)
					if err != nil || len(children) != 1 || children[0].Status != "cancelled" {
						t.Errorf("search queued during cleanup: %+v, %v", children, err)
					}
					releaseCleanup()
					waitForDeletionStep(t, deletionDone)
				}
				if deleteErr != nil {
					t.Fatal(deleteErr)
				}
				svc.bus.Stop()
				if grabbed.Load() != 0 {
					t.Fatalf("inflight search published %d grabs", grabbed.Load())
				}
				if _, err := st.GetMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("search recreated parent: %v", err)
				}
			})
		}
	}
}

type autoDownloadTransactionStore struct {
	store.Store
	inTx        bool
	fail        string
	steps       *[]string
	afterCommit func()
}

func (s *autoDownloadTransactionStore) WithTx(fn func(store.Store) error) error {
	err := s.Store.WithTx(func(tx store.Store) error {
		wrapped := *s
		wrapped.Store, wrapped.inTx = tx, true
		if err := fn(&wrapped); err != nil {
			return err
		}
		if s.fail == "commit" {
			return errors.New("transaction failed")
		}
		return nil
	})
	if err == nil && s.afterCommit != nil {
		s.afterCommit()
	}
	return err
}

func (s *autoDownloadTransactionStore) record(step string) error {
	if !s.inTx {
		return errors.New("auto-download check outside transaction: " + step)
	}
	*s.steps = append(*s.steps, step)
	return nil
}

func (s *autoDownloadTransactionStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if err := s.record("parent"); err != nil {
		return nil, err
	}
	if s.fail == "parent" {
		return nil, errors.New("parent read failed")
	}
	return s.Store.GetMediaItem(id)
}

func (s *autoDownloadTransactionStore) HasActiveDownloadByURL(id uint, url string) (bool, error) {
	if err := s.record("dedup"); err != nil {
		return false, err
	}
	return s.Store.HasActiveDownloadByURL(id, url)
}

func (s *autoDownloadTransactionStore) IsBlocklisted(id uint, url string, threshold int) (bool, error) {
	if err := s.record("blocklist"); err != nil {
		return false, err
	}
	return s.Store.IsBlocklisted(id, url, threshold)
}

func (s *autoDownloadTransactionStore) CreateDownload(dl *store.Download) error {
	if err := s.record("insert"); err != nil {
		return err
	}
	return s.Store.CreateDownload(dl)
}

func TestAutoDownloadFinalChecksAreTransactional(t *testing.T) {
	for _, fail := range []string{"", "parent", "commit"} {
		t.Run("failure="+fail, func(t *testing.T) {
			svc, st, _, item := newDecisionTest(t, "movie")
			var steps []string
			svc.store = &autoDownloadTransactionStore{Store: st.Store, fail: fail, steps: &steps}
			var events atomic.Int32
			svc.bus.SubscribeAll(func(eventbus.Event) { events.Add(1) })
			svc.bus.Start()
			t.Cleanup(svc.bus.Stop)
			detail := svc.createAutoDownload(item, indexer.TorrentResult{Title: "Movie.1080p", DownloadURL: "new"}, nil, nil)
			svc.bus.Stop()
			wantSteps := []string{"parent", "dedup", "blocklist", "insert"}
			if fail == "parent" {
				wantSteps = wantSteps[:1]
			}
			if !reflect.DeepEqual(steps, wantSteps) {
				t.Errorf("transaction checks=%v, want %v", steps, wantSteps)
			}
			downloads, err := st.ListDownloads(&item.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			if fail == "" {
				if detail.Outcome != "grabbed" || len(downloads) != 1 || events.Load() != 2 {
					t.Fatalf("successful insert=%+v, downloads=%+v, events=%d", detail, downloads, events.Load())
				}
			} else if detail.Outcome != "error" || len(downloads) != 0 || events.Load() != 0 {
				t.Fatalf("failed transaction leaked grab: %+v, downloads=%+v, events=%d", detail, downloads, events.Load())
			}
		})
	}
}

func TestMonitorMarkersPreserveNewerParentSettings(t *testing.T) {
	for _, clear := range []bool{false, true} {
		t.Run(fmt.Sprintf("clear=%t", clear), func(t *testing.T) {
			svc, st, _, item := newDecisionTest(t, "movie")
			profile := &store.MediaProfile{Name: "New profile"}
			if err := st.CreateMediaProfile(profile); err != nil {
				t.Fatal(err)
			}
			var changed *store.MediaItem
			changeSettings := func() {
				fresh, err := st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				fresh.Monitored = false
				fresh.MediaProfileID, fresh.PreferredRelease = &profile.ID, "NEW"
				if err := st.UpdateMediaItem(fresh); err != nil {
					t.Fatal(err)
				}
				changed, err = st.GetMediaItem(item.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if clear {
				startedAt := time.Now().Add(-time.Hour)
				if err := st.SetMonitorSearchStartedAt(item.ID, &startedAt); err != nil {
					t.Fatal(err)
				}
				item.MonitorSearchStartedAt = &startedAt
				var steps []string
				svc.store = &autoDownloadTransactionStore{Store: st.Store, steps: &steps, afterCommit: changeSettings}
				detail := svc.createAutoDownload(item, indexer.TorrentResult{Title: "Movie.1080p", DownloadURL: "new"}, nil, nil)
				if detail.Outcome != "grabbed" {
					t.Fatalf("insert before disabling failed: %+v", detail)
				}
			} else {
				changeSettings()
				svc.markSearchStarted(item)
			}
			fresh, err := st.GetMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			changed.MonitorSearchStartedAt = nil
			if fresh.MonitorSearchStartedAt != nil || !reflect.DeepEqual(fresh, changed) {
				t.Fatalf("marker overwrote newer settings: got=%+v want=%+v", fresh, changed)
			}
		})
	}
}

type staleMonitorListStore struct {
	store.Store
	item    store.MediaItem
	readErr bool
}

func (s *staleMonitorListStore) ListMonitoredMediaItems() ([]store.MediaItem, error) {
	return []store.MediaItem{s.item}, nil
}

func (s *staleMonitorListStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if s.readErr {
		return nil, errors.New("parent read failed")
	}
	return s.Store.GetMediaItem(id)
}

func TestMonitorRefreshesListedItemBeforeEvaluation(t *testing.T) {
	for _, state := range []string{"disabled", "deleted", "read_error", "changed_settings"} {
		t.Run(state, func(t *testing.T) {
			svc, st, search, item := newDecisionTest(t, "movie")
			stale := *item
			switch state {
			case "disabled":
				item.Monitored = false
				if err := st.UpdateMediaItem(item); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := st.DeleteMediaItem(item.ID); err != nil {
					t.Fatal(err)
				}
			case "changed_settings":
				item.PreferredRelease = "NEW"
				if err := st.UpdateMediaItem(item); err != nil {
					t.Fatal(err)
				}
				search.results = []indexer.TorrentResult{
					{Title: "Movie.1080p-OLD", DownloadURL: "old"},
					{Title: "Movie.1080p-NEW", DownloadURL: "new"},
				}
			}
			svc.store = &staleMonitorListStore{Store: st.Store, item: stale, readErr: state == "read_error"}
			svc.processOnce()
			if state == "changed_settings" {
				downloads, err := st.ListDownloads(&item.ID, nil)
				if err != nil || len(downloads) != 1 || downloads[0].DownloadURL != "new" || search.calls != 1 {
					t.Fatalf("stale settings evaluated: downloads=%+v calls=%d err=%v", downloads, search.calls, err)
				}
				decision := latestDecision(t, st, item.ID)
				if decision.InputUpdatedAt == nil || !decision.InputUpdatedAt.Equal(item.UpdatedAt) {
					t.Fatalf("snapshot did not capture refreshed input: %+v", decision)
				}
			} else if search.calls != 0 {
				t.Fatalf("searched unavailable parent %d times", search.calls)
			}
		})
	}
}

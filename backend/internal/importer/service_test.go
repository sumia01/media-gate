package importer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/store"
)

// stubStore embeds store.Store so tests only implement the handful of methods the
// importer actually calls; any other call nil-panics and surfaces the gap.
type stubStore struct {
	store.Store
	item      *store.MediaItem
	lib       *store.Library
	mediaFile []store.MediaFile

	mu           sync.Mutex
	updated      []store.Download
	createdFiles int
}

func (s *stubStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	if s.item != nil && s.item.ID == id {
		return s.item, nil
	}
	return nil, store.ErrNotFound
}

func (s *stubStore) GetLibrary(id uint) (*store.Library, error) {
	if s.lib != nil && s.lib.ID == id {
		return s.lib, nil
	}
	return nil, store.ErrNotFound
}

func (s *stubStore) GetMediaMetadataByMediaItem(uint) (*store.MediaMetadata, error) {
	return nil, store.ErrNotFound
}

func (s *stubStore) ListMediaFilesByMediaItem(uint) ([]store.MediaFile, error) {
	return s.mediaFile, nil
}

func (s *stubStore) CreateMediaFile(*store.MediaFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdFiles++
	return nil
}

func (s *stubStore) UpdateDownload(dl *store.Download) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, *dl)
	return nil
}

// fakeQBit records requests and serves a fixed torrent-file list.
type fakeQBit struct {
	files        []qbittorrent.TorrentFile
	mu           sync.Mutex
	deleteCalled bool
}

func (f *fakeQBit) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-sid"})
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("/api/v2/torrents/files", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.files)
	})
	mux.HandleFunc("/api/v2/torrents/delete", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.deleteCalled = true
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

// TestImportOne_NoVideoFiles_DoesNotDeleteOrComplete covers bug #3: a torrent
// that yields zero imported video files must NOT be deleted nor marked
// "completed" (data loss + permanent re-grab block). It must land in the
// non-active "import_failed" state with the data left on disk.
func TestImportOne_NoVideoFiles_DoesNotDeleteOrComplete(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "library")
	savePath := filepath.Join(root, "downloads")
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real archive-only release: a single .rar file, no video.
	if err := os.WriteFile(filepath.Join(savePath, "release.rar"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	fq := &fakeQBit{files: []qbittorrent.TorrentFile{{Name: "release.rar", Size: 4}}}
	srv := fq.server(t)
	defer srv.Close()
	client := qbittorrent.NewClient(srv.URL, "u", "p", srv.Client())

	st := &stubStore{
		item: &store.MediaItem{ID: 7, LibraryID: 3, Title: "Some Movie", MediaType: "movie"},
		lib:  &store.Library{ID: 3, Path: libPath, MediaType: "movie"},
	}
	bus := eventbus.New(16)
	rec := newRecorder()
	bus.SubscribeAll(rec.handle)
	bus.Start()

	svc := &Service{store: st, bus: bus}
	dl := &store.Download{
		ID: 42, MediaItemID: 7, IndexerID: 1, Title: "Some.Movie.2020.1080p",
		DownloadURL: "http://x/t", Status: "downloaded", ClientTorrentHash: "abc",
		SavePath: savePath,
	}

	svc.importOne(client, dl)
	bus.Stop() // drains queued events

	if dl.Status != "import_failed" {
		t.Fatalf("status = %q, want import_failed", dl.Status)
	}
	if dl.LinkedToLibrary {
		t.Error("LinkedToLibrary must stay false when nothing was imported")
	}
	if fq.deleteCalled {
		t.Error("DeleteTorrent must NOT be called for a zero-video import (data loss)")
	}
	if _, err := os.Stat(filepath.Join(savePath, "release.rar")); err != nil {
		t.Errorf("source data must be preserved on disk: %v", err)
	}
	if got := rec.count(eventbus.ImportFailed); got != 1 {
		t.Errorf("ImportFailed events = %d, want 1", got)
	}
	if got := rec.count(eventbus.ImportCompleted); got != 0 {
		t.Errorf("ImportCompleted events = %d, want 0", got)
	}
}

// TestImportOne_TransientTorrentFilesError_Retries covers bug #8: when
// GetTorrentFiles fails (qBit hiccup), the download must revert to "downloaded"
// with a backoff — NOT be marked import_failed on the first attempt.
func TestImportOne_TransientTorrentFilesError_Retries(t *testing.T) {
	root := t.TempDir()
	// Server returns 500 for the files endpoint => GetTorrentFiles errors.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("/api/v2/torrents/files", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := qbittorrent.NewClient(srv.URL, "u", "p", srv.Client())

	st := &stubStore{
		item: &store.MediaItem{ID: 7, LibraryID: 3, Title: "M", MediaType: "movie"},
		lib:  &store.Library{ID: 3, Path: filepath.Join(root, "lib"), MediaType: "movie"},
	}
	svc := &Service{store: st, bus: eventbus.New(4)}
	dl := &store.Download{ID: 1, MediaItemID: 7, IndexerID: 1, Title: "M.2020", Status: "downloaded", ClientTorrentHash: "h", SavePath: root}

	svc.importOne(client, dl)

	if dl.Status != "downloaded" {
		t.Fatalf("status = %q, want downloaded (transient retry)", dl.Status)
	}
	if dl.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", dl.RetryCount)
	}
	if dl.NextRetryAt == nil || !dl.NextRetryAt.After(time.Now()) {
		t.Error("NextRetryAt must be scheduled in the future")
	}
	if dl.LastError == "" {
		t.Error("LastError should record the transient failure")
	}
}

// TestRetryImport_ExhaustsToImportFailed verifies the bounded retry eventually
// gives up permanently.
func TestRetryImport_ExhaustsToImportFailed(t *testing.T) {
	st := &stubStore{}
	svc := &Service{store: st, bus: eventbus.New(4)}
	dl := &store.Download{ID: 1, MediaItemID: 2, Status: "downloaded", RetryCount: maxImportRetries}

	svc.retryImport(dl, "boom")

	if dl.Status != "import_failed" {
		t.Fatalf("status = %q, want import_failed after exhausting retries", dl.Status)
	}
}

type recorder struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func newRecorder() *recorder { return &recorder{} }

func (r *recorder) handle(e eventbus.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) count(t eventbus.EventType) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.Type == t {
			n++
		}
	}
	return n
}

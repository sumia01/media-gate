package notification

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

type stubStore struct {
	store.Store
	download *store.Download
	item     *store.MediaItem
	meta     *store.MediaMetadata
	webhook  *store.Setting
}

func (s *stubStore) GetDownload(uint) (*store.Download, error) {
	if s.download == nil {
		return nil, store.ErrNotFound
	}
	return s.download, nil
}

func (s *stubStore) GetMediaItem(uint) (*store.MediaItem, error) {
	if s.item == nil {
		return nil, store.ErrNotFound
	}
	return s.item, nil
}

func (s *stubStore) GetMediaMetadataByMediaItem(uint) (*store.MediaMetadata, error) {
	return s.meta, nil
}

func (s *stubStore) GetLibrary(uint) (*store.Library, error) {
	return &store.Library{Name: "Movies"}, nil
}

func (s *stubStore) GetSetting(key string) (*store.Setting, error) {
	if key == settings.KeyDiscordWebhookURL && s.webhook != nil {
		return s.webhook, nil
	}
	return nil, store.ErrNotFound
}

func (s *stubStore) SetSetting(value *store.Setting) error {
	s.webhook = value
	return nil
}

func TestFailureNotificationsUsePersistedSafeDetails(t *testing.T) {
	for _, tc := range []struct {
		name     string
		event    eventbus.EventType
		status   string
		reason   string
		category string
		want     string
	}{
		{"download exhausted", eventbus.DownloadFailed, "failed", "download retries exhausted: Get https://user:ERROR_SECRET@tracker.invalid/?apikey=TOKEN_SECRET", "Download Failed", "Could not fetch or add the torrent after all retry attempts."},
		{"download missing", eventbus.DownloadFailed, "failed", "torrent missing from qBittorrent before import", "Download Failed", "Torrent was removed from qBittorrent before import."},
		{"download client error", eventbus.DownloadFailed, "failed", "qBittorrent reported a torrent error", "Download Failed", "qBittorrent reports a torrent error."},
		{"download files missing", eventbus.DownloadFailed, "failed", "qBittorrent reported missing files", "Download Failed", "qBittorrent reports missing download files."},
		{"import exhausted", eventbus.ImportFailed, "import_failed", "GET https://user:ERROR_SECRET@qbit.invalid/?token=TOKEN_SECRET (max import retries exceeded)", "Import Failed", "Import could not finish after all retry attempts."},
		{"import file error", eventbus.ImportFailed, "import_failed", "failed to hardlink/copy file: ERROR_SECRET.mkv", "Import Failed", "Could not link or copy a downloaded video into the library."},
		{"import no videos", eventbus.ImportFailed, "import_failed", "no video files imported (archive-only release, all samples, or files rejected)", "Import Failed", "No usable video files were imported."},
		{"unknown download error", eventbus.DownloadFailed, "failed", "ERROR_SECRET https://tracker.invalid/TOKEN_SECRET @everyone <@123>", "Download Failed", "Download failed. Check MediaGate for details."},
		{"unknown import error", eventbus.ImportFailed, "import_failed", "Authorization: ERROR_SECRET Cookie: TOKEN_SECRET", "Import Failed", "Import failed. Check MediaGate for details."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan []byte, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				requests <- body
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			year := 2026
			st := &stubStore{
				download: &store.Download{
					ID: 7, MediaItemID: 12, Status: tc.status, LastError: tc.reason,
					Title: "https://tracker.invalid/TITLE_SECRET", DownloadURL: "https://tracker.invalid/DOWNLOAD_SECRET",
					IndexerName: "INDEXER_SECRET", SavePath: "/private/PATH_SECRET",
				},
				item: &store.MediaItem{ID: 12, Title: "A Movie @everyone <@123> <@&456>", Year: &year},
			}
			settingsSvc := settings.NewService(st, "", nil, "test-secret", srv.Client())
			if err := settingsSvc.Update([]settings.KeyValue{{Key: settings.KeyDiscordWebhookURL, Value: srv.URL + "/WEBHOOK_SECRET"}}); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(st.webhook.Value, "enc:") {
				t.Fatal("test must exercise existing encrypted webhook setting")
			}
			bus := eventbus.New(8)
			NewService(st, settingsSvc, bus, srv.Client())
			bus.Start()
			bus.Publish(tc.event, eventbus.DownloadPayload{DownloadID: 7, MediaItemID: 999, Title: "PAYLOAD_SECRET", Status: "stale"})
			bus.Stop()
			if len(requests) != 1 {
				t.Fatalf("webhook requests=%d, want 1", len(requests))
			}
			body := <-requests
			var payload struct {
				Embeds []struct {
					Title  string                         `json:"title"`
					Color  int                            `json:"color"`
					Fields []struct{ Name, Value string } `json:"fields"`
				} `json:"embeds"`
				AllowedMentions struct {
					Parse []string `json:"parse"`
				} `json:"allowed_mentions"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Embeds) != 1 {
				t.Fatalf("unexpected embeds: %s", body)
			}
			embed := payload.Embeds[0]
			if embed.Title != st.item.Title+" (2026)" || embed.Color != colorRed {
				t.Errorf("unexpected embed: %+v", embed)
			}
			if len(embed.Fields) != 2 || embed.Fields[0].Value != tc.category || embed.Fields[1].Value != tc.want {
				t.Errorf("unexpected fields: %+v", embed.Fields)
			}
			if payload.AllowedMentions.Parse == nil || len(payload.AllowedMentions.Parse) != 0 {
				t.Errorf("mentions not disabled: %s", body)
			}
			for _, forbidden := range []string{"SECRET", "https://", "http://", `"url":`} {
				if strings.Contains(string(body), forbidden) {
					t.Errorf("unsafe or invented detail %q in webhook: %s", forbidden, body)
				}
			}
		})
	}
}

func TestFailureNotificationsIgnoreNonTerminalAndUnavailableDownloads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  string
		event   eventbus.EventType
		payload any
		webhook bool
	}{
		{"pending retry", "pending", eventbus.DownloadFailed, eventbus.DownloadPayload{DownloadID: 1}, true},
		{"import retry", "downloaded", eventbus.ImportFailed, eventbus.DownloadPayload{DownloadID: 1}, true},
		{"cancelled", "cancelled", eventbus.DownloadFailed, eventbus.DownloadPayload{DownloadID: 1}, true},
		{"wrong failure stage", "failed", eventbus.ImportFailed, eventbus.DownloadPayload{DownloadID: 1}, true},
		{"missing download", "", eventbus.DownloadFailed, eventbus.DownloadPayload{DownloadID: 1}, true},
		{"wrong payload", "failed", eventbus.ImportFailed, eventbus.ImportPayload{DownloadID: 1}, true},
		{"webhook disabled", "failed", eventbus.DownloadFailed, eventbus.DownloadPayload{DownloadID: 1}, false},
		{"unsubscribed event", "failed", eventbus.DownloadCompleted, eventbus.DownloadPayload{DownloadID: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("unexpected notification")
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			st := &stubStore{}
			if tc.status != "" {
				st.download = &store.Download{ID: 1, Status: tc.status}
			}
			if tc.webhook {
				st.webhook = &store.Setting{Key: settings.KeyDiscordWebhookURL, Value: srv.URL}
			}
			settingsSvc := settings.NewService(st, "", nil, "test-secret", srv.Client())
			bus := eventbus.New(8)
			NewService(st, settingsSvc, bus, srv.Client())
			bus.Start()
			bus.Publish(tc.event, tc.payload)
			bus.Stop()
		})
	}
}

func TestSafeFailureTitle(t *testing.T) {
	for _, title := range []string{
		"https://user:password@tracker.invalid/torrent?apikey=secret",
		"[Release](https://tracker.invalid/secret)",
		"Release api_key=secret", "Release token:secret", "Release passkey=secret",
		"magnet:?xt=secret", "www.tracker.invalid/secret", "Release api\tkey=secret", "\t\n",
	} {
		if got := safeFailureTitle(title, 42); got != "Download #42" {
			t.Errorf("unsafe title %q yielded %q", title, got)
		}
	}
	if got := safeFailureTitle(" Some.Movie.2026.1080p ", 42); got != "Some.Movie.2026.1080p" {
		t.Errorf("safe release title changed: %q", got)
	}
	longTitle := strings.Repeat("\u00e9", 300)
	got := safeFailureTitle(longTitle, 42)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 256 || !strings.HasSuffix(got, "...") {
		t.Errorf("incorrectly truncated title: %q", got)
	}
}

func TestImportSuccessNotificationStillWorks(t *testing.T) {
	requests := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	st := &stubStore{
		download: &store.Download{ID: 1, MediaItemID: 2, Status: "completed", Title: "Movie.1080p", Size: "2 GB", IndexerName: "Indexer"},
		item:     &store.MediaItem{ID: 2, Title: "Movie", MediaType: "movie"},
		meta:     &store.MediaMetadata{Source: "tmdb", ExternalID: 123, Overview: "Overview", ImdbID: "tt123", PosterPath: "/poster.jpg"},
		webhook:  &store.Setting{Key: settings.KeyDiscordWebhookURL, Value: srv.URL},
	}
	settingsSvc := settings.NewService(st, "", nil, "test-secret", srv.Client())
	bus := eventbus.New(8)
	NewService(st, settingsSvc, bus, srv.Client())
	bus.Start()
	bus.Publish(eventbus.ImportCompleted, eventbus.ImportPayload{DownloadID: 1, MediaItemID: 2, FilesCount: 3})
	bus.Stop()
	select {
	case body := <-requests:
		for _, want := range []string{"Movie Imported to Movies", "Movie.1080p", "Overview", "www.themoviedb.org/movie/123", `"value":"3"`, `"parse":[]`} {
			if !strings.Contains(body, want) {
				t.Errorf("success notification missing %q: %s", want, body)
			}
		}
	default:
		t.Fatal("success notification not sent")
	}
}

func TestFailureNotificationFallsBackToReleaseTitle(t *testing.T) {
	requests := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	st := &stubStore{
		download: &store.Download{ID: 1, Status: "import_failed", Title: "Movie.1080p", LastError: "media item not found"},
		webhook:  &store.Setting{Key: settings.KeyDiscordWebhookURL, Value: srv.URL},
	}
	svc := &Service{store: st, settings: settings.NewService(st, "", nil, "test-secret", srv.Client()), httpClient: srv.Client()}
	svc.handleFailure(eventbus.Event{Type: eventbus.ImportFailed, Payload: eventbus.DownloadPayload{DownloadID: 1}, Timestamp: time.Now()})
	select {
	case body := <-requests:
		if !strings.Contains(body, `"title":"Movie.1080p"`) || !strings.Contains(body, "could not be found") {
			t.Errorf("missing fallback title/reason: %s", body)
		}
	default:
		t.Fatal("missing media item prevented notification")
	}
}

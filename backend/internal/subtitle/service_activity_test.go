package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

type activityTestProvider struct {
	results       []SearchResult
	download      DownloadedFile
	downloadCalls int
}

func (p *activityTestProvider) Name() string { return "test-provider" }

func (p *activityTestProvider) Search(context.Context, SearchRequest) ([]SearchResult, error) {
	return append([]SearchResult(nil), p.results...), nil
}

func (p *activityTestProvider) Download(context.Context, string) (*DownloadedFile, error) {
	p.downloadCalls++
	result := p.download
	result.Data = append([]byte(nil), result.Data...)
	return &result, nil
}

type activityFailingStore struct {
	store.Store
	err          error
	beforeAppend func()
}

type subtitleClaimStore struct {
	store.Store
	txCount  *int
	beforeTx func(int)
}

func (s *subtitleClaimStore) WithTx(fn func(store.Store) error) error {
	(*s.txCount)++
	if s.beforeTx != nil {
		s.beforeTx(*s.txCount)
	}
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&subtitleClaimStore{Store: tx, txCount: s.txCount, beforeTx: s.beforeTx})
	})
}

func (s *activityFailingStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&activityFailingStore{Store: tx, err: s.err, beforeAppend: s.beforeAppend})
	})
}

func (s *activityFailingStore) AppendMediaActivity(*store.MediaActivity) error {
	if s.beforeAppend != nil {
		s.beforeAppend()
	}
	return s.err
}

type subtitleEventRecorder struct {
	mu     sync.Mutex
	counts map[eventbus.EventType]int
}

func (r *subtitleEventRecorder) record(event eventbus.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[event.Type]++
}

func (r *subtitleEventRecorder) count(eventType eventbus.EventType) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[eventType]
}

type subtitleActivityFixture struct {
	store     *sqlite.SQLiteStore
	item      *store.MediaItem
	user      *store.User
	settings  *settings.Service
	provider  *activityTestProvider
	videoPath string
}

func newSubtitleActivityFixture(t *testing.T, mediaType string, seasonNumber, episodeNumber *int) subtitleActivityFixture {
	t.Helper()
	basePath := t.TempDir()
	db, err := sqlite.New(filepath.Join(basePath, "subtitle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	libraryPath := filepath.Join(basePath, "library")
	if err := os.MkdirAll(libraryPath, 0755); err != nil {
		t.Fatal(err)
	}
	library := &store.Library{Name: "Test", Path: libraryPath, MediaType: mediaType}
	if err := db.CreateLibrary(library); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: library.ID, Title: "Test title", MediaType: mediaType, Status: "ready", Source: "disk"}
	if err := db.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	user := &store.User{Email: "subtitle-actor@example.test", PasswordHash: "hash"}
	if err := db.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	videoName := "Test.Title.mkv"
	if seasonNumber != nil && episodeNumber != nil {
		videoName = "Test.Title.S02E04.mkv"
	}
	videoPath := filepath.Join(libraryPath, videoName)
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	mediaFile := &store.MediaFile{
		MediaItemID: item.ID, Path: videoPath, FileName: videoName, Size: 5,
		SeasonNumber: copyInt(seasonNumber), EpisodeNumber: copyInt(episodeNumber),
	}
	if err := db.CreateMediaFile(mediaFile); err != nil {
		t.Fatal(err)
	}
	provider := &activityTestProvider{download: DownloadedFile{
		FileName: "Provider.Release.srt", Format: "srt", Data: []byte("subtitle"),
	}}
	return subtitleActivityFixture{
		store: db, item: item, user: user,
		settings: settings.NewService(db, basePath, nil, "test-secret", nil),
		provider: provider, videoPath: videoPath,
	}
}

func TestManualSubtitleDownloadAppendsSafeScopedActivity(t *testing.T) {
	season, episode := 2, 4
	fixture := newSubtitleActivityFixture(t, "series", &season, &episode)
	bus, events := newSubtitleActivityBus()
	svc := NewService(fixture.store, fixture.settings, bus, []Provider{fixture.provider})

	sub, err := svc.Download(
		context.Background(), fixture.user.ID, fixture.item.ID, fixture.provider.Name(),
		"provider-file-secret", "en", &season, &episode,
		&DownloadOpts{ReleaseName: "/private/releases/Test.Release.1080p"},
	)
	if err != nil {
		bus.Stop()
		t.Fatal(err)
	}
	bus.Stop()

	rows := subtitleActivityRows(t, fixture.store, fixture.item.ID, fixture.user.ID)
	if len(rows) != 1 || rows[0].Action != store.MediaActivityActionSubtitleDownloaded {
		t.Fatalf("activity rows = %+v", rows)
	}
	if rows[0].ActorUserID == nil || *rows[0].ActorUserID != fixture.user.ID || rows[0].Visibility != store.MediaActivityVisibilityShared {
		t.Fatalf("activity actor = %+v", rows[0])
	}
	details := subtitleActivityDetailsFromRow(t, rows[0])
	if details.Target == nil || details.Target.Scope != store.MediaActivityScopeEpisode ||
		details.Target.ObjectID == nil || *details.Target.ObjectID != sub.ID ||
		details.Target.SeasonNumber == nil || *details.Target.SeasonNumber != season ||
		details.Target.EpisodeNumber == nil || *details.Target.EpisodeNumber != episode {
		t.Fatalf("activity target = %+v", details.Target)
	}
	if details.Language != "en" || details.Provider != fixture.provider.Name() ||
		details.FileName != filepath.Base(sub.FilePath) || details.ReleaseName != "Test.Release.1080p" {
		t.Fatalf("activity details = %+v", details)
	}
	for _, secret := range []string{"provider-file-secret", sub.FilePath, "/private/releases"} {
		if strings.Contains(rows[0].Details, secret) {
			t.Fatalf("activity leaked %q: %s", secret, rows[0].Details)
		}
	}
	if events.count(eventbus.SubtitleDownloaded) != 1 || events.count(eventbus.MediaActivityAdded) != 1 {
		t.Fatalf("events: downloaded=%d activity=%d", events.count(eventbus.SubtitleDownloaded), events.count(eventbus.MediaActivityAdded))
	}
}

func TestSubtitleActivityDetailsUseNaturalScope(t *testing.T) {
	season, episode := 3, 7
	tests := []struct {
		name        string
		sub         store.Subtitle
		wantScope   string
		wantSeason  bool
		wantEpisode bool
	}{
		{name: "media", sub: store.Subtitle{ID: 1}, wantScope: store.MediaActivityScopeMedia},
		{name: "season", sub: store.Subtitle{ID: 2, SeasonNumber: &season}, wantScope: store.MediaActivityScopeSeason, wantSeason: true},
		{name: "episode", sub: store.Subtitle{ID: 3, SeasonNumber: &season, EpisodeNumber: &episode}, wantScope: store.MediaActivityScopeEpisode, wantSeason: true, wantEpisode: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := subtitleActivityDetails(&test.sub).Target
			if target == nil || target.Scope != test.wantScope || target.ObjectID == nil || *target.ObjectID != test.sub.ID {
				t.Fatalf("target = %+v", target)
			}
			if (target.SeasonNumber != nil) != test.wantSeason || (target.EpisodeNumber != nil) != test.wantEpisode {
				t.Fatalf("target scope values = %+v", target)
			}
		})
	}
}

func TestManualSubtitleDownloadFailureRollsBackAndCompensatesFile(t *testing.T) {
	for _, test := range []struct {
		name      string
		actorID   func(subtitleActivityFixture) uint
		wrapStore func(subtitleActivityFixture) store.Store
		want      error
	}{
		{
			name:      "missing actor",
			actorID:   func(f subtitleActivityFixture) uint { return f.user.ID + 100 },
			wrapStore: func(f subtitleActivityFixture) store.Store { return f.store },
			want:      store.ErrActivityActorNotFound,
		},
		{
			name:    "activity append",
			actorID: func(f subtitleActivityFixture) uint { return f.user.ID },
			wrapStore: func(f subtitleActivityFixture) store.Store {
				return &activityFailingStore{Store: f.store, err: errActivityUnavailable}
			},
			want: errActivityUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSubtitleActivityFixture(t, "movie", nil, nil)
			bus, events := newSubtitleActivityBus()
			svc := NewService(test.wrapStore(fixture), fixture.settings, bus, []Provider{fixture.provider})
			_, err := svc.Download(context.Background(), test.actorID(fixture), fixture.item.ID, fixture.provider.Name(), "private-id", "en", nil, nil, nil)
			if !errors.Is(err, test.want) {
				bus.Stop()
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			bus.Stop()

			subs, listErr := fixture.store.ListSubtitlesByMediaItem(fixture.item.ID)
			if listErr != nil || len(subs) != 0 {
				t.Fatalf("rolled-back subtitles = %+v, err = %v", subs, listErr)
			}
			subtitlePath := strings.TrimSuffix(fixture.videoPath, filepath.Ext(fixture.videoPath)) + ".en.srt"
			if _, statErr := os.Stat(subtitlePath); !os.IsNotExist(statErr) {
				t.Fatalf("compensated file stat error = %v", statErr)
			}
			if rows := subtitleActivityRows(t, fixture.store, fixture.item.ID, fixture.user.ID); len(rows) != 0 {
				t.Fatalf("rolled-back activity = %+v", rows)
			}
			if events.count(eventbus.SubtitleDownloaded) != 0 || events.count(eventbus.MediaActivityAdded) != 0 {
				t.Fatalf("failed download events = %+v", events.counts)
			}
		})
	}
}

func TestManualSubtitleDownloadRejectsDeletionClaim(t *testing.T) {
	for _, finalGate := range []bool{false, true} {
		name := "preflight"
		if finalGate {
			name = "final gate"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newSubtitleActivityFixture(t, "movie", nil, nil)
			serviceStore := store.Store(fixture.store)
			if finalGate {
				txCount := 0
				serviceStore = &subtitleClaimStore{
					Store: fixture.store, txCount: &txCount,
					beforeTx: func(number int) {
						if number != 2 {
							return
						}
						item, err := fixture.store.GetMediaItem(fixture.item.ID)
						if err != nil {
							t.Fatal(err)
						}
						item.DeletionPending = true
						if err := fixture.store.UpdateMediaItem(item); err != nil {
							t.Fatal(err)
						}
					},
				}
			} else {
				fixture.item.DeletionPending = true
				if err := fixture.store.UpdateMediaItem(fixture.item); err != nil {
					t.Fatal(err)
				}
			}
			bus, events := newSubtitleActivityBus()
			svc := NewService(serviceStore, fixture.settings, bus, []Provider{fixture.provider})
			_, err := svc.Download(context.Background(), fixture.user.ID, fixture.item.ID, fixture.provider.Name(), "private-id", "en", nil, nil, nil)
			if !errors.Is(err, store.ErrMediaDeletionPending) {
				bus.Stop()
				t.Fatalf("Download() error = %v, want ErrMediaDeletionPending", err)
			}
			bus.Stop()
			wantCalls := 0
			if finalGate {
				wantCalls = 1
			}
			if fixture.provider.downloadCalls != wantCalls {
				t.Fatalf("provider download calls = %d, want %d", fixture.provider.downloadCalls, wantCalls)
			}
			subs, listErr := fixture.store.ListSubtitlesByMediaItem(fixture.item.ID)
			if listErr != nil || len(subs) != 0 {
				t.Fatalf("claimed subtitle records = %+v, %v", subs, listErr)
			}
			subtitlePath := strings.TrimSuffix(fixture.videoPath, filepath.Ext(fixture.videoPath)) + ".en.srt"
			if _, statErr := os.Stat(subtitlePath); !os.IsNotExist(statErr) {
				t.Fatalf("claimed subtitle file stat error = %v", statErr)
			}
			if rows := subtitleActivityRows(t, fixture.store, fixture.item.ID, fixture.user.ID); len(rows) != 0 {
				t.Fatalf("claimed subtitle activity = %+v", rows)
			}
			if events.count(eventbus.SubtitleDownloaded) != 0 || events.count(eventbus.MediaActivityAdded) != 0 {
				t.Fatalf("claimed subtitle events = %+v", events.counts)
			}
		})
	}
}

func TestManualSubtitleDeleteRecordsObservedCleanupAndSurvivesChildDeletion(t *testing.T) {
	for _, test := range []struct {
		name        string
		preparePath func(*testing.T, string)
		wantOutcome string
		wantExists  bool
	}{
		{
			name: "removed",
			preparePath: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("subtitle"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantOutcome: "removed",
		},
		{name: "already missing", preparePath: func(*testing.T, string) {}, wantOutcome: "already_missing"},
		{
			name: "failed",
			preparePath: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "child"), []byte("keep"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantOutcome: "failed", wantExists: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			season, episode := 2, 4
			fixture := newSubtitleActivityFixture(t, "series", &season, &episode)
			path := filepath.Join(filepath.Dir(fixture.videoPath), "delete-me.en.srt")
			test.preparePath(t, path)
			sub := &store.Subtitle{
				MediaItemID: fixture.item.ID, SeasonNumber: &season, EpisodeNumber: &episode,
				Language: "en", Provider: fixture.provider.Name(), ProviderFileID: "private-provider-id",
				FileName: filepath.Base(path), FilePath: path, ReleaseName: "/private/releases/Delete.Release", Source: "manual",
			}
			if err := fixture.store.CreateSubtitle(sub); err != nil {
				t.Fatal(err)
			}
			bus, events := newSubtitleActivityBus()
			svc := NewService(fixture.store, fixture.settings, bus, nil)
			if err := svc.Delete(fixture.user.ID, sub.ID); err != nil {
				bus.Stop()
				t.Fatal(err)
			}
			bus.Stop()

			if _, err := fixture.store.GetSubtitle(sub.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("subtitle child survived: %v", err)
			}
			_, statErr := os.Stat(path)
			if test.wantExists && statErr != nil {
				t.Fatalf("failed cleanup path missing: %v", statErr)
			}
			if !test.wantExists && !os.IsNotExist(statErr) {
				t.Fatalf("cleanup path stat error = %v", statErr)
			}
			rows := subtitleActivityRows(t, fixture.store, fixture.item.ID, fixture.user.ID)
			if len(rows) != 1 || rows[0].Action != store.MediaActivityActionSubtitleRemoved {
				t.Fatalf("activity after child deletion = %+v", rows)
			}
			details := subtitleActivityDetailsFromRow(t, rows[0])
			if details.RecordRemoved == nil || !*details.RecordRemoved || details.FileCleanupOutcome != test.wantOutcome ||
				details.Target == nil || details.Target.ObjectID == nil || *details.Target.ObjectID != sub.ID ||
				details.Target.Scope != store.MediaActivityScopeEpisode || details.FileName != filepath.Base(path) ||
				details.Language != "en" || details.Provider != fixture.provider.Name() || details.ReleaseName != "Delete.Release" {
				t.Fatalf("removal details = %+v", details)
			}
			for _, secret := range []string{sub.ProviderFileID, path, "/private/releases"} {
				if strings.Contains(rows[0].Details, secret) {
					t.Fatalf("removal activity leaked %q: %s", secret, rows[0].Details)
				}
			}
			if events.count(eventbus.SubtitleDeleted) != 1 || events.count(eventbus.MediaActivityAdded) != 1 {
				t.Fatalf("events: deleted=%d activity=%d", events.count(eventbus.SubtitleDeleted), events.count(eventbus.MediaActivityAdded))
			}
		})
	}
}

func TestManualSubtitleDeleteAppendFailureKeepsRecord(t *testing.T) {
	fixture := newSubtitleActivityFixture(t, "movie", nil, nil)
	path := filepath.Join(filepath.Dir(fixture.videoPath), "rollback.en.srt")
	original := []byte("original subtitle bytes\x00\xff")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	sub := &store.Subtitle{
		MediaItemID: fixture.item.ID, Language: "en", Provider: fixture.provider.Name(),
		FileName: filepath.Base(path), FilePath: path, Source: "manual",
	}
	if err := fixture.store.CreateSubtitle(sub); err != nil {
		t.Fatal(err)
	}
	wrapped := &activityFailingStore{Store: fixture.store, err: errActivityUnavailable}
	bus, events := newSubtitleActivityBus()
	svc := NewService(wrapped, fixture.settings, bus, nil)
	if err := svc.Delete(fixture.user.ID, sub.ID); !errors.Is(err, errActivityUnavailable) {
		bus.Stop()
		t.Fatalf("error = %v, want append failure", err)
	}
	bus.Stop()

	if _, err := fixture.store.GetSubtitle(sub.ID); err != nil {
		t.Fatalf("subtitle record did not roll back: %v", err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("restored subtitle: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("restored subtitle = %q, want %q", restored, original)
	}
	if rows := subtitleActivityRows(t, fixture.store, fixture.item.ID, fixture.user.ID); len(rows) != 0 {
		t.Fatalf("rolled-back removal activity = %+v", rows)
	}
	if events.count(eventbus.SubtitleDeleted) != 0 || events.count(eventbus.MediaActivityAdded) != 0 {
		t.Fatalf("failed delete events = %+v", events.counts)
	}
}

func TestManualSubtitleDeleteAppendFailurePreservesConcurrentReplacement(t *testing.T) {
	fixture := newSubtitleActivityFixture(t, "movie", nil, nil)
	path := filepath.Join(filepath.Dir(fixture.videoPath), "replaced.en.srt")
	if err := os.WriteFile(path, []byte("original subtitle"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := &store.Subtitle{
		MediaItemID: fixture.item.ID, Language: "en", Provider: fixture.provider.Name(),
		FileName: filepath.Base(path), FilePath: path, Source: "manual",
	}
	if err := fixture.store.CreateSubtitle(sub); err != nil {
		t.Fatal(err)
	}

	replacement := []byte("concurrent replacement")
	var replacementErr error
	wrapped := &activityFailingStore{
		Store: fixture.store,
		err:   errActivityUnavailable,
		beforeAppend: func() {
			replacementErr = os.WriteFile(path, replacement, 0644)
		},
	}
	bus, events := newSubtitleActivityBus()
	svc := NewService(wrapped, fixture.settings, bus, nil)
	if err := svc.Delete(fixture.user.ID, sub.ID); !errors.Is(err, errActivityUnavailable) {
		bus.Stop()
		t.Fatalf("error = %v, want append failure", err)
	}
	bus.Stop()
	if replacementErr != nil {
		t.Fatalf("writing concurrent replacement: %v", replacementErr)
	}

	if _, err := fixture.store.GetSubtitle(sub.ID); err != nil {
		t.Fatalf("subtitle record did not roll back: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("concurrent replacement: %v", err)
	}
	if !bytes.Equal(got, replacement) {
		t.Fatalf("subtitle = %q, want concurrent replacement %q", got, replacement)
	}
	if rows := subtitleActivityRows(t, fixture.store, fixture.item.ID, fixture.user.ID); len(rows) != 0 {
		t.Fatalf("rolled-back removal activity = %+v", rows)
	}
	if events.count(eventbus.SubtitleDeleted) != 0 || events.count(eventbus.MediaActivityAdded) != 0 {
		t.Fatalf("failed delete events = %+v", events.counts)
	}
}

func TestAutomaticSubtitleDownloadWritesNoActivity(t *testing.T) {
	fixture := newSubtitleActivityFixture(t, "movie", nil, nil)
	if err := fixture.store.CreateMediaMetadata(&store.MediaMetadata{MediaItemID: fixture.item.ID, Source: "tmdb", ExternalID: 1, Title: "Test title"}); err != nil {
		t.Fatal(err)
	}
	download := &store.Download{MediaItemID: fixture.item.ID, Title: "Test.Release", Status: "completed", LinkedToLibrary: true}
	if err := fixture.store.CreateDownload(download); err != nil {
		t.Fatal(err)
	}
	fixture.provider.results = []SearchResult{{
		ProviderName: fixture.provider.Name(), ProviderFileID: "automatic-private-id", Language: "en", ReleaseName: "Test.Release",
	}}
	settingsSvc := settings.NewService(fixture.store, filepath.Dir(filepath.Dir(fixture.videoPath)), map[string]string{
		settings.KeySubtitleAutoSearch: "true", settings.KeySubtitleLanguages: `["en"]`,
	}, "test-secret", nil)
	bus, events := newSubtitleActivityBus()
	svc := NewService(fixture.store, settingsSvc, bus, []Provider{fixture.provider})

	svc.HandleImportCompleted(eventbus.Event{Type: eventbus.ImportCompleted, Payload: eventbus.ImportPayload{
		DownloadID: download.ID, MediaItemID: fixture.item.ID,
	}})
	bus.Stop()

	subs, err := fixture.store.ListSubtitlesByMediaItem(fixture.item.ID)
	if err != nil || len(subs) != 1 || subs[0].Source != "auto" {
		t.Fatalf("automatic subtitles = %+v, err = %v", subs, err)
	}
	if rows := subtitleActivityRows(t, fixture.store, fixture.item.ID, fixture.user.ID); len(rows) != 0 {
		t.Fatalf("automatic activity = %+v", rows)
	}
	if events.count(eventbus.SubtitleDownloaded) != 1 || events.count(eventbus.SubtitleAutoSearchCompleted) != 1 || events.count(eventbus.MediaActivityAdded) != 0 {
		t.Fatalf("automatic events: downloaded=%d completed=%d activity=%d", events.count(eventbus.SubtitleDownloaded), events.count(eventbus.SubtitleAutoSearchCompleted), events.count(eventbus.MediaActivityAdded))
	}
}

var errActivityUnavailable = errors.New("activity unavailable")

func newSubtitleActivityBus() (*eventbus.Bus, *subtitleEventRecorder) {
	bus := eventbus.New(16)
	recorder := &subtitleEventRecorder{counts: make(map[eventbus.EventType]int)}
	bus.SubscribeAll(recorder.record)
	bus.Start()
	return bus, recorder
}

func subtitleActivityRows(t *testing.T, st store.Store, mediaItemID, viewerID uint) []store.MediaActivityAttribution {
	t.Helper()
	rows, _, err := st.ListMediaActivityPage(mediaItemID, viewerID, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func subtitleActivityDetailsFromRow(t *testing.T, row store.MediaActivityAttribution) store.MediaActivityDetails {
	t.Helper()
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
		t.Fatal(err)
	}
	return details
}

package sync

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

const (
	airedDate  = "2020-01-01"
	futureDate = "2099-01-01"
)

func intPtr(v int) *int { return &v }

// seriesFile builds a MediaFile carrying season/episode numbers for item 1.
func seriesFile(season, episode int) store.MediaFile {
	return store.MediaFile{
		MediaItemID:   1,
		Path:          "/lib/show/" + string(rune('a'+season)) + string(rune('a'+episode)) + ".mkv",
		SeasonNumber:  intPtr(season),
		EpisodeNumber: intPtr(episode),
	}
}

// TestRecalcMediaItemStatusStateMachine exercises the status state machine
// documented on RecalcMediaItemStatus.
func TestRecalcMediaItemStatusStateMachine(t *testing.T) {
	matched := map[uint]*store.MediaMetadata{1: {ID: 1, MediaItemID: 1, Title: "Show"}}

	cases := []struct {
		name            string
		item            store.MediaItem
		files           []store.MediaFile
		episodes        []store.Episode
		seasonMonitors  []store.SeasonMonitor
		episodeMonitors []store.EpisodeMonitor
		want            string
	}{
		// ---- movies ----
		{
			name:  "movie with file is available",
			item:  store.MediaItem{ID: 1, MediaType: "movie", Source: "disk", Status: "missing"},
			files: []store.MediaFile{{MediaItemID: 1, Path: "/lib/m/a.mkv"}},
			want:  "available",
		},
		{
			name: "requested movie without file stays requested",
			item: store.MediaItem{ID: 1, MediaType: "movie", Source: "request", Status: "available"},
			want: "requested",
		},
		{
			name: "disk movie without file is missing",
			item: store.MediaItem{ID: 1, MediaType: "movie", Source: "disk", Status: "available"},
			want: "missing",
		},
		// ---- series: the reported bug scenario ----
		{
			name: "monitored future-seasons-only with old seasons deleted is available, not missing",
			item: store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "missing", Monitored: true},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 2, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 1, AirDate: futureDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 1, Monitored: false},
				{MediaItemID: 1, SeasonNumber: 2, Monitored: true},
			},
			want: "available",
		},
		{
			name: "monitored future-seasons-only with old season files kept is available",
			item: store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "partial", Monitored: true},
			files: []store.MediaFile{
				seriesFile(1, 1), seriesFile(1, 2),
			},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 2, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 1, AirDate: futureDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 1, Monitored: false},
				{MediaItemID: 1, SeasonNumber: 2, Monitored: true},
			},
			want: "available",
		},
		// ---- series: monitored coverage ----
		{
			name: "monitored season fully covered is available even when unmonitored season is absent",
			item: store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "partial", Monitored: true},
			files: []store.MediaFile{
				seriesFile(2, 1), seriesFile(2, 2),
			},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 2, AirDate: airedDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 1, Monitored: false},
				{MediaItemID: 1, SeasonNumber: 2, Monitored: true},
			},
			want: "available",
		},
		{
			name:  "monitored season partially covered is partial",
			item:  store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "available", Monitored: true},
			files: []store.MediaFile{seriesFile(2, 1)},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 2, AirDate: airedDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 2, Monitored: true},
			},
			want: "partial",
		},
		{
			name: "monitored season with nothing covered and no files is missing",
			item: store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "available", Monitored: true},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 1, AirDate: airedDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 2, Monitored: true},
			},
			want: "missing",
		},
		{
			name:  "monitored season with nothing covered but unmonitored files present is partial",
			item:  store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "available", Monitored: true},
			files: []store.MediaFile{seriesFile(1, 1)},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 1, AirDate: airedDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 1, Monitored: false},
				{MediaItemID: 1, SeasonNumber: 2, Monitored: true},
			},
			want: "partial",
		},
		{
			name: "episode-level override wins over unmonitored season",
			item: store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "available", Monitored: true},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 2, AirDate: airedDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 1, Monitored: false},
			},
			episodeMonitors: []store.EpisodeMonitor{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 2, Monitored: true},
			},
			want: "missing", // S01E02 is wanted, uncovered, and no files exist
		},
		// ---- series: unmonitored items keep disk-completeness semantics ----
		{
			name:  "unmonitored series with all aired episodes covered is available",
			item:  store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "partial"},
			files: []store.MediaFile{seriesFile(1, 1)},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 2, AirDate: futureDate},
			},
			want: "available",
		},
		{
			name:  "unmonitored series with some aired episodes covered is partial",
			item:  store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "available"},
			files: []store.MediaFile{seriesFile(1, 1)},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 2, AirDate: airedDate},
			},
			want: "partial",
		},
		{
			name: "unmonitored series without files is missing",
			item: store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "available"},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
			},
			want: "missing",
		},
		{
			name:  "series with files but no aired episodes tracked is available",
			item:  store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "new"},
			files: []store.MediaFile{seriesFile(1, 1)},
			want:  "available",
		},
		// ---- series: requests ----
		{
			name: "requested series without files stays requested even with wanted aired episodes",
			item: store.MediaItem{ID: 1, MediaType: "series", Source: "request", Status: "requested", Monitored: true},
			episodes: []store.Episode{
				{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
			},
			seasonMonitors: []store.SeasonMonitor{
				{MediaItemID: 1, SeasonNumber: 1, Monitored: true},
			},
			want: "requested",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := tc.item
			fs := &fakeStore{
				items:           map[uint]*store.MediaItem{1: &item},
				files:           tc.files,
				metas:           matched,
				episodes:        tc.episodes,
				seasonMonitors:  tc.seasonMonitors,
				episodeMonitors: tc.episodeMonitors,
			}
			svc := NewService(fs)
			if err := svc.RecalcMediaItemStatus(1); err != nil {
				t.Fatalf("RecalcMediaItemStatus returned error: %v", err)
			}
			if got := fs.items[1].Status; got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRecalcMediaItemStatusKeepsUnmatchedNew verifies the recalculator never
// touches unmatched items: "new" is the auto-match queue marker
// (ListNewMediaItemsByLibrary) and overwriting it would exclude the item from
// MatchLibrary forever.
func TestRecalcMediaItemStatusKeepsUnmatchedNew(t *testing.T) {
	item := &store.MediaItem{ID: 1, MediaType: "series", Source: "disk", Status: "new"}
	fs := &fakeStore{
		items: map[uint]*store.MediaItem{1: item},
		files: []store.MediaFile{seriesFile(1, 1)}, // files exist, but the item is unmatched
	}
	svc := NewService(fs)
	if err := svc.RecalcMediaItemStatus(1); err != nil {
		t.Fatalf("RecalcMediaItemStatus returned error: %v", err)
	}
	if fs.items[1].Status != "new" {
		t.Fatalf("unmatched item status changed to %q, must stay \"new\"", fs.items[1].Status)
	}
}

// TestFindOrphanedMediaItemsSkipsMonitored verifies that a monitored item whose
// files all vanished from disk is never hard-deleted by a library sync — the
// user asked the app to keep tracking it (e.g. future seasons only).
func TestFindOrphanedMediaItemsSkipsMonitored(t *testing.T) {
	fs := &fakeStore{items: map[uint]*store.MediaItem{
		1: {ID: 1, Source: "disk", Status: "available"},                  // real orphan → deletable
		2: {ID: 2, Source: "disk", Status: "available", Monitored: true}, // monitored → protected
	}}
	svc := NewService(fs)

	allFiles := []store.MediaFile{
		{Path: "/lib/A/a.mkv", MediaItemID: 1},
		{Path: "/lib/B/b.mkv", MediaItemID: 2},
	}
	removed := []string{"/lib/A/a.mkv", "/lib/B/b.mkv"}
	pathToItem := map[string]uint{
		"/lib/A/a.mkv": 1,
		"/lib/B/b.mkv": 2,
	}

	orphans := svc.findOrphanedMediaItems(removed, allFiles, pathToItem)
	if len(orphans) != 1 || orphans[0] != 1 {
		t.Fatalf("expected only unmonitored item 1 as orphan, got %v", orphans)
	}
}

// TestSyncLibraryRecalcsStatusWhenFilesRemoved is the regression test for the
// reported bug: a monitored series (future seasons only) whose old-season files
// are deleted from disk must survive the library sync and settle on "available"
// (nothing it monitors is absent) instead of being stuck on "missing".
func TestSyncLibraryRecalcsStatusWhenFilesRemoved(t *testing.T) {
	libRoot := t.TempDir()

	stalePath := libRoot + "/My Show/Season 01/My Show S01E01.mkv" // not present on disk

	item := &store.MediaItem{
		ID: 1, LibraryID: 1, MediaType: "series", Source: "disk",
		Status: "available", Monitored: true,
	}
	fs := &fakeStore{
		items: map[uint]*store.MediaItem{1: item},
		files: []store.MediaFile{
			{ID: 1, MediaItemID: 1, Path: stalePath, FileName: "My Show S01E01.mkv"},
		},
		metas: map[uint]*store.MediaMetadata{1: {ID: 1, MediaItemID: 1, Title: "My Show"}},
		episodes: []store.Episode{
			{MediaItemID: 1, SeasonNumber: 1, EpisodeNumber: 1, AirDate: airedDate},
			{MediaItemID: 1, SeasonNumber: 2, EpisodeNumber: 1, AirDate: futureDate},
		},
		seasonMonitors: []store.SeasonMonitor{
			{MediaItemID: 1, SeasonNumber: 1, Monitored: false},
			{MediaItemID: 1, SeasonNumber: 2, Monitored: true},
		},
	}
	svc := NewService(fs)
	lib := &store.Library{ID: 1, Path: libRoot, MediaType: "series"}

	_, removed, err := svc.SyncLibrary(lib)
	if err != nil {
		t.Fatalf("SyncLibrary returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected the stale file record to be removed, got %d", removed)
	}
	if len(fs.deletedItems) != 0 {
		t.Fatalf("monitored item must not be deleted, deleted=%v", fs.deletedItems)
	}
	if got := fs.items[1].Status; got != "available" {
		t.Fatalf("status after sync = %q, want \"available\" (only future seasons monitored)", got)
	}
}

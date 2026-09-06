package sqlite

import (
	"fmt"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func TestTimelineFilteringBoundariesAndEnrichment(t *testing.T) {
	s := newTestStore(t)
	followed := mustCreateMediaItem(t, s)
	followed.Monitored = true
	if err := s.UpdateMediaItem(followed); err != nil {
		t.Fatal(err)
	}
	unfollowed := mustCreateMediaItem(t, s)
	movie := mustCreateMediaItem(t, s)
	movie.Monitored, movie.MediaType = true, "movie"
	if err := s.UpdateMediaItem(movie); err != nil {
		t.Fatal(err)
	}
	create := func(value any) {
		t.Helper()
		if err := s.db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	create(&store.MediaMetadata{MediaItemID: followed.ID, Source: "tmdb", ExternalID: 1, Title: "Matched show", PosterPath: "/poster.jpg"})
	create(&store.SeasonMonitor{MediaItemID: followed.ID, SeasonNumber: 1, Monitored: true})
	create(&store.SeasonMonitor{MediaItemID: followed.ID, SeasonNumber: 2, Monitored: false})
	create(&store.EpisodeMonitor{MediaItemID: followed.ID, SeasonNumber: 1, EpisodeNumber: 2, Monitored: false})
	create(&store.EpisodeMonitor{MediaItemID: followed.ID, SeasonNumber: 2, EpisodeNumber: 1, Monitored: true})
	season, episode := 1, 2
	for i := range 2 {
		create(&store.MediaFile{MediaItemID: followed.ID, Path: fmt.Sprintf("/show/%d.mkv", i), FileName: "ep.mkv", SeasonNumber: &season, EpisodeNumber: &episode})
	}
	// Deliberately insert out of order. Duplicate files must not duplicate rows.
	for _, ep := range []store.Episode{
		{MediaItemID: followed.ID, SeasonNumber: 2, EpisodeNumber: 1, AirDate: "2026-09-06"},
		{MediaItemID: followed.ID, SeasonNumber: 1, EpisodeNumber: 3, AirDate: "2026-09-19"},
		{MediaItemID: followed.ID, SeasonNumber: 1, EpisodeNumber: 2, AirDate: "2026-09-05", Title: "Available"},
		{MediaItemID: followed.ID, SeasonNumber: 1, EpisodeNumber: 1, AirDate: "2026-09-05"},
		{MediaItemID: followed.ID, SeasonNumber: 3, EpisodeNumber: 1, AirDate: "2026-09-06"},
		{MediaItemID: followed.ID, SeasonNumber: 4, EpisodeNumber: 1, AirDate: "2026-09-04"},
		{MediaItemID: followed.ID, SeasonNumber: 4, EpisodeNumber: 2},
		{MediaItemID: unfollowed.ID, SeasonNumber: 1, EpisodeNumber: 1, AirDate: "2026-09-05"},
		{MediaItemID: movie.ID, SeasonNumber: 1, EpisodeNumber: 1, AirDate: "2026-09-05"},
	} {
		create(&ep)
	}
	rows, err := s.ListTimelineEpisodes("2026-09-05", "2026-09-19")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4: %+v", len(rows), rows)
	}
	for i, want := range []struct {
		season, episode    int
		monitored, hasFile bool
	}{
		{1, 1, true, false}, {1, 2, false, true}, {2, 1, true, false}, {3, 1, false, false},
	} {
		got := rows[i]
		if got.SeasonNumber != want.season || got.EpisodeNumber != want.episode || got.Monitored != want.monitored || got.HasFile != want.hasFile || got.SeriesTitle != "Matched show" || got.PosterPath != "/poster.jpg" {
			t.Errorf("row %d = %+v, want %+v", i, got, want)
		}
	}
	rows, err = s.ListTimelineEpisodes("2026-09-19", "2026-09-20")
	if err != nil || len(rows) != 1 || rows[0].EpisodeNumber != 3 {
		t.Fatalf("adjacent window: %+v, %v", rows, err)
	}
	rows, err = s.ListTimelineEpisodes("2030-01-01", "2030-01-15")
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty future window: %+v, %v", rows, err)
	}
}

func TestTimelineNoTruncationAndItemOrder(t *testing.T) {
	s := newTestStore(t)
	for range 2 {
		item := mustCreateMediaItem(t, s)
		item.Monitored = true
		if err := s.UpdateMediaItem(item); err != nil {
			t.Fatal(err)
		}
		eps := make([]store.Episode, 600)
		for i := range eps {
			eps[i] = store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 600 - i, AirDate: "2026-09-05"}
		}
		if err := s.db.CreateInBatches(eps, 100).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ListTimelineEpisodes("2026-09-05", "2026-09-06")
	if err != nil || len(rows) != 1200 {
		t.Fatalf("got %d rows, err %v", len(rows), err)
	}
	for i, row := range rows {
		if row.EpisodeNumber != i%600+1 || row.SeriesTitle != "Show" || row.PosterPath != "" || (i > 0 && row.MediaItemID < rows[i-1].MediaItemID) {
			t.Fatalf("unexpected row %d: %+v", i, row)
		}
	}
}

func TestTimelineIndexMigration(t *testing.T) {
	s := newTestStore(t)
	if !s.db.Migrator().HasIndex(&store.Episode{}, "idx_episodes_air_date") {
		t.Fatal("fresh migration did not create the air-date index")
	}
	for _, direction := range []string{"down", "up"} {
		script, err := migrationsFS.ReadFile("migrations/0006_episode_timeline_index." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.db.Exec(string(script)).Error; err != nil {
			t.Fatal(err)
		}
		if s.db.Migrator().HasIndex(&store.Episode{}, "idx_episodes_air_date") != (direction == "up") {
			t.Fatalf("index state after %s migration is incorrect", direction)
		}
	}
}

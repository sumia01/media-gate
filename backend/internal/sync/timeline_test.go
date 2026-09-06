package sync

import (
	"errors"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

type timelineStore struct {
	store.Store         // Unexpected catalog/file/monitor reads panic.
	rows                []store.TimelineEpisode
	downloads           map[uint][]store.Download
	calls               map[uint]int
	from, to            string
	rowErr, downloadErr error
}

func (s *timelineStore) ListTimelineEpisodes(from, to string) ([]store.TimelineEpisode, error) {
	s.from, s.to = from, to
	return s.rows, s.rowErr
}

func (s *timelineStore) ListDownloads(id *uint, _ *string) ([]store.Download, error) {
	if s.calls == nil {
		s.calls = make(map[uint]int)
	}
	s.calls[*id]++
	return s.downloads[*id], s.downloadErr
}

func TestAssembleEpisodeTimelineStatusScope(t *testing.T) {
	season, id := 1, uint(1)
	fake := &timelineStore{downloads: map[uint][]store.Download{
		1: {
			{EpisodeID: &id, Status: "importing"},
			{SeasonNumber: &season, Title: "Show.S01E02.1080p", Status: "downloading"},
			{SeasonNumber: &season, Title: "Show.S01.Complete", Status: "pending"},
			{Title: "Show collection", Status: "seeding"},
		},
		2: {{SeasonNumber: &season, Title: "Other.S01E02.1080p", Status: "downloading"}},
	}}
	for i, key := range [][3]int{{1, 1, 1}, {1, 1, 2}, {1, 1, 3}, {1, 2, 1}, {2, 1, 1}, {2, 1, 2}} {
		fake.rows = append(fake.rows, store.TimelineEpisode{
			Episode:     store.Episode{ID: uint(i + 1), MediaItemID: uint(key[0]), SeasonNumber: key[1], EpisodeNumber: key[2], AirDate: "2026-09-05"},
			SeriesTitle: "Show", PosterPath: "/poster.jpg", HasFile: i == 0, Monitored: i != 1,
		})
	}
	rows, err := NewService(fake).AssembleEpisodeTimeline("2026-09-01", "2026-09-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 || fake.from != "2026-09-01" || fake.to != "2026-09-15" || fake.calls[1] != 1 || fake.calls[2] != 1 {
		t.Fatalf("unexpected result/read scope: %+v, %+v", rows, fake)
	}
	for i, want := range []string{"importing", "downloading", "pending", "seeding", "", "downloading"} {
		if rows[i].DownloadStatus != want || rows[i].Episode.ID != uint(i+1) || rows[i].SeriesTitle != "Show" || rows[i].PosterPath != "/poster.jpg" || rows[i].HasFile != (i == 0) || rows[i].Monitored != (i != 1) {
			t.Errorf("row %d = %+v, want status %q and preserved enrichment", i, rows[i], want)
		}
	}
}

func TestAssembleEpisodeTimelineEmptyAndErrors(t *testing.T) {
	boom := errors.New("storage failed")
	for _, fake := range []*timelineStore{
		{}, {rowErr: boom},
		{rows: []store.TimelineEpisode{{Episode: store.Episode{MediaItemID: 1}}}, downloadErr: boom},
	} {
		rows, err := NewService(fake).AssembleEpisodeTimeline("2026-09-01", "2026-09-15")
		if fake.rowErr != nil || fake.downloadErr != nil {
			if !errors.Is(err, boom) {
				t.Errorf("got error %v", err)
			}
		} else if err != nil || rows == nil || len(rows) != 0 || len(fake.calls) != 0 {
			t.Errorf("empty window: %+v, %v", rows, err)
		}
	}
}

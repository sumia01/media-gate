package activity

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

type downloadTargetStore struct {
	store.Store
	episodes []store.Episode
}

func (s *downloadTargetStore) ListEpisodesByMediaItem(uint) ([]store.Episode, error) {
	return s.episodes, nil
}

func TestDownloadTargetsUsesParsedScopeNotNullEpisodeID(t *testing.T) {
	item := &store.MediaItem{ID: 1, MediaType: "series"}
	season := 2
	for _, tc := range []struct {
		name  string
		dl    store.Download
		scope string
		count int
	}{
		{name: "single episode", dl: store.Download{ID: 2, MediaItemID: 1, SeasonNumber: &season, Title: "Show.S02E04.1080p"}, scope: store.MediaActivityScopeEpisode, count: 1},
		{name: "episode range", dl: store.Download{ID: 3, MediaItemID: 1, SeasonNumber: &season, Title: "Show.S02E04-E06.1080p"}, scope: store.MediaActivityScopeEpisode, count: 3},
		{name: "season pack", dl: store.Download{ID: 4, MediaItemID: 1, SeasonNumber: &season, Title: "Show.S02.1080p"}, scope: store.MediaActivityScopeSeason, count: 1},
		{name: "ambiguous", dl: store.Download{ID: 5, MediaItemID: 1, SeasonNumber: &season, Title: "Show.1080p"}, scope: store.MediaActivityScopeUnknown, count: 1},
		{name: "unnumbered special", dl: store.Download{ID: 6, MediaItemID: 1, SeasonNumber: &season, Title: "Show.S02.Special.1080p"}, scope: store.MediaActivityScopeUnknown, count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targets, total := DownloadTargets(&downloadTargetStore{}, item, &tc.dl)
			if total != tc.count || len(targets) != tc.count || targets[0].Scope != tc.scope {
				t.Fatalf("targets=%+v total=%d", targets, total)
			}
		})
	}
}

func TestDownloadTargetsEpisodeIDTakesPrecedenceOverMismatchedTitle(t *testing.T) {
	episodeID := uint(9)
	st := &downloadTargetStore{episodes: []store.Episode{{ID: episodeID, MediaItemID: 1, SeasonNumber: 3, EpisodeNumber: 7}}}
	item := &store.MediaItem{ID: 1, MediaType: "series"}
	for _, tc := range []struct {
		name         string
		title        string
		wantEpisodes []int
	}{
		{name: "ambiguous title", title: "ambiguous", wantEpisodes: []int{7}},
		{name: "mismatched single episode", title: "Show.S02E04.1080p", wantEpisodes: []int{7}},
		{name: "mismatched season pack", title: "Show.S02.Complete.1080p", wantEpisodes: []int{7}},
		{name: "range containing resolved episode", title: "Show.S03E06-E08.1080p", wantEpisodes: []int{6, 7, 8}},
		{name: "range not containing resolved episode", title: "Show.S03E08-E09.1080p", wantEpisodes: []int{7}},
		{name: "range in mismatched season", title: "Show.S02E06-E08.1080p", wantEpisodes: []int{7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dl := &store.Download{ID: 2, MediaItemID: 1, EpisodeID: &episodeID, Title: tc.title}
			targets, total := DownloadTargets(st, item, dl)
			if total != len(tc.wantEpisodes) || len(targets) != len(tc.wantEpisodes) {
				t.Fatalf("targets=%+v total=%d", targets, total)
			}
			for i, target := range targets {
				if target.Scope != store.MediaActivityScopeEpisode || target.SeasonNumber == nil || *target.SeasonNumber != 3 ||
					target.EpisodeNumber == nil || *target.EpisodeNumber != tc.wantEpisodes[i] {
					t.Fatalf("targets=%+v, want season 3 episodes %v", targets, tc.wantEpisodes)
				}
			}
		})
	}
}

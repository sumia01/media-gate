package monitor

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// TestBuildDownloadMapEpisodeRange verifies that a download whose title parses
// to a multi-episode range is keyed for EVERY episode in [Episode..EpisodeEnd],
// so the monitor treats all covered episodes as having an active download and
// does not grab overlapping releases. Regression guard for bug #10a.
func TestBuildDownloadMapEpisodeRange(t *testing.T) {
	tests := []struct {
		name         string
		downloads    []store.Download
		wantKeys     []downloadKey
		absentKeys   []downloadKey
		coveredEps   [][2]int // {season, episode} that must be covered
		uncoveredEps [][2]int // {season, episode} that must NOT be covered
	}{
		{
			name: "range without episode_id keys every episode",
			downloads: []store.Download{
				{SeasonNumber: ptr(1), Status: "downloading", Title: "Show.S01E01-E03.1080p"},
			},
			wantKeys: []downloadKey{
				{seasonNumber: 1, episodeNumber: 1},
				{seasonNumber: 1, episodeNumber: 2},
				{seasonNumber: 1, episodeNumber: 3},
			},
			coveredEps:   [][2]int{{1, 1}, {1, 2}, {1, 3}},
			uncoveredEps: [][2]int{{1, 4}},
		},
		{
			name: "range with episode_id keys episode_id plus every episode in range",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(500)), SeasonNumber: ptr(2), Status: "seeding", Title: "Show.S02E05-E08.1080p"},
			},
			wantKeys: []downloadKey{
				{episodeID: 500},
				{seasonNumber: 2, episodeNumber: 5},
				{seasonNumber: 2, episodeNumber: 6},
				{seasonNumber: 2, episodeNumber: 7},
				{seasonNumber: 2, episodeNumber: 8},
			},
			// Episodes 6-8 have their own IDs (not 500) — they must still be
			// treated as covered via the (season, episode) keys.
			coveredEps:   [][2]int{{2, 5}, {2, 6}, {2, 7}, {2, 8}},
			uncoveredEps: [][2]int{{2, 4}, {2, 9}},
		},
		{
			name: "single episode with episode_id is unchanged (no parsed key)",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(42)), Status: "downloading", Title: "Show.S01E05.720p"},
			},
			wantKeys:   []downloadKey{{episodeID: 42}},
			absentKeys: []downloadKey{{seasonNumber: 1, episodeNumber: 5}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := buildDownloadMap(tt.downloads)
			for _, k := range tt.wantKeys {
				if !m[k] {
					t.Errorf("expected key %+v in map, not found; map=%+v", k, m)
				}
			}
			for _, k := range tt.absentKeys {
				if m[k] {
					t.Errorf("unexpected key %+v in map; map=%+v", k, m)
				}
			}
			if len(m) != len(tt.wantKeys) {
				t.Errorf("map has %d keys, want %d; map=%+v", len(m), len(tt.wantKeys), m)
			}
			for _, e := range tt.coveredEps {
				if !hasActiveDownloadForEpisode(m, nil, e[0], e[1]) {
					t.Errorf("episode S%dE%d should be covered by range download", e[0], e[1])
				}
			}
			for _, e := range tt.uncoveredEps {
				if hasActiveDownloadForEpisode(m, nil, e[0], e[1]) {
					t.Errorf("episode S%dE%d should NOT be covered by range download", e[0], e[1])
				}
			}
		})
	}
}

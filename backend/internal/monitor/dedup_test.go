package monitor

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func ptr[T any](v T) *T { return &v }

func TestBuildDownloadMap(t *testing.T) {
	tests := []struct {
		name      string
		downloads []store.Download
		wantKeys  []downloadKey
	}{
		{
			name:      "empty",
			downloads: nil,
			wantKeys:  nil,
		},
		{
			name: "active download with episode_id",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(42)), Status: "downloading", Title: "Show.S01E05.720p"},
			},
			wantKeys: []downloadKey{
				{episodeID: 42},
			},
		},
		{
			name: "inactive download is ignored",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(42)), Status: "failed", Title: "Show.S01E05.720p"},
			},
			wantKeys: nil,
		},
		{
			name: "season pack without episode_id",
			downloads: []store.Download{
				{SeasonNumber: ptr(1), Status: "seeding", Title: "Show.S01.Complete.1080p"},
			},
			wantKeys: []downloadKey{
				{seasonNumber: 1},
			},
		},
		{
			name: "single episode without episode_id — tracked by parsed season+episode",
			downloads: []store.Download{
				{SeasonNumber: ptr(1), Status: "seeding", Title: "Show.S01E07.1080p"},
			},
			wantKeys: []downloadKey{
				{seasonNumber: 1, episodeNumber: 7},
			},
		},
		{
			name: "single episode without episode_id and without season_number",
			downloads: []store.Download{
				{Status: "seeding", Title: "Show.S01E07.1080p"},
			},
			wantKeys: []downloadKey{
				{seasonNumber: 1, episodeNumber: 7},
			},
		},
		{
			name: "completed download still tracked",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(10)), Status: "completed", Title: "Show.S01E01.720p"},
			},
			wantKeys: []downloadKey{
				{episodeID: 10},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := buildDownloadMap(tt.downloads)
			for _, k := range tt.wantKeys {
				if !m[k] {
					t.Errorf("expected key %+v in map, but not found", k)
				}
			}
			if len(m) != len(tt.wantKeys) {
				t.Errorf("map has %d keys, want %d; map=%+v", len(m), len(tt.wantKeys), m)
			}
		})
	}
}

func TestHasActiveDownloadForEpisode(t *testing.T) {
	tests := []struct {
		name          string
		downloads     []store.Download
		episodeID     *uint
		seasonNumber  int
		episodeNumber int
		want          bool
	}{
		{
			name: "match by episode_id",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(42)), Status: "downloading", Title: "Show.S01E05.720p"},
			},
			episodeID:     ptr(uint(42)),
			seasonNumber:  1,
			episodeNumber: 5,
			want:          true,
		},
		{
			name: "no match by episode_id",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(42)), Status: "downloading", Title: "Show.S01E05.720p"},
			},
			episodeID:     ptr(uint(99)),
			seasonNumber:  1,
			episodeNumber: 3,
			want:          false,
		},
		{
			name: "match by season pack",
			downloads: []store.Download{
				{SeasonNumber: ptr(1), Status: "seeding", Title: "Show.S01.Complete.1080p"},
			},
			episodeID:     ptr(uint(99)),
			seasonNumber:  1,
			episodeNumber: 5,
			want:          true,
		},
		{
			name: "match by parsed title when episode_id is NULL",
			downloads: []store.Download{
				{SeasonNumber: ptr(1), Status: "seeding", Title: "Ettermek.csataja.S01E07.HUN.WEB-DL.1080p"},
			},
			episodeID:     ptr(uint(999)),
			seasonNumber:  1,
			episodeNumber: 7,
			want:          true,
		},
		{
			name: "no match by parsed title — different episode",
			downloads: []store.Download{
				{SeasonNumber: ptr(1), Status: "seeding", Title: "Ettermek.csataja.S01E07.HUN.WEB-DL.1080p"},
			},
			episodeID:     ptr(uint(999)),
			seasonNumber:  1,
			episodeNumber: 8,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := buildDownloadMap(tt.downloads)
			got := hasActiveDownloadForEpisode(m, tt.episodeID, tt.seasonNumber, tt.episodeNumber)
			if got != tt.want {
				t.Errorf("hasActiveDownloadForEpisode() = %v, want %v; map=%+v", got, tt.want, m)
			}
		})
	}
}

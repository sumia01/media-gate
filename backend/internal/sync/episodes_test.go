package sync

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func ptr[T any](v T) *T { return &v }

func TestResolveDownloadStatuses(t *testing.T) {
	tests := []struct {
		name             string
		downloads        []store.Download
		wantEpisode      map[uint]string
		wantEpisodeKey   map[string]string
		wantSeason       map[int]string
		wantItemStatus   string
	}{
		{
			name:           "empty downloads",
			downloads:      nil,
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{},
			wantSeason:     map[int]string{},
			wantItemStatus: "",
		},
		{
			name: "download with episode_id goes to episode tier",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(42)), SeasonNumber: ptr(1), Status: "seeding", Title: "Show.S01E05.720p"},
			},
			wantEpisode:    map[uint]string{42: "seeding"},
			wantEpisodeKey: map[string]string{},
			wantSeason:     map[int]string{},
		},
		{
			name: "single-episode download without episode_id goes to episode-key tier",
			downloads: []store.Download{
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "seeding", Title: "Show.S01E07.HUN.WEB-DL.720p"},
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "seeding", Title: "Show.S01E08.HUN.WEB-DL.720p"},
			},
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{"S1E7": "seeding", "S1E8": "seeding"},
			wantSeason:     map[int]string{},
		},
		{
			name: "season pack without episode_id goes to season tier",
			downloads: []store.Download{
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "downloading", Title: "Show.S01.COMPLETE.720p"},
			},
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{},
			wantSeason:     map[int]string{1: "downloading"},
		},
		{
			name: "episode range goes to season tier",
			downloads: []store.Download{
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "downloading", Title: "Show.S01E01-E10.720p"},
			},
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{},
			wantSeason:     map[int]string{1: "downloading"},
		},
		{
			name: "download with no episode_id and no season_number goes to item tier",
			downloads: []store.Download{
				{EpisodeID: nil, SeasonNumber: nil, Status: "pending", Title: "Movie.2024.1080p"},
			},
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{},
			wantSeason:     map[int]string{},
			wantItemStatus: "pending",
		},
		{
			name: "completed and failed statuses are ignored",
			downloads: []store.Download{
				{EpisodeID: ptr(uint(1)), Status: "completed", Title: "Show.S01E01.720p"},
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "failed", Title: "Show.S01E02.720p"},
			},
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{},
			wantSeason:     map[int]string{},
		},
		{
			name: "higher priority status wins within same tier",
			downloads: []store.Download{
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "seeding", Title: "Show.S01E05.v1"},
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "downloading", Title: "Show.S01E05.v2"},
			},
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{"S1E5": "downloading"},
			wantSeason:     map[int]string{},
		},
		{
			name: "mixed: single-ep without episode_id does not bleed to other episodes",
			downloads: []store.Download{
				// Single-episode downloads without episode_id (created via season search)
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "seeding", Title: "Ettermek.csataja.S01E07.HUN.WEB-DL.720p.H.264-LEGION"},
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "seeding", Title: "Ettermek.csataja.S01E08.HUN.WEB-DL.720p.H.264-LEGION"},
				// Download with episode_id
				{EpisodeID: ptr(uint(3631)), SeasonNumber: ptr(1), Status: "seeding", Title: "Ettermek.csataja.S01E12.HUN.WEB-DL.1080p.H.264-LEGION"},
			},
			wantEpisode:    map[uint]string{3631: "seeding"},
			wantEpisodeKey: map[string]string{"S1E7": "seeding", "S1E8": "seeding"},
			wantSeason:     map[int]string{},
		},
		{
			name: "unparseable title with season_number falls to season tier",
			downloads: []store.Download{
				{EpisodeID: nil, SeasonNumber: ptr(1), Status: "downloading", Title: "random-garbage-title"},
			},
			wantEpisode:    map[uint]string{},
			wantEpisodeKey: map[string]string{},
			wantSeason:     map[int]string{1: "downloading"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			epStatus, epKeyStatus, seasonStatus, itemStatus := resolveDownloadStatuses(tt.downloads)

			// Check episode tier
			if len(epStatus) != len(tt.wantEpisode) {
				t.Errorf("episodeStatus length = %d, want %d", len(epStatus), len(tt.wantEpisode))
			}
			for k, v := range tt.wantEpisode {
				if got := epStatus[k]; got != v {
					t.Errorf("episodeStatus[%d] = %q, want %q", k, got, v)
				}
			}

			// Check episode-key tier
			if len(epKeyStatus) != len(tt.wantEpisodeKey) {
				t.Errorf("episodeKeyStatus length = %d, want %d", len(epKeyStatus), len(tt.wantEpisodeKey))
			}
			for k, v := range tt.wantEpisodeKey {
				if got := epKeyStatus[k]; got != v {
					t.Errorf("episodeKeyStatus[%s] = %q, want %q", k, got, v)
				}
			}

			// Check season tier
			if len(seasonStatus) != len(tt.wantSeason) {
				t.Errorf("seasonStatus length = %d, want %d", len(seasonStatus), len(tt.wantSeason))
			}
			for k, v := range tt.wantSeason {
				if got := seasonStatus[k]; got != v {
					t.Errorf("seasonStatus[%d] = %q, want %q", k, got, v)
				}
			}

			// Check item tier
			if itemStatus != tt.wantItemStatus {
				t.Errorf("itemStatus = %q, want %q", itemStatus, tt.wantItemStatus)
			}
		})
	}
}

package monitor

import (
	"testing"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/store"
)

func TestMonitorForceReleaseSelection(t *testing.T) {
	for _, mediaType := range []string{"movie", "series"} {
		for _, tc := range []struct {
			name     string
			keywords string
			force    bool
			wantURL  string
		}{
			{"forced match", " missing, flux ", true, "flux"},
			{"forced no match waits", "MISSING", true, ""},
			{"soft no match falls back", "MISSING", false, "other"},
			{"soft match preferred", "flux", false, "flux"},
			{"empty forced list unrestricted", "", true, "other"},
			{"blank forced list unrestricted", " , , ", true, "other"},
		} {
			t.Run(mediaType+"/"+tc.name, func(t *testing.T) {
				svc, st, search, item := newDecisionTest(t, mediaType)
				item.PreferredRelease, item.ForceRelease = tc.keywords, tc.force
				if err := st.UpdateMediaItem(item); err != nil {
					t.Fatal(err)
				}
				search.results = []indexer.TorrentResult{
					{Title: "Show.S01.Complete.1080p-OTHER", DownloadURL: "other"},
					{Title: "Show.S01.Complete.1080p-FLUX", DownloadURL: "flux"},
				}
				svc.processItem(item)
				downloads, err := st.ListDownloads(&item.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantURL == "" {
					if len(downloads) != 0 || item.MonitorSearchStartedAt == nil {
						t.Fatalf("forced non-match should wait: downloads=%+v item=%+v", downloads, item)
					}
				} else if len(downloads) != 1 || downloads[0].DownloadURL != tc.wantURL {
					t.Fatalf("downloads = %+v, want %s", downloads, tc.wantURL)
				}
			})
		}
	}
}

func TestForcedReleaseStillRespectsProfileAndRanking(t *testing.T) {
	svc, st, _, item := newDecisionTest(t, "movie")
	profile := &store.MediaProfile{Name: "HD", Resolutions: `["1080p","720p"]`, Languages: `[]`}
	if err := st.CreateMediaProfile(profile); err != nil {
		t.Fatal(err)
	}
	item.MediaProfileID, item.PreferredRelease = &profile.ID, "FLUX, ETHEL"
	results := []indexer.TorrentResult{
		{Title: "Show.2160p-FLUX"},
		{Title: "Show.720p-ETHEL"},
		{Title: "Show.1080p-OTHER"},
		{Title: "Show.1080p-FLUX"},
	}
	filtered := svc.filterByProfile(results, item)
	if len(filtered) != 2 || filtered[0].Title != results[3].Title || filtered[1].Title != results[1].Title {
		t.Fatalf("forced mode bypassed profile filtering/ranking: %+v", filtered)
	}
	if filtered := svc.filterByProfile(results[:1], item); len(filtered) != 0 {
		t.Fatalf("keyword match bypassed quality profile: %+v", filtered)
	}
}

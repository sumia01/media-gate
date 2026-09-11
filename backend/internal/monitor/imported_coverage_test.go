package monitor

import (
	"testing"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/store"
)

func monitorMissingSpecialFixture(t *testing.T) (*Service, *decisionTestStore, *decisionSearch, *store.MediaItem) {
	t.Helper()
	svc, st, search, item := newDecisionTest(t, "series")
	if err := st.DeleteEpisodesByMediaItem(item.ID); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 4; n++ {
		if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 2, EpisodeNumber: n, AirDate: "2014-12-16"}); err != nil {
			t.Fatal(err)
		}
		if n < 4 {
			if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "original-" + fileKey(2, n), FileName: "original.mkv", Resolution: "1080p", SeasonNumber: ptr(2), EpisodeNumber: ptr(n)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: 2, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	return svc, st, search, item
}

func TestImportedSeasonPackUsesActualEpisodeCoverage(t *testing.T) {
	for _, status := range []string{"seeding", "completed"} {
		for _, alternative := range []bool{false, true} {
			name := status + "/no alternative"
			if alternative {
				name = status + "/unused episode available"
			}
			t.Run(name, func(t *testing.T) {
				svc, st, search, item := monitorMissingSpecialFixture(t)
				pack := indexer.TorrentResult{Title: "Show.S02.720p.BluRay", DownloadURL: "https://fake.invalid/season"}
				search.results = []indexer.TorrentResult{pack}
				// Preserve the existing preference policy: 25% missing may still
				// fall back to a pack when there is no individual episode result.
				svc.processItem(item)
				downloads, err := st.ListDownloads(&item.ID, nil)
				if err != nil || len(downloads) != 1 || downloads[0].EpisodeID != nil {
					t.Fatalf("pack fallback: downloads=%+v err=%v", downloads, err)
				}
				finished := downloads[0]
				finished.Status, finished.LinkedToLibrary = status, true
				if err := st.UpdateDownload(&finished); err != nil {
					t.Fatal(err)
				}
				// The payload only contained the three episodes already owned.
				for n := 1; n <= 3; n++ {
					if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "pack-" + fileKey(2, n), FileName: "pack.mkv", SeasonNumber: ptr(2), EpisodeNumber: ptr(n)}); err != nil {
						t.Fatal(err)
					}
				}
				if alternative {
					search.results = append(search.results, indexer.TorrentResult{Title: "Show.S02E04.720p", DownloadURL: "https://fake.invalid/special"})
				}
				search.calls = 0
				svc.processItem(item)
				decision := latestDecision(t, st, item.ID)
				downloads, err = st.ListDownloads(&item.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				if search.calls != 1 {
					t.Fatalf("missing S02E04 was not searched: %+v", decision)
				}
				if alternative {
					if len(downloads) != 2 || decision.Outcome != "grabbed" {
						t.Fatalf("unused episode not queued: decision=%+v downloads=%+v", decision, downloads)
					}
					for _, dl := range downloads {
						if dl.ID != finished.ID && (dl.Title != "Show.S02E04.720p" || dl.EpisodeID == nil) {
							t.Fatalf("wrong fallback: %+v", dl)
						}
					}
				} else if len(downloads) != 1 || decision.Outcome != "no_match" {
					t.Fatalf("incomplete pack falsely covered target or was requeued: decision=%+v downloads=%+v", decision, downloads)
				}
				if exists, err := st.HasActiveDownloadByURL(item.ID, pack.DownloadURL); err != nil || !exists {
					t.Fatalf("finished release URL dedup lost: exists=%t err=%v", exists, err)
				}
			})
		}
	}
}

func TestImportedPackWithAllWantedFilesDoesNotSearch(t *testing.T) {
	svc, st, search, item := monitorMissingSpecialFixture(t)
	if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "special", FileName: "special.mkv", SeasonNumber: ptr(2), EpisodeNumber: ptr(4)}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Title: "Show.S02.720p", SeasonNumber: ptr(2), Status: "completed", LinkedToLibrary: true}); err != nil {
		t.Fatal(err)
	}
	svc.processItem(item)
	if decision := latestDecision(t, st, item.ID); decision.Outcome != "already_present" || search.calls != 0 {
		t.Fatalf("complete library searched: %+v calls=%d", decision, search.calls)
	}
}

func TestUnnumberedSpecialIsNotAnAutomaticSeasonPack(t *testing.T) {
	svc, st, search, item := monitorMissingSpecialFixture(t)
	search.results = []indexer.TorrentResult{{Title: "Show.S02.Special.White.Christmas.1080p", DownloadURL: "https://fake.invalid/special"}}
	svc.processItem(item)
	downloads, err := st.ListDownloads(&item.ID, nil)
	if err != nil || len(downloads) != 0 {
		t.Fatalf("ambiguous special auto-grabbed: %+v err=%v", downloads, err)
	}
	if decision := latestDecision(t, st, item.ID); decision.Outcome != "no_match" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	for _, status := range []string{"pending", "downloading", "downloaded", "importing", "seeding", "completed"} {
		m := buildDownloadMap([]store.Download{{Title: search.results[0].Title, SeasonNumber: ptr(2), Status: status}})
		for _, episode := range []int{1, 4, 99} {
			if hasActiveDownloadForEpisode(m, nil, 2, episode) {
				t.Errorf("unnumbered special in %s claims S02E%02d", status, episode)
			}
		}
	}
}

func TestAutoGrabRechecksFilesForEpisodeAndPack(t *testing.T) {
	for _, title := range []string{"Show.S02E04.720p", "Show.S02.720p"} {
		t.Run(title, func(t *testing.T) {
			svc, st, search, item := monitorMissingSpecialFixture(t)
			search.results = []indexer.TorrentResult{{Title: title, DownloadURL: "https://fake.invalid/release"}}
			search.after = func() {
				if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "new-special", FileName: "special.mkv", SeasonNumber: ptr(2), EpisodeNumber: ptr(4)}); err != nil {
					t.Fatal(err)
				}
			}
			svc.processItem(item)
			downloads, err := st.ListDownloads(&item.ID, nil)
			if err != nil || len(downloads) != 0 {
				t.Fatalf("queued after file appeared: %+v err=%v", downloads, err)
			}
		})
	}
}

func TestImportedEpisodeRangeRetainsActualFileCoverage(t *testing.T) {
	svc, st, search, item := newDecisionTest(t, "series")
	if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "range", FileName: "Show.S01E01-E02.mkv", SeasonNumber: ptr(1), EpisodeNumber: ptr(1)}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Title: "Show.S01E01-E02", SeasonNumber: ptr(1), Status: "completed", LinkedToLibrary: true}); err != nil {
		t.Fatal(err)
	}
	svc.processItem(item)
	if decision := latestDecision(t, st, item.ID); decision.Outcome != "already_present" || search.calls != 0 {
		t.Fatalf("imported range triggered redundant search: %+v calls=%d", decision, search.calls)
	}
	for _, tc := range []struct {
		file   store.MediaFile
		absent string
	}{
		{store.MediaFile{FileName: "Show.S01E01-E03.mkv", SeasonNumber: ptr(2), EpisodeNumber: ptr(1)}, "2x2"},
		{store.MediaFile{FileName: "Show.S01E01-E03.mkv", SeasonNumber: ptr(1), EpisodeNumber: ptr(4)}, "1x2"},
		{store.MediaFile{FileName: "Show.S01E01-E03.mkv", SeasonNumber: ptr(1)}, "1x2"},
	} {
		if buildFileMap([]store.MediaFile{tc.file})[tc.absent] {
			t.Errorf("range overrode missing/conflicting file identity: %+v", tc.file)
		}
	}
}

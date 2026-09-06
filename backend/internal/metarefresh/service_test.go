package metarefresh

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

type claimDuringBackfillStore struct {
	store.Store
	claim func()
}

func (s *claimDuringBackfillStore) ListDownloads(itemID *uint, status *string) ([]store.Download, error) {
	downloads, err := s.Store.ListDownloads(itemID, status)
	if err == nil {
		s.claim()
	}
	return downloads, err
}

func TestOrphanBackfillDefersImportingAndLosesToConcurrentClaim(t *testing.T) {
	for _, concurrentClaim := range []bool{false, true} {
		name := "already importing"
		if concurrentClaim {
			name = "claim after metadata snapshot"
		}
		t.Run(name, func(t *testing.T) {
			s, err := sqlite.New(filepath.Join(t.TempDir(), "metadata.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			lib := &store.Library{Name: "Shows", Path: t.TempDir(), MediaType: "series"}
			if err := s.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: "series", Source: "disk"}
			if err := s.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			ep := &store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1}
			if err := s.CreateEpisode(ep); err != nil {
				t.Fatal(err)
			}
			downloadedAt := time.Now().Add(-time.Hour)
			dl := &store.Download{MediaItemID: item.ID, Title: "Show.S01E01", Status: "downloaded", LastError: "previous error", DownloadedAt: &downloadedAt}
			if err := s.CreateDownload(dl); err != nil {
				t.Fatal(err)
			}
			claim := func() {
				dl.Status = "importing"
				if err := s.UpdateDownload(dl); err != nil {
					t.Fatal(err)
				}
			}
			var st store.Store = s
			if concurrentClaim {
				st = &claimDuringBackfillStore{Store: s, claim: claim}
			} else {
				claim()
			}
			(&Service{store: st}).resolveOrphanDownloads(item.ID)
			got, err := s.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "importing" || got.EpisodeID != nil || !got.UpdatedAt.Equal(dl.UpdatedAt) {
				t.Fatalf("backfill invalidated claimed import: %+v", got)
			}
			// The owning import can still persist a retry using its original claim.
			dl.Status = "downloaded"
			if err := s.UpdateDownload(dl); err != nil {
				t.Fatalf("import retry rejected after metadata pass: %v", err)
			}
			stale := *dl
			(&Service{store: s}).resolveOrphanDownloads(item.ID)
			got, err = s.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.EpisodeID == nil || *got.EpisodeID != ep.ID || got.LastError != "previous error" || got.DownloadedAt == nil || !got.DownloadedAt.Equal(downloadedAt) {
				t.Fatalf("later metadata pass did not safely backfill: %+v", got)
			}
			stale.Status = "importing"
			if err := s.UpdateDownload(&stale); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("stale claim overwrote metadata: %v", err)
			}
		})
	}
}

package matching

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

type episodeMatchStore struct {
	store.Store
	failActivity bool
}

func (s *episodeMatchStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&episodeMatchStore{Store: tx, failActivity: s.failActivity})
	})
}

func (s *episodeMatchStore) AppendMediaActivity(entry *store.MediaActivity) error {
	if s.failActivity {
		return errors.New("activity unavailable")
	}
	return s.Store.AppendMediaActivity(entry)
}

func TestRematchPreservesEpisodeReferencesForSameIdentity(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         string
		changeIdentity bool
		changeSource   bool
		rollback       bool
	}{
		{name: "seeding", status: "seeding"},
		{name: "importing snapshot", status: "importing"},
		{name: "completed", status: "completed"},
		{name: "different identity", status: "seeding", changeIdentity: true},
		{name: "different provider", status: "seeding", changeSource: true},
		{name: "atomic rollback", status: "importing", rollback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sqlite.New(filepath.Join(t.TempDir(), "match.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			st := &episodeMatchStore{Store: db, failActivity: tc.rollback}
			lib := &store.Library{Name: "Shows", Path: t.TempDir(), MediaType: "series"}
			if err := st.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			user := &store.User{Email: "match@example.test", PasswordHash: "fixture"}
			if err := st.CreateUser(user); err != nil {
				t.Fatal(err)
			}
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Black Mirror", MediaType: "series", Source: "disk", Status: "partial"}
			if err := st.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			meta := &store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 42009, Title: "Black Mirror"}
			if err := st.CreateMediaMetadata(meta); err != nil {
				t.Fatal(err)
			}
			runtime := 60
			ep := &store.Episode{MediaItemID: item.ID, SeasonNumber: 2, EpisodeNumber: 4, Title: "Old title", Overview: "Old overview", Runtime: &runtime, AirDate: "2014-12-16"}
			if err := st.CreateEpisode(ep); err != nil {
				t.Fatal(err)
			}
			removed := &store.Episode{MediaItemID: item.ID, SeasonNumber: 2, EpisodeNumber: 3}
			if err := st.CreateEpisode(removed); err != nil {
				t.Fatal(err)
			}
			dl := &store.Download{MediaItemID: item.ID, EpisodeID: &ep.ID, SeasonNumber: &ep.SeasonNumber, Title: "Black.Mirror.S02.Special.White.Christmas.1080p", Status: tc.status}
			if err := st.CreateDownload(dl); err != nil {
				t.Fatal(err)
			}
			removedDL := &store.Download{MediaItemID: item.ID, EpisodeID: &removed.ID, Title: "Show.S02E03", Status: "completed"}
			if err := st.CreateDownload(removedDL); err != nil {
				t.Fatal(err)
			}
			snapshot := *dl
			svc := NewService(st, nil, t.TempDir(), http.DefaultClient)
			expected, err := svc.captureMatchVersion(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			candidate := *meta
			if tc.changeIdentity {
				candidate.ExternalID++
			}
			if tc.changeSource {
				candidate.Source = "tvdb"
			}
			_, _, err = svc.replaceMatch(expected, &candidate, []episodeData{
				{seasonNumber: 2, episodeNumber: 4, title: "White Christmas"},
				{seasonNumber: 2, episodeNumber: 5, title: "New episode"},
			}, matchActor{userID: user.ID})
			if (err != nil) != tc.rollback {
				t.Fatalf("replace error=%v, rollback=%t", err, tc.rollback)
			}
			fresh, err := st.GetEpisodeByNumber(item.ID, 2, 4)
			if err != nil {
				t.Fatal(err)
			}
			current, err := st.GetDownload(dl.ID)
			if err != nil {
				t.Fatal(err)
			}
			removedCurrent, err := st.GetDownload(removedDL.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.rollback:
				if fresh.ID != ep.ID || fresh.Title != "Old title" || current.EpisodeID == nil || *current.EpisodeID != ep.ID || removedCurrent.EpisodeID == nil {
					t.Fatalf("rollback lost original catalog or references: episode=%+v download=%+v", fresh, current)
				}
				if _, err := st.GetEpisodeByNumber(item.ID, 2, 5); !errors.Is(err, store.ErrNotFound) {
					t.Fatal("rolled-back episode survived")
				}
			case tc.changeIdentity || tc.changeSource:
				if fresh.ID == ep.ID || current.EpisodeID != nil {
					t.Fatal("different provider identity reused episode semantics")
				}
			default:
				if fresh.ID != ep.ID || !fresh.CreatedAt.Equal(ep.CreatedAt) || current.EpisodeID == nil || *current.EpisodeID != ep.ID {
					t.Fatal("same identity lost stable episode reference")
				}
				if fresh.Title != "White Christmas" || fresh.Overview != "" || fresh.AirDate != "" || fresh.Runtime != nil {
					t.Fatalf("metadata not updated, including zero values: %+v", fresh)
				}
				if removedCurrent.EpisodeID != nil {
					t.Fatal("removed episode's FK was not cleared")
				}
				if _, err := st.GetEpisodeByNumber(item.ID, 2, 5); err != nil {
					t.Fatal(err)
				}
				if !current.UpdatedAt.Equal(snapshot.UpdatedAt) {
					t.Fatal("rematch invalidated download worker's CAS version")
				}
				if err := st.UpdateDownload(&snapshot); err != nil {
					t.Fatalf("owned download snapshot became stale: %v", err)
				}
			}
		})
	}
}

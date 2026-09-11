package sqlite

import (
	"errors"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func TestUpdateEpisodeCannotChangeIdentityOrResurrectRows(t *testing.T) {
	st := newTestStore(t)
	item := mustCreateMediaItem(t, st)
	episode := &store.Episode{MediaItemID: item.ID, SeasonNumber: 2, EpisodeNumber: 4, Title: "Original"}
	if err := st.CreateEpisode(episode); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*store.Episode){
		func(ep *store.Episode) { ep.ID = 0 },
		func(ep *store.Episode) { ep.MediaItemID++ },
		func(ep *store.Episode) { ep.SeasonNumber++ },
		func(ep *store.Episode) { ep.EpisodeNumber++ },
	} {
		changed := *episode
		alter(&changed)
		changed.Title = "Wrong"
		if err := st.UpdateEpisode(&changed); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("identity-changing update: %v", err)
		}
	}
	current, err := st.GetEpisodeByNumber(item.ID, 2, 4)
	if err != nil || current.Title != "Original" {
		t.Fatalf("identity update corrupted row: %+v %v", current, err)
	}
	if err := st.DeleteEpisode(episode.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateEpisode(episode); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted episode update: %v", err)
	}
	if _, err := st.GetEpisodeByNumber(item.ID, 2, 4); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("episode resurrected: %v", err)
	}
}

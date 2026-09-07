package sqlite

import (
	"errors"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func TestWatchedIdentityIncludesMediaType(t *testing.T) {
	s := newTestStore(t)
	user := &store.User{Email: "watched@example.com", PasswordHash: "hash"}
	if err := s.CreateUser(user); err != nil {
		t.Fatal(err)
	}

	movie := &store.WatchedItem{UserID: user.ID, Source: "tmdb", ExternalID: 42, MediaType: "movie", Title: "Movie"}
	series := &store.WatchedItem{UserID: user.ID, Source: "tmdb", ExternalID: 42, MediaType: "series", Title: "Series"}
	if err := s.CreateWatchedItem(movie); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateWatchedItem(series); err != nil {
		t.Fatalf("same numeric TMDB ID for series: %v", err)
	}
	if err := s.CreateWatchedItem(&store.WatchedItem{
		UserID: user.ID, Source: "tmdb", ExternalID: 42, MediaType: "movie", Title: "Duplicate",
	}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("exact duplicate error = %v, want ErrDuplicate", err)
	}

	gotMovie, err := s.GetWatchedBySourceExternal(&user.ID, "tmdb", "movie", 42)
	if err != nil || gotMovie.ID != movie.ID {
		t.Fatalf("movie lookup = %+v, %v", gotMovie, err)
	}
	gotSeries, err := s.GetWatchedBySourceExternal(&user.ID, "tmdb", "series", 42)
	if err != nil || gotSeries.ID != series.ID {
		t.Fatalf("series lookup = %+v, %v", gotSeries, err)
	}

	byID, err := s.GetWatchedItem(series.ID)
	if err != nil || byID.MediaType != "series" {
		t.Fatalf("GetWatchedItem = %+v, %v", byID, err)
	}
	if _, err := s.GetWatchedItem(series.ID + 1000); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing GetWatchedItem error = %v, want ErrNotFound", err)
	}
}

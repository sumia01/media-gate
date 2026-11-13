package sqlite

import (
	"errors"
	"reflect"
	"testing"
	"time"

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

func TestWatchedPagesKeepOrderAndPrivacyAcrossDeletes(t *testing.T) {
	s := newTestStore(t)
	users := []*store.User{{Email: "first@watched.test", PasswordHash: "hash"}, {Email: "second@watched.test", PasswordHash: "hash"}}
	for _, user := range users {
		if err := s.CreateUser(user); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 11, 13, 12, 0, 0, 123456789, time.UTC)
	var created []store.WatchedItem
	for i, offset := range []time.Duration{0, -time.Hour, 0, -2 * time.Hour, 0, -time.Hour, 0, -3 * time.Hour} {
		item := store.WatchedItem{UserID: users[i%2].ID, Source: "tmdb", ExternalID: i + 1, MediaType: "movie", Title: "Fixture", WatchedAt: base.Add(offset)}
		if err := s.CreateWatchedItem(&item); err != nil {
			t.Fatal(err)
		}
		created = append(created, item)
	}
	first, more, err := s.ListWatchedItemsPage(nil, nil, 2)
	if err != nil || !more || len(first) != 2 || first[0].ID != created[6].ID || first[1].ID != created[4].ID {
		t.Fatalf("first page = %+v, more=%v, err=%v", first, more, err)
	}
	cursor := &store.WatchedItemCursor{WatchedAt: first[1].WatchedAt, ID: first[1].ID}
	// Both a previously rendered item and the cursor anchor can disappear.
	for _, item := range first {
		if err := s.DeleteWatchedItem(item.ID); err != nil {
			t.Fatal(err)
		}
	}
	newer := &store.WatchedItem{UserID: users[1].ID, Source: "tmdb", ExternalID: 100, MediaType: "movie", Title: "New", WatchedAt: base.Add(time.Hour)}
	if err := s.CreateWatchedItem(newer); err != nil {
		t.Fatal(err)
	}
	var got []uint
	for {
		page, hasMore, err := s.ListWatchedItemsPage(nil, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page {
			got = append(got, item.ID)
		}
		if !hasMore {
			break
		}
		if len(got) > len(created) {
			t.Fatal("watched pages did not terminate")
		}
		last := page[len(page)-1]
		cursor = &store.WatchedItemCursor{WatchedAt: last.WatchedAt, ID: last.ID}
	}
	want := []uint{created[2].ID, created[0].ID, created[5].ID, created[1].ID, created[3].ID, created[7].ID}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remaining pages = %v, want %v", got, want)
	}
	// Filtering must happen before limiting, on both the first and later pages.
	var privateCursor *store.WatchedItemCursor
	for range 4 {
		page, hasMore, err := s.ListWatchedItemsPage(&users[0].ID, privateCursor, 1)
		if err != nil || len(page) != 1 || page[0].UserID != users[0].ID {
			t.Fatalf("private page = %+v, more=%v, err=%v", page, hasMore, err)
		}
		privateCursor = &store.WatchedItemCursor{WatchedAt: page[0].WatchedAt, ID: page[0].ID}
		if !hasMore {
			break
		}
	}
	for _, limit := range []int{-1, 0, store.MaxWatchedPageSize + 1} {
		if _, _, err := s.ListWatchedItemsPage(nil, nil, limit); err == nil {
			t.Fatalf("accepted invalid limit %d", limit)
		}
	}
}

func TestWatchedPagesIncludeUndatedLegacyItems(t *testing.T) {
	s := newTestStore(t)
	user := &store.User{Email: "legacy@watched.test", PasswordHash: "hash"}
	if err := s.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		item := &store.WatchedItem{UserID: user.ID, Source: "tmdb", ExternalID: i + 1, MediaType: "movie", Title: "Undated"}
		if err := s.CreateWatchedItem(item); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if err := s.db.Model(item).Update("watched_at", nil).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var before *store.WatchedItemCursor
	var got []uint
	for range 4 {
		items, more, err := s.ListWatchedItemsPage(nil, before, 1)
		if err != nil || len(items) != 1 {
			t.Fatalf("undated page = %+v, more=%v, err=%v", items, more, err)
		}
		got = append(got, items[0].ID)
		if !more {
			break
		}
		before = &store.WatchedItemCursor{WatchedAt: items[0].WatchedAt, ID: items[0].ID}
	}
	if !reflect.DeepEqual(got, []uint{3, 2, 1}) {
		t.Fatalf("legacy traversal = %v", got)
	}
}

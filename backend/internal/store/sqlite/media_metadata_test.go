package sqlite

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
)

func TestListMediaMetadataExternalIDsIncludesJoinedMediaType(t *testing.T) {
	s := newTestStore(t)
	lib := &store.Library{Name: "Mixed", Path: t.TempDir(), MediaType: "movie"}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	want := make(map[uint]store.MediaMetadata)
	for _, identity := range []struct{ source, mediaType string }{
		{"tmdb", "movie"}, {"tmdb", "series"}, {"tvdb", "series"},
	} {
		item := &store.MediaItem{LibraryID: lib.ID, Title: "Title", MediaType: identity.mediaType}
		if err := s.CreateMediaItem(item); err != nil {
			t.Fatal(err)
		}
		meta := store.MediaMetadata{MediaItemID: item.ID, Source: identity.source, ExternalID: 42, Title: "Title"}
		if err := s.CreateMediaMetadata(&meta); err != nil {
			t.Fatal(err)
		}
		meta.MediaType = identity.mediaType
		want[item.ID] = meta
	}
	// Unmatched items have no provider identity and must not leak into the result.
	if err := s.CreateMediaItem(&store.MediaItem{LibraryID: lib.ID, Title: "Unmatched", MediaType: "series"}); err != nil {
		t.Fatal(err)
	}
	queries := 0
	if err := s.db.Callback().Query().After("gorm:query").Register("count_external_ids", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListMediaMetadataExternalIDs()
	if err != nil {
		t.Fatal(err)
	}
	if queries != 1 {
		t.Fatalf("got %d queries, want one joined query", queries)
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		expected, ok := want[row.MediaItemID]
		if !ok || row.Source != expected.Source || row.ExternalID != expected.ExternalID || row.MediaType != expected.MediaType {
			t.Errorf("unexpected identity: %#v; want %#v", row, expected)
		}
		if row.Title != "" {
			t.Error("identity query should not load full metadata")
		}
	}
}

func TestMediaMetadataMediaTypeIsReadOnly(t *testing.T) {
	s := newTestStore(t)
	if s.db.Migrator().HasColumn(&store.MediaMetadata{}, "media_type") {
		t.Fatal("media_type must not be a metadata column")
	}
	item := mustCreateMediaItem(t, s)
	meta := &store.MediaMetadata{MediaItemID: item.ID, Source: "tmdb", ExternalID: 42, Title: "Original", MediaType: "movie"}
	if err := s.CreateMediaMetadata(meta); err != nil {
		t.Fatalf("create must ignore projected media type: %v", err)
	}
	meta.Title = "Updated"
	if err := s.UpdateMediaMetadata(meta); err != nil {
		t.Fatalf("update must ignore projected media type: %v", err)
	}
	got, err := s.GetMediaMetadataByMediaItem(item.ID)
	if err != nil || got.Title != "Updated" || got.MediaType != "" {
		t.Fatalf("normal metadata read: %#v, %v", got, err)
	}
	rows, err := s.ListMediaMetadataExternalIDs()
	if err != nil || len(rows) != 1 || rows[0].MediaType != "series" {
		t.Fatalf("type must come from media item, not projected write: %#v, %v", rows, err)
	}
}

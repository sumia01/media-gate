package sqlite

import (
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

// TestListDownloadsPopulatesMediaItemTitle guards bug #22: the media_item_title
// alias selected by ListDownloads must be scanned into Download.MediaItemTitle.
// Previously the field carried gorm:"-" which made GORM ignore the aliased
// column, so the title was always empty.
func TestListDownloadsPopulatesMediaItemTitle(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s) // Title: "Show"

	dl := &store.Download{
		MediaItemID: item.ID,
		IndexerID:   1,
		IndexerName: "idx",
		Title:       "Show.S01E01.1080p",
		DownloadURL: "magnet:?x",
		Status:      "pending",
	}
	if err := s.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload: %v", err)
	}

	got, err := s.ListDownloads(&item.ID, nil)
	if err != nil {
		t.Fatalf("ListDownloads: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 download, got %d", len(got))
	}
	if got[0].MediaItemTitle != "Show" {
		t.Fatalf("MediaItemTitle not populated from JOIN alias: got %q, want %q",
			got[0].MediaItemTitle, "Show")
	}
}

func TestListDownloadsPageAppliesChronologicalOrderingLimitAndFilter(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	base := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	downloads := []store.Download{
		{MediaItemID: item.ID, IndexerID: 1, IndexerName: "idx", Title: "completed-new", DownloadURL: "url-1", Status: "completed", CreatedAt: base.Add(5 * time.Hour)},
		{MediaItemID: item.ID, IndexerID: 1, IndexerName: "idx", Title: "pending", DownloadURL: "url-2", Status: "pending", CreatedAt: base.Add(2 * time.Hour)},
		{MediaItemID: item.ID, IndexerID: 1, IndexerName: "idx", Title: "downloading", DownloadURL: "url-3", Status: "downloading", CreatedAt: base},
		{MediaItemID: item.ID, IndexerID: 1, IndexerName: "idx", Title: "completed-old", DownloadURL: "url-4", Status: "completed", CreatedAt: base.Add(time.Hour)},
		{MediaItemID: item.ID, IndexerID: 1, IndexerName: "idx", Title: "failed", DownloadURL: "url-5", Status: "failed", CreatedAt: base.Add(4 * time.Hour)},
	}
	for i := range downloads {
		if err := s.CreateDownload(&downloads[i]); err != nil {
			t.Fatalf("CreateDownload(%s): %v", downloads[i].Title, err)
		}
	}

	page, hasMore, err := s.ListDownloadsPage(nil, nil, 3)
	if err != nil {
		t.Fatalf("ListDownloadsPage: %v", err)
	}
	if !hasMore {
		t.Fatal("hasMore = false, want true")
	}
	if len(page) != 3 {
		t.Fatalf("page length = %d, want 3", len(page))
	}
	for i, want := range []string{"completed-new", "failed", "pending"} {
		if page[i].Title != want {
			t.Errorf("page[%d].Title = %q, want %q", i, page[i].Title, want)
		}
	}

	status := "completed"
	page, hasMore, err = s.ListDownloadsPage(nil, &status, 1)
	if err != nil {
		t.Fatalf("ListDownloadsPage filtered: %v", err)
	}
	if !hasMore {
		t.Fatal("filtered hasMore = false, want true")
	}
	if len(page) != 1 || page[0].Title != "completed-new" {
		t.Fatalf("filtered page = %#v, want completed-new", page)
	}

	page, hasMore, err = s.ListDownloadsPage(nil, &status, 2)
	if err != nil {
		t.Fatalf("ListDownloadsPage exact boundary: %v", err)
	}
	if hasMore {
		t.Fatal("exact-boundary hasMore = true, want false")
	}
	if len(page) != 2 {
		t.Fatalf("exact-boundary page length = %d, want 2", len(page))
	}
}

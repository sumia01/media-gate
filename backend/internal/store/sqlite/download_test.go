package sqlite

import (
	"testing"

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

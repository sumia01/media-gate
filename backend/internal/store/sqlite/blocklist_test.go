package sqlite

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// TestBlocklistRecordAndCheck verifies the fail-count threshold semantics of the
// download blocklist: below-threshold URLs are not blocked, at/above-threshold
// URLs are, and the recorded fail count is kept as a high-water mark.
func TestBlocklistRecordAndCheck(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	const url = "https://tracker.example/torrent/abc"

	// No entry yet — not blocklisted.
	blocked, err := s.IsBlocklisted(item.ID, url, 3)
	if err != nil {
		t.Fatalf("IsBlocklisted: %v", err)
	}
	if blocked {
		t.Fatal("expected not blocklisted before any failure")
	}

	// Two observed failures — below the threshold of 3.
	if err := s.RecordBlocklistFailure(item.ID, url, "Show.S01E01.1080p", "dead url", 2); err != nil {
		t.Fatalf("RecordBlocklistFailure(2): %v", err)
	}
	blocked, err = s.IsBlocklisted(item.ID, url, 3)
	if err != nil {
		t.Fatalf("IsBlocklisted: %v", err)
	}
	if blocked {
		t.Fatal("expected not blocklisted at fail_count=2 with threshold=3")
	}

	// Third failure reaches the threshold.
	if err := s.RecordBlocklistFailure(item.ID, url, "Show.S01E01.1080p", "dead url", 3); err != nil {
		t.Fatalf("RecordBlocklistFailure(3): %v", err)
	}
	blocked, err = s.IsBlocklisted(item.ID, url, 3)
	if err != nil {
		t.Fatalf("IsBlocklisted: %v", err)
	}
	if !blocked {
		t.Fatal("expected blocklisted at fail_count=3 with threshold=3")
	}

	// A lower observed count must NOT lower the stored high-water mark
	// (idempotent / durable block even if failed download rows are pruned).
	if err := s.RecordBlocklistFailure(item.ID, url, "Show.S01E01.1080p", "dead url", 1); err != nil {
		t.Fatalf("RecordBlocklistFailure(1): %v", err)
	}
	blocked, err = s.IsBlocklisted(item.ID, url, 3)
	if err != nil {
		t.Fatalf("IsBlocklisted: %v", err)
	}
	if !blocked {
		t.Fatal("expected still blocklisted after a lower observed count (high-water mark)")
	}

	// A different URL for the same item is independent.
	blocked, err = s.IsBlocklisted(item.ID, "https://other/x", 3)
	if err != nil {
		t.Fatalf("IsBlocklisted(other): %v", err)
	}
	if blocked {
		t.Fatal("unrelated URL must not be blocklisted")
	}
}

// TestBlocklistCascadeOnMediaItemDelete verifies the FK cascade: deleting the
// media item removes its blocklist entries (no manual cleanup, FK does it).
func TestBlocklistCascadeOnMediaItemDelete(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	const url = "https://tracker.example/torrent/xyz"

	if err := s.RecordBlocklistFailure(item.ID, url, "t", "err", 5); err != nil {
		t.Fatalf("RecordBlocklistFailure: %v", err)
	}
	if err := s.DeleteMediaItem(item.ID); err != nil {
		t.Fatalf("DeleteMediaItem: %v", err)
	}

	var count int64
	if err := s.db.Model(&store.DownloadBlocklist{}).
		Where("media_item_id = ?", item.ID).Count(&count).Error; err != nil {
		t.Fatalf("count blocklist: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected blocklist entries removed by FK cascade, got %d", count)
	}
}

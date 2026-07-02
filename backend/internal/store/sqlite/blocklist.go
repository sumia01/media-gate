package sqlite

import (
	"time"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IsBlocklisted reports whether a blocklist entry exists for the given
// (mediaItemID, downloadURL) with a fail count at or above threshold.
func (s *SQLiteStore) IsBlocklisted(mediaItemID uint, downloadURL string, threshold int) (bool, error) {
	var count int64
	err := s.db.Model(&store.DownloadBlocklist{}).
		Where("media_item_id = ? AND download_url = ? AND fail_count >= ?",
			mediaItemID, downloadURL, threshold,
		).
		Count(&count).Error
	return count > 0, err
}

// RecordBlocklistFailure upserts a blocklist entry for (mediaItemID, downloadURL).
// failCount is the number of failed download rows observed for the release; the
// stored fail_count is kept as a high-water mark (MAX of existing and failCount)
// so a persistent block is not lost if failed download rows are later removed.
func (s *SQLiteStore) RecordBlocklistFailure(mediaItemID uint, downloadURL, title, lastError string, failCount int) error {
	if failCount < 1 {
		failCount = 1
	}
	now := time.Now()
	entry := store.DownloadBlocklist{
		MediaItemID:  mediaItemID,
		DownloadURL:  downloadURL,
		Title:        title,
		FailCount:    failCount,
		LastError:    lastError,
		LastFailedAt: now,
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "media_item_id"}, {Name: "download_url"}},
		DoUpdates: clause.Assignments(map[string]any{
			"fail_count":     gorm.Expr("MAX(download_blocklists.fail_count, excluded.fail_count)"),
			"title":          title,
			"last_error":     lastError,
			"last_failed_at": now,
			"updated_at":     now,
		}),
	}).Create(&entry).Error
}

package sqlite

import (
	"errors"
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

func (s *SQLiteStore) AppendMediaActivity(activity *store.MediaActivity) error {
	if err := store.ValidateMediaActivity(activity); err != nil {
		return err
	}
	if _, err := s.GetMediaItem(activity.MediaItemID); err != nil {
		return err
	}
	if activity.ActorKind == store.MediaActivityActorUser {
		if _, err := s.GetUser(*activity.ActorUserID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return store.ErrActivityActorNotFound
			}
			return err
		}
	}
	activity.ID = 0
	activity.RecordedAt = time.Now().UTC()
	return s.db.Table("media_activity").Create(activity).Error
}

func (s *SQLiteStore) ListMediaActivityPage(mediaItemID, viewerUserID uint, beforeID *uint, limit int) ([]store.MediaActivityAttribution, bool, error) {
	if limit <= 0 {
		return nil, false, store.ErrInvalidMediaActivity
	}
	query := s.db.Table("media_activity").
		Select("media_activity.*, users.first_name, users.last_name, users.email").
		Joins("LEFT JOIN users ON users.id = media_activity.actor_user_id").
		Where("media_activity.media_item_id = ?", mediaItemID).
		Where("media_activity.visibility = ? OR (media_activity.visibility = ? AND media_activity.actor_user_id = ?)",
			store.MediaActivityVisibilityShared, store.MediaActivityVisibilityActorOnly, viewerUserID)
	if beforeID != nil {
		query = query.Where("media_activity.id < ?", *beforeID)
	}
	var rows []store.MediaActivityAttribution
	if err := query.Order("media_activity.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}

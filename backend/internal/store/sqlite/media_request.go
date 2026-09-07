package sqlite

import (
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

func (s *SQLiteStore) CreateMediaRequest(request *store.MediaRequest) error {
	if request.UserID == nil {
		return store.ErrRequesterNotFound
	}
	if request.RequestedAt.IsZero() {
		request.RequestedAt = time.Now()
	}
	result := s.db.Exec(`
		INSERT INTO media_requests (media_item_id, user_id, scope, season_number, episode_number, requested_at)
		SELECT ?, ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM users WHERE id = ?)
		ON CONFLICT DO NOTHING`,
		request.MediaItemID,
		*request.UserID,
		request.Scope,
		request.SeasonNumber,
		request.EpisodeNumber,
		request.RequestedAt,
		*request.UserID,
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := s.db.Model(&store.User{}).Where("id = ?", *request.UserID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return store.ErrRequesterNotFound
		}
	}
	return nil
}

func (s *SQLiteStore) ListMediaRequestsByMediaItem(mediaItemID uint) ([]store.MediaRequestAttribution, error) {
	var requests []store.MediaRequestAttribution
	err := s.db.Table("media_requests").
		Select("media_requests.*, users.first_name, users.last_name, users.email").
		Joins("LEFT JOIN users ON users.id = media_requests.user_id").
		Where("media_requests.media_item_id = ?", mediaItemID).
		Order("media_requests.requested_at ASC, media_requests.id ASC").
		Scan(&requests).Error
	return requests, err
}

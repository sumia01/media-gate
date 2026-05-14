package sqlite

import (
	"errors"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
)

func (s *SQLiteStore) CreateEpisode(episode *store.Episode) error {
	return s.db.Create(episode).Error
}

func (s *SQLiteStore) ListEpisodesByMediaItem(mediaItemID uint) ([]store.Episode, error) {
	var episodes []store.Episode
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("season_number ASC, episode_number ASC").Find(&episodes).Error; err != nil {
		return nil, err
	}
	return episodes, nil
}

func (s *SQLiteStore) GetEpisodeByNumber(mediaItemID uint, seasonNumber, episodeNumber int) (*store.Episode, error) {
	var ep store.Episode
	if err := s.db.Where("media_item_id = ? AND season_number = ? AND episode_number = ?", mediaItemID, seasonNumber, episodeNumber).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &ep, nil
}

func (s *SQLiteStore) DeleteEpisodesByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&store.Episode{}).Error
}

package sqlite

import "github.com/sumia01/media-gate/internal/store"

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

func (s *SQLiteStore) DeleteEpisodesByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&store.Episode{}).Error
}

package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) ListEpisodeMonitorsByMediaItem(mediaItemID uint) ([]store.EpisodeMonitor, error) {
	var monitors []store.EpisodeMonitor
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("season_number ASC, episode_number ASC").Find(&monitors).Error; err != nil {
		return nil, err
	}
	return monitors, nil
}

func (s *SQLiteStore) UpsertEpisodeMonitor(monitor *store.EpisodeMonitor) error {
	var existing store.EpisodeMonitor
	err := s.db.Where("media_item_id = ? AND season_number = ? AND episode_number = ?",
		monitor.MediaItemID, monitor.SeasonNumber, monitor.EpisodeNumber).First(&existing).Error
	if err == nil {
		existing.Monitored = monitor.Monitored
		return s.db.Save(&existing).Error
	}
	return s.db.Create(monitor).Error
}

func (s *SQLiteStore) DeleteEpisodeMonitorsBySeason(mediaItemID uint, seasonNumber int) error {
	return s.db.Where("media_item_id = ? AND season_number = ?", mediaItemID, seasonNumber).Delete(&store.EpisodeMonitor{}).Error
}

func (s *SQLiteStore) DeleteEpisodeMonitorsByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&store.EpisodeMonitor{}).Error
}

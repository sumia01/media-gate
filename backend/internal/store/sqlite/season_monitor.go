package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateSeasonMonitor(monitor *store.SeasonMonitor) error {
	return s.db.Select("MediaItemID", "SeasonNumber", "Monitored").Create(monitor).Error
}

func (s *SQLiteStore) ListSeasonMonitorsByMediaItem(mediaItemID uint) ([]store.SeasonMonitor, error) {
	var monitors []store.SeasonMonitor
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("season_number ASC").Find(&monitors).Error; err != nil {
		return nil, err
	}
	return monitors, nil
}

func (s *SQLiteStore) UpdateSeasonMonitor(monitor *store.SeasonMonitor) error {
	return save(s.db, monitor)
}

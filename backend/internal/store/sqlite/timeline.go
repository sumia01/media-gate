package sqlite

import "github.com/sumia01/media-gate/internal/store"

// ListTimelineEpisodes returns every dated episode in [from, to), including
// available and individually unmonitored episodes of followed series.
func (s *SQLiteStore) ListTimelineEpisodes(from, to string) ([]store.TimelineEpisode, error) {
	items := make([]store.TimelineEpisode, 0)
	err := s.db.Table("episodes AS e").
		Select(`e.*, COALESCE(NULLIF(md.title, ''), m.title) AS series_title,
			COALESCE(md.poster_path, '') AS poster_path,
			COALESCE(em.monitored, sm.monitored, 0) AS monitored,
			EXISTS (SELECT 1 FROM media_files f WHERE f.media_item_id = e.media_item_id
				AND f.season_number = e.season_number AND f.episode_number = e.episode_number) AS has_file`).
		Joins("JOIN media_items m ON m.id = e.media_item_id").
		Joins("LEFT JOIN media_metadata md ON md.media_item_id = m.id").
		Joins("LEFT JOIN season_monitors sm ON sm.media_item_id = m.id AND sm.season_number = e.season_number").
		Joins(`LEFT JOIN episode_monitors em ON em.media_item_id = m.id
			AND em.season_number = e.season_number AND em.episode_number = e.episode_number`).
		Where("m.media_type = ? AND m.monitored = ? AND e.air_date >= ? AND e.air_date < ?", "series", true, from, to).
		Order("e.air_date, e.media_item_id, e.season_number, e.episode_number").
		Scan(&items).Error
	return items, err
}

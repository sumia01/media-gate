package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateSubtitle(subtitle *store.Subtitle) error {
	return s.db.Create(subtitle).Error
}

func (s *SQLiteStore) GetSubtitle(id uint) (*store.Subtitle, error) {
	return getByID[store.Subtitle](s.db, id)
}

func (s *SQLiteStore) ListSubtitlesByMediaItem(mediaItemID uint) ([]store.Subtitle, error) {
	var subtitles []store.Subtitle
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("created_at DESC").Find(&subtitles).Error; err != nil {
		return nil, err
	}
	return subtitles, nil
}

func (s *SQLiteStore) DeleteSubtitle(id uint) error {
	return deleteByID(s.db, &store.Subtitle{}, id)
}

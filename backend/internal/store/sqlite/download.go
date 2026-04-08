package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateDownload(download *store.Download) error {
	return s.db.Create(download).Error
}

func (s *SQLiteStore) GetDownload(id uint) (*store.Download, error) {
	return getByID[store.Download](s.db, id)
}

func (s *SQLiteStore) UpdateDownload(download *store.Download) error {
	return save(s.db, download)
}

func (s *SQLiteStore) ListDownloads(mediaItemID *uint, status *string) ([]store.Download, error) {
	var downloads []store.Download
	q := s.db.Order("created_at DESC")
	if mediaItemID != nil {
		q = q.Where("media_item_id = ?", *mediaItemID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if err := q.Find(&downloads).Error; err != nil {
		return nil, err
	}
	return downloads, nil
}

func (s *SQLiteStore) DeleteDownload(id uint) error {
	return deleteByID(s.db, &store.Download{}, id)
}

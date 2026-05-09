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
	q := s.db.
		Select("downloads.*, media_items.title AS media_item_title").
		Joins("LEFT JOIN media_items ON media_items.id = downloads.media_item_id").
		Order("created_at DESC")
	if mediaItemID != nil {
		q = q.Where("downloads.media_item_id = ?", *mediaItemID)
	}
	if status != nil {
		q = q.Where("downloads.status = ?", *status)
	}
	if err := q.Find(&downloads).Error; err != nil {
		return nil, err
	}
	return downloads, nil
}

func (s *SQLiteStore) HasActiveDownloadByURL(mediaItemID uint, downloadURL string) (bool, error) {
	var count int64
	err := s.db.Model(&store.Download{}).
		Where("media_item_id = ? AND download_url = ? AND status IN ?",
			mediaItemID, downloadURL, store.ActiveDownloadStatuses,
		).
		Count(&count).Error
	return count > 0, err
}

func (s *SQLiteStore) DeleteDownload(id uint) error {
	return deleteByID(s.db, &store.Download{}, id)
}

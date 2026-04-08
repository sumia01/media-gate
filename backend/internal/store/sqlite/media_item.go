package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateMediaItem(item *store.MediaItem) error {
	return s.db.Create(item).Error
}

func (s *SQLiteStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	return getByID[store.MediaItem](s.db, id)
}

func (s *SQLiteStore) UpdateMediaItem(item *store.MediaItem) error {
	return save(s.db, item)
}

func (s *SQLiteStore) DeleteMediaItem(id uint) error {
	return deleteByID(s.db, &store.MediaItem{}, id)
}

func (s *SQLiteStore) ListMediaItemsByLibrary(libraryID uint) ([]store.MediaItem, error) {
	var items []store.MediaItem
	if err := s.db.Where("library_id = ?", libraryID).Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListDiskMediaItemsByLibrary(libraryID uint) ([]store.MediaItem, error) {
	var items []store.MediaItem
	if err := s.db.Where("library_id = ? AND source = ?", libraryID, "disk").Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListNewMediaItemsByLibrary(libraryID uint) ([]store.MediaItem, error) {
	var items []store.MediaItem
	if err := s.db.Where("library_id = ? AND status = ?", libraryID, "new").Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) CountMediaItemsByLibrary(libraryID uint) (int64, error) {
	var count int64
	if err := s.db.Model(&store.MediaItem{}).Where("library_id = ?", libraryID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStore) ListMonitoredMediaItems() ([]store.MediaItem, error) {
	var items []store.MediaItem
	if err := s.db.Where("monitored = ?", true).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListRecentMediaItems(limit int) ([]store.MediaItem, error) {
	var items []store.MediaItem
	if err := s.db.Where("source != ?", "disk").Order("created_at DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) MediaItemExistsByExternalID(libraryID uint, source string, externalID int) (bool, error) {
	var count int64
	err := s.db.Model(&store.MediaMetadata{}).
		Joins("JOIN media_items ON media_items.id = media_metadata.media_item_id").
		Where("media_items.library_id = ? AND media_metadata.source = ? AND media_metadata.external_id = ?", libraryID, source, externalID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

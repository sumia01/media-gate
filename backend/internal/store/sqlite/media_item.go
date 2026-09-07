package sqlite

import (
	"errors"
	"time"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
)

func (s *SQLiteStore) CreateMediaItem(item *store.MediaItem) error {
	return s.db.Create(item).Error
}

func (s *SQLiteStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	return getByID[store.MediaItem](s.db, id)
}

func (s *SQLiteStore) UpdateMediaItem(item *store.MediaItem) error {
	if item.ID == 0 {
		return store.ErrNotFound
	}
	next := *item
	// Only the initial false-to-true claim may update this row. Once claimed,
	// final deletion is the sole operation allowed to change the parent.
	query := s.db.Where("deletion_pending = ?", false)
	if err := save(query, &next); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		var claimed int64
		if lookupErr := s.db.Model(&store.MediaItem{}).
			Where("id = ? AND deletion_pending = ?", next.ID, true).
			Count(&claimed).Error; lookupErr != nil {
			return lookupErr
		}
		if claimed > 0 {
			return store.ErrMediaDeletionPending
		}
		return err
	}
	*item = next
	return nil
}

func (s *SQLiteStore) SetMonitorSearchStartedAt(mediaItemID uint, startedAt *time.Time) error {
	query := s.db.Model(&store.MediaItem{}).Where("id = ?", mediaItemID)
	if startedAt != nil {
		query = query.Where("monitored = ? AND deletion_pending = ? AND monitor_search_started_at IS NULL", true, false)
	}
	// Marker bookkeeping is not a change to the monitor's input version.
	return query.UpdateColumn("monitor_search_started_at", startedAt).Error
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
	if err := s.db.Where("monitored = ? AND deletion_pending = ?", true, false).Find(&items).Error; err != nil {
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

func (s *SQLiteStore) GetMediaItemByExternalID(libraryID uint, source string, externalID int) (*store.MediaItem, error) {
	var item store.MediaItem
	err := s.db.
		Joins("JOIN media_metadata ON media_metadata.media_item_id = media_items.id").
		Where("media_items.library_id = ? AND media_metadata.source = ? AND media_metadata.external_id = ?", libraryID, source, externalID).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

package sqlite

import (
	"errors"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
)

func (s *SQLiteStore) CreateWatchedItem(item *store.WatchedItem) error {
	return s.db.Create(item).Error
}

func (s *SQLiteStore) DeleteWatchedItem(id uint) error {
	return deleteByID(s.db, &store.WatchedItem{}, id)
}

func (s *SQLiteStore) ListWatchedItems() ([]store.WatchedItem, error) {
	var items []store.WatchedItem
	if err := s.db.Order("watched_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListWatchedItemsByUser(userID uint) ([]store.WatchedItem, error) {
	var items []store.WatchedItem
	if err := s.db.Where("user_id = ?", userID).Order("watched_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) GetWatchedBySourceExternal(userID *uint, source string, externalID int) (*store.WatchedItem, error) {
	var item store.WatchedItem
	q := s.db.Where("source = ? AND external_id = ?", source, externalID)
	if userID != nil {
		q = q.Where("user_id = ?", *userID)
	}
	if err := q.First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) ClearWatchedMediaItemID(mediaItemID uint) error {
	return s.db.Model(&store.WatchedItem{}).Where("media_item_id = ?", mediaItemID).Update("media_item_id", nil).Error
}

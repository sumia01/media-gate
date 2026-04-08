package sqlite

import (
	"errors"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
)

func (s *SQLiteStore) CreateMediaMetadata(meta *store.MediaMetadata) error {
	return s.db.Create(meta).Error
}

func (s *SQLiteStore) GetMediaMetadataByMediaItem(mediaItemID uint) (*store.MediaMetadata, error) {
	var meta store.MediaMetadata
	if err := s.db.Where("media_item_id = ?", mediaItemID).First(&meta).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &meta, nil
}

func (s *SQLiteStore) UpdateMediaMetadata(meta *store.MediaMetadata) error {
	return save(s.db, meta)
}

func (s *SQLiteStore) DeleteMediaMetadataByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&store.MediaMetadata{}).Error
}

func (s *SQLiteStore) ListMediaMetadataByMediaItemIDs(ids []uint) ([]store.MediaMetadata, error) {
	if len(ids) == 0 {
		return []store.MediaMetadata{}, nil
	}
	var metas []store.MediaMetadata
	if err := s.db.Where("media_item_id IN ?", ids).Find(&metas).Error; err != nil {
		return nil, err
	}
	return metas, nil
}

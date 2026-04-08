package sqlite

import (
	"errors"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
)

func (s *SQLiteStore) GetSetting(key string) (*store.Setting, error) {
	var setting store.Setting
	if err := s.db.First(&setting, "key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &setting, nil
}

func (s *SQLiteStore) SetSetting(setting *store.Setting) error {
	return s.db.Save(setting).Error
}

func (s *SQLiteStore) ListSettings() ([]store.Setting, error) {
	var settings []store.Setting
	if err := s.db.Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *SQLiteStore) DeleteSetting(key string) error {
	result := s.db.Delete(&store.Setting{}, "key = ?", key)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteSettingsByPrefix(prefix string) error {
	return s.db.Where("key LIKE ?", prefix+"%").Delete(&store.Setting{}).Error
}

func (s *SQLiteStore) ListSettingsByPrefix(prefix string) ([]store.Setting, error) {
	var settings []store.Setting
	if err := s.db.Where("key LIKE ?", prefix+"%").Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

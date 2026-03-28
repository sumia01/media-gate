package store

import (
	"errors"
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ Store = (*SQLiteStore)(nil)

type SQLiteStore struct {
	db *gorm.DB
}

func NewSQLite(dbPath string) (*SQLiteStore, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	if err := db.AutoMigrate(&Library{}, &MediaItem{}, &Setting{}); err != nil {
		return nil, fmt.Errorf("auto-migrating database: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Ping() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func (s *SQLiteStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *SQLiteStore) CreateLibrary(lib *Library) error {
	return s.db.Create(lib).Error
}

func (s *SQLiteStore) ListLibraries() ([]Library, error) {
	var libs []Library
	if err := s.db.Find(&libs).Error; err != nil {
		return nil, err
	}
	return libs, nil
}

func (s *SQLiteStore) GetLibrary(id uint) (*Library, error) {
	var lib Library
	if err := s.db.First(&lib, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &lib, nil
}

func (s *SQLiteStore) UpdateLibrary(lib *Library) error {
	result := s.db.Save(lib)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteLibrary(id uint) error {
	result := s.db.Delete(&Library{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CreateMediaItem(item *MediaItem) error {
	return s.db.Create(item).Error
}

func (s *SQLiteStore) ListMediaItemsByLibrary(libraryID uint) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("library_id = ?", libraryID).Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) DeleteMediaItemsByLibrary(libraryID uint) error {
	return s.db.Where("library_id = ?", libraryID).Delete(&MediaItem{}).Error
}

func (s *SQLiteStore) DeleteMediaItemsByPaths(libraryID uint, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return s.db.Where("library_id = ? AND path IN ?", libraryID, paths).Delete(&MediaItem{}).Error
}

func (s *SQLiteStore) CountMediaItemsByLibrary(libraryID uint) (int64, error) {
	var count int64
	if err := s.db.Model(&MediaItem{}).Where("library_id = ?", libraryID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStore) GetSetting(key string) (*Setting, error) {
	var setting Setting
	if err := s.db.First(&setting, "key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &setting, nil
}

func (s *SQLiteStore) SetSetting(setting *Setting) error {
	return s.db.Save(setting).Error
}

func (s *SQLiteStore) ListSettings() ([]Setting, error) {
	var settings []Setting
	if err := s.db.Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *SQLiteStore) DeleteSetting(key string) error {
	result := s.db.Delete(&Setting{}, "key = ?", key)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

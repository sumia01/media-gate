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

	if err := db.AutoMigrate(&Library{}); err != nil {
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

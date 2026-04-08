package sqlite

import (
	"errors"
	"fmt"

	"github.com/glebarez/sqlite"
	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ store.Store = (*SQLiteStore)(nil)

type SQLiteStore struct {
	db *gorm.DB
}

func New(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_foreign_keys=ON"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	sqlDB, _ := db.DB()

	// Pre-migration renames for existing databases (ignore errors for fresh installs)
	if sqlDB != nil {
		sqlDB.Exec("ALTER TABLE quality_profiles RENAME TO media_profiles")
		sqlDB.Exec("ALTER TABLE media_profiles ADD COLUMN languages TEXT DEFAULT ''")
		sqlDB.Exec("ALTER TABLE libraries RENAME COLUMN quality_profile_id TO media_profile_id")
		sqlDB.Exec("ALTER TABLE media_items RENAME COLUMN quality_profile_id TO media_profile_id")
		sqlDB.Exec("UPDATE media_items SET status = 'available' WHERE status = 'matched'")
	}

	if err := db.AutoMigrate(
		&store.Library{},
		&store.MediaItem{},
		&store.MediaMetadata{},
		&store.MediaProfile{},
		&store.MediaFile{},
		&store.SeasonMonitor{},
		&store.EpisodeMonitor{},
		&store.Episode{},
		&store.Setting{},
		&store.JobRecord{},
		&store.Indexer{},
		&store.Download{},
		&store.User{},
		&store.RefreshToken{},
		&store.WatchedItem{},
	); err != nil {
		return nil, fmt.Errorf("auto-migrating database: %w", err)
	}

	// Run versioned schema migrations (FK rebuilds, etc.).
	if sqlDB != nil {
		runMigrations(sqlDB)
		cleanupOrphans(sqlDB)
	}

	// Re-enable foreign keys — AutoMigrate and rebuildTable both disable
	// this pragma during table alterations, and the deferred restore may
	// land on a different pooled connection than the one GORM uses.
	db.Exec("PRAGMA foreign_keys = ON")

	return &SQLiteStore{db: db}, nil
}

// --- Generic CRUD helpers ---

func getByID[T any](db *gorm.DB, id uint) (*T, error) {
	var result T
	if err := db.First(&result, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &result, nil
}

func save(db *gorm.DB, model any) error {
	result := db.Save(model)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func deleteByID(db *gorm.DB, model any, id uint) error {
	result := db.Delete(model, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
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

func (s *SQLiteStore) WithTx(fn func(store.Store) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		return fn(&SQLiteStore{db: tx})
	})
}

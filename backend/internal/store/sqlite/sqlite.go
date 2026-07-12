package sqlite

import (
	"errors"
	"fmt"

	"github.com/glebarez/sqlite"
	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

var _ store.Store = (*SQLiteStore)(nil)

type SQLiteStore struct {
	db *gorm.DB
}

func New(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	if err := db.Use(tracing.NewPlugin(tracing.WithoutMetrics())); err != nil {
		return nil, fmt.Errorf("registering gorm opentelemetry plugin: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("obtaining sql.DB handle: %w", err)
	}

	// Idempotent pre-baseline fixups for very old databases (no-ops on fresh /
	// already-migrated installs). Must run before schema migrations.
	applyLegacyRenames(sqlDB)

	// Schema is managed entirely by golang-migrate (embedded SQL under
	// migrations/). No GORM AutoMigrate — AutoMigrate's per-boot table rebuilds
	// were what silently dropped ALTER-added columns. See migrator.go.
	if err := runSchemaMigrations(sqlDB); err != nil {
		return nil, fmt.Errorf("running schema migrations: %w", err)
	}

	// Safety net: prune orphaned rows. Also the cascade mechanism for
	// episode_monitors and subtitles, which (matching the historical schema)
	// carry no foreign key.
	cleanupOrphans(sqlDB)

	// Re-enable foreign keys — migrations may toggle this pragma, and the
	// restore may land on a different pooled connection than the one GORM uses.
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

// save performs a full-column UPDATE of an existing row identified by its
// primary key. It intentionally does NOT use gorm's Save: Save re-INSERTs
// (upserts via OnConflict{UpdateAll}) when the UPDATE affects zero rows, which
// would silently resurrect a concurrently-deleted row. Providing an explicit
// Select("*") both (a) forces zero-value fields to be written — preserving the
// full-write semantics of Save so intentional field-clearing keeps working —
// and (b) disables the Save-only INSERT fallback. A zero RowsAffected therefore
// unambiguously means the row no longer exists.
func save(db *gorm.DB, model any) error {
	result := db.Model(model).Select("*").Updates(model)
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

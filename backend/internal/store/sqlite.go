package store

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ Store = (*SQLiteStore)(nil)

type SQLiteStore struct {
	db *gorm.DB
}

func NewSQLite(dbPath string) (*SQLiteStore, error) {
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
		&Library{},
		&MediaItem{},
		&MediaMetadata{},
		&MediaProfile{},
		&MediaFile{},
		&SeasonMonitor{},
		&EpisodeMonitor{},
		&Episode{},
		&Setting{},
		&JobRecord{},
		&Indexer{},
		&Download{},
		&User{},
		&RefreshToken{},
		&WatchedItem{},
	); err != nil {
		return nil, fmt.Errorf("auto-migrating database: %w", err)
	}

	// Run versioned schema migrations (FK rebuilds, orphan cleanup, etc.).
	if sqlDB != nil {
		runMigrations(sqlDB)
	}

	// Re-enable foreign keys — AutoMigrate and rebuildTable both disable
	// this pragma during table alterations, and the deferred restore may
	// land on a different pooled connection than the one GORM uses.
	db.Exec("PRAGMA foreign_keys = ON")

	return &SQLiteStore{db: db}, nil
}

// --- Versioned schema migrations ---

// migrations is the ordered list of schema migration functions.
// Each function migrates from version N to N+1 (index 0 = version 0→1).
// New migrations are appended to the end — never reorder or remove entries.
var migrations = []func(*sql.DB) error{
	migrateV1, // 0→1: FK constraint rebuilds + orphan cleanup (existing tables)
	migrateV2, // 1→2: watched_items — add media_item_id FK with SET NULL
	migrateV3, // 2→3: explicit season monitoring — add monitor_new_seasons column + backfill SeasonMonitor rows
	migrateV4, // 3→4: episode_monitors table (AutoMigrate handles creation, this is a no-op placeholder)
}

func getSchemaVersion(db *sql.DB) int {
	var val string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&val)
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(val)
	return v
}

func setSchemaVersion(db *sql.DB, version int) {
	_, _ = db.Exec(
		`INSERT OR REPLACE INTO settings (key, value, sensitive, created_at, updated_at)
		 VALUES ('schema_version', ?, false, datetime('now'), datetime('now'))`,
		strconv.Itoa(version),
	)
}

func runMigrations(db *sql.DB) {
	current := getSchemaVersion(db)
	for i := current; i < len(migrations); i++ {
		slog.Info("running schema migration", "from", i, "to", i+1)
		if err := migrations[i](db); err != nil {
			slog.Error("schema migration failed", "version", i+1, "error", err)
			return
		}
		setSchemaVersion(db, i+1)
	}
}

// migrateV1 adds FK constraints to tables created before GORM could add them.
// For fresh installs where AutoMigrate already created FKs, the per-table
// tableHasForeignKeys check causes each rebuild to be skipped.
func migrateV1(db *sql.DB) error {
	type fkDef struct {
		table   string
		newDDL  string
		columns string
	}

	tableDefs := []fkDef{
		{
			table: "media_items",
			newDDL: `CREATE TABLE "media_items" (
				"id" integer PRIMARY KEY AUTOINCREMENT,
				"library_id" integer NOT NULL REFERENCES "libraries"("id") ON DELETE CASCADE,
				"title" text NOT NULL,
				"media_type" text NOT NULL,
				"status" text NOT NULL DEFAULT "new",
				"source" text NOT NULL DEFAULT "disk",
				"year" integer,
				"media_profile_id" integer,
				"monitored" numeric NOT NULL DEFAULT false,
				"monitor_search_started_at" datetime,
				"created_at" datetime,
				"updated_at" datetime
			)`,
			columns: "id,library_id,title,media_type,status,source,year,media_profile_id,monitored,monitor_search_started_at,created_at,updated_at",
		},
		{
			table: "media_metadata",
			newDDL: `CREATE TABLE "media_metadata" (
				"id" integer PRIMARY KEY AUTOINCREMENT,
				"media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
				"source" text NOT NULL,
				"external_id" integer NOT NULL,
				"imdb_id" text,
				"title" text NOT NULL,
				"overview" text,
				"poster_path" text,
				"genres" text,
				"credits" text,
				"year" integer,
				"rating" real,
				"status" text,
				"runtime" integer,
				"seasons" integer,
				"release_date" text,
				"confidence" real,
				"matched_at" datetime,
				"created_at" datetime,
				"updated_at" datetime
			)`,
			columns: "id,media_item_id,source,external_id,imdb_id,title,overview,poster_path,genres,credits,year,rating,status,runtime,seasons,release_date,confidence,matched_at,created_at,updated_at",
		},
		{
			table: "media_files",
			newDDL: `CREATE TABLE "media_files" (
				"id" integer PRIMARY KEY AUTOINCREMENT,
				"media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
				"path" text NOT NULL,
				"file_name" text NOT NULL,
				"size" integer,
				"resolution" text,
				"source_type" text,
				"season_number" integer,
				"episode_number" integer,
				"added_at" datetime,
				"created_at" datetime,
				"updated_at" datetime
			)`,
			columns: "id,media_item_id,path,file_name,size,resolution,source_type,season_number,episode_number,added_at,created_at,updated_at",
		},
		{
			table: "season_monitors",
			newDDL: `CREATE TABLE "season_monitors" (
				"id" integer PRIMARY KEY AUTOINCREMENT,
				"media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
				"season_number" integer NOT NULL,
				"monitored" numeric NOT NULL,
				"created_at" datetime,
				"updated_at" datetime
			)`,
			columns: "id,media_item_id,season_number,monitored,created_at,updated_at",
		},
		{
			table: "episodes",
			newDDL: `CREATE TABLE "episodes" (
				"id" integer PRIMARY KEY AUTOINCREMENT,
				"media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
				"season_number" integer NOT NULL,
				"episode_number" integer NOT NULL,
				"title" text,
				"overview" text,
				"air_date" text,
				"runtime" integer,
				"created_at" datetime,
				"updated_at" datetime
			)`,
			columns: "id,media_item_id,season_number,episode_number,title,overview,air_date,runtime,created_at,updated_at",
		},
		{
			table: "downloads",
			newDDL: `CREATE TABLE "downloads" (
				"id" integer PRIMARY KEY AUTOINCREMENT,
				"media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
				"episode_id" integer REFERENCES "episodes"("id") ON DELETE SET NULL,
				"season_number" integer,
				"indexer_id" integer NOT NULL,
				"indexer_name" text NOT NULL,
				"title" text NOT NULL,
				"download_url" text NOT NULL,
				"details_url" text,
				"size" text,
				"imdb_id" text,
				"status" text NOT NULL DEFAULT "pending",
				"client_torrent_hash" text,
				"save_path" text,
				"seeding_required" numeric NOT NULL DEFAULT false,
				"linked_to_library" numeric NOT NULL DEFAULT false,
				"retry_count" integer NOT NULL DEFAULT 0,
				"next_retry_at" datetime,
				"last_error" text,
				"created_at" datetime,
				"updated_at" datetime,
				"completed_at" datetime
			)`,
			columns: "id,media_item_id,episode_id,season_number,indexer_id,indexer_name,title,download_url,details_url,size,imdb_id,status,client_torrent_hash,save_path,seeding_required,linked_to_library,retry_count,next_retry_at,last_error,created_at,updated_at,completed_at",
		},
		{
			table: "refresh_tokens",
			newDDL: `CREATE TABLE "refresh_tokens" (
				"id" integer PRIMARY KEY AUTOINCREMENT,
				"user_id" integer NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
				"token" text NOT NULL,
				"expires_at" datetime NOT NULL,
				"created_at" datetime
			)`,
			columns: "id,user_id,token,expires_at,created_at",
		},
	}

	for _, m := range tableDefs {
		if tableHasForeignKeys(db, m.table) {
			continue
		}
		if err := rebuildTable(db, m.table, m.newDDL, m.columns); err != nil {
			return err
		}
	}

	// Recreate indexes that may have been dropped with old tables.
	indexDDLs := []string{
		`CREATE INDEX IF NOT EXISTS "idx_media_items_library_id" ON "media_items"("library_id")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_media_metadata_media_item_id" ON "media_metadata"("media_item_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_media_files_media_item_id" ON "media_files"("media_item_id")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_media_files_path" ON "media_files"("path")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_media_season" ON "season_monitors"("media_item_id","season_number")`,
		`CREATE INDEX IF NOT EXISTS "idx_episodes_media_item_id" ON "episodes"("media_item_id")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_episode_unique" ON "episodes"("media_item_id","season_number","episode_number")`,
		`CREATE INDEX IF NOT EXISTS "idx_downloads_media_item_id" ON "downloads"("media_item_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_downloads_episode_id" ON "downloads"("episode_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_refresh_tokens_user_id" ON "refresh_tokens"("user_id")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_refresh_tokens_token" ON "refresh_tokens"("token")`,
	}
	for _, ddl := range indexDDLs {
		if _, err := db.Exec(ddl); err != nil {
			slog.Warn("failed to recreate index", "ddl", ddl, "error", err)
		}
	}

	cleanupOrphans(db)
	return nil
}

// migrateV2 rebuilds watched_items to add the media_item_id FK with ON DELETE SET NULL.
// The table already has a user_id FK (from initial creation), so tableHasForeignKeys
// returns true and migrateV1 skips it. This migration targets the specific missing FK.
func migrateV2(db *sql.DB) error {
	if tableHasFKOnColumn(db, "watched_items", "media_item_id") {
		return nil // fresh install — AutoMigrate already created both FKs
	}

	newDDL := `CREATE TABLE "watched_items" (
		"id" integer PRIMARY KEY AUTOINCREMENT,
		"user_id" integer NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
		"source" text NOT NULL,
		"external_id" integer NOT NULL,
		"imdb_id" text,
		"title" text NOT NULL,
		"media_type" text NOT NULL,
		"year" integer,
		"poster_path" text,
		"media_item_id" integer REFERENCES "media_items"("id") ON DELETE SET NULL,
		"watched_at" datetime,
		"created_at" datetime,
		"updated_at" datetime
	)`
	columns := "id,user_id,source,external_id,imdb_id,title,media_type,year,poster_path,media_item_id,watched_at,created_at,updated_at"

	if err := rebuildTable(db, "watched_items", newDDL, columns); err != nil {
		return err
	}

	indexDDLs := []string{
		`CREATE INDEX IF NOT EXISTS "idx_watched_items_user_id" ON "watched_items"("user_id")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_watched_user_source_ext" ON "watched_items"("user_id","source","external_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_watched_items_media_item_id" ON "watched_items"("media_item_id")`,
	}
	for _, ddl := range indexDDLs {
		if _, err := db.Exec(ddl); err != nil {
			slog.Warn("failed to recreate index", "ddl", ddl, "error", err)
		}
	}
	return nil
}

// migrateV3 adds the monitor_new_seasons column to media_items and backfills
// explicit SeasonMonitor rows for all monitored series. After this migration,
// "no SeasonMonitor row" means "not monitored" (previously it meant monitored).
func migrateV3(db *sql.DB) error {
	// 1. Add column if not already present (AutoMigrate may have added it on fresh installs).
	var hasColumn bool
	rows, err := db.Query("PRAGMA table_info('media_items')")
	if err != nil {
		return fmt.Errorf("checking media_items columns: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == "monitor_new_seasons" {
			hasColumn = true
			break
		}
	}
	rows.Close()

	if !hasColumn {
		if _, err := db.Exec("ALTER TABLE media_items ADD COLUMN monitor_new_seasons BOOLEAN NOT NULL DEFAULT 1"); err != nil {
			return fmt.Errorf("adding monitor_new_seasons column: %w", err)
		}
	}

	// 2. Backfill SeasonMonitor rows for all monitored series.
	// For each monitored series, create SeasonMonitor(monitored=true) for every
	// season that has episodes but no existing SeasonMonitor row.
	itemRows, err := db.Query("SELECT id FROM media_items WHERE monitored = 1 AND media_type = 'series'")
	if err != nil {
		return fmt.Errorf("querying monitored series: %w", err)
	}
	var itemIDs []int
	for itemRows.Next() {
		var id int
		if err := itemRows.Scan(&id); err != nil {
			continue
		}
		itemIDs = append(itemIDs, id)
	}
	itemRows.Close()

	for _, itemID := range itemIDs {
		// Find seasons that have episodes but no SeasonMonitor row.
		_, err := db.Exec(`
			INSERT INTO season_monitors (media_item_id, season_number, monitored, created_at, updated_at)
			SELECT ?, e.season_number, 1, datetime('now'), datetime('now')
			FROM (SELECT DISTINCT season_number FROM episodes WHERE media_item_id = ?) e
			WHERE NOT EXISTS (
				SELECT 1 FROM season_monitors
				WHERE media_item_id = ? AND season_number = e.season_number
			)
		`, itemID, itemID, itemID)
		if err != nil {
			slog.Warn("migrateV3: failed to backfill season monitors", "item_id", itemID, "error", err)
		}
	}

	return nil
}

// migrateV4 is a no-op placeholder — AutoMigrate already creates the episode_monitors
// table from the EpisodeMonitor model. The migration entry bumps schema_version so
// future migrations can rely on the table existing.
func migrateV4(_ *sql.DB) error {
	return nil
}

// rebuildTable recreates a table using SQLite's 12-step ALTER TABLE procedure.
// This is the only way to add FK constraints to an existing SQLite table.
func rebuildTable(db *sql.DB, table, newDDL, columns string) error {
	slog.Info("rebuilding table", "table", table)

	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign_keys for %s: %w", table, err)
	}
	defer db.Exec("PRAGMA foreign_keys = ON")

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", table, err)
	}

	tmpTable := table + "_migration_new"
	tmpDDL := strings.Replace(newDDL, fmt.Sprintf(`CREATE TABLE "%s"`, table), fmt.Sprintf(`CREATE TABLE "%s"`, tmpTable), 1)

	stmts := []string{
		tmpDDL,
		fmt.Sprintf(`INSERT INTO "%s" (%s) SELECT %s FROM "%s"`, tmpTable, columns, columns, table),
		fmt.Sprintf(`DROP TABLE "%s"`, table),
		fmt.Sprintf(`ALTER TABLE "%s" RENAME TO "%s"`, tmpTable, table),
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return fmt.Errorf("rebuild %s: %w", table, err)
		}
	}

	return tx.Commit()
}

// tableHasForeignKeys checks if a table has any foreign key constraints defined.
func tableHasForeignKeys(db *sql.DB, table string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list('%s')", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

// tableHasFKOnColumn checks if a table has a foreign key constraint on a specific column.
func tableHasFKOnColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list('%s')", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var id, seq int
		var refTable, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			continue
		}
		if from == column {
			return true
		}
	}
	return false
}

// cleanupOrphans removes child records whose parent no longer exists.
// This catches leftovers from before FK constraints were enforced.
func cleanupOrphans(db *sql.DB) {
	orphanQueries := []struct {
		label string
		query string
	}{
		{"media_metadata", `DELETE FROM media_metadata WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"media_files", `DELETE FROM media_files WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"season_monitors", `DELETE FROM season_monitors WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"episode_monitors", `DELETE FROM episode_monitors WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"episodes", `DELETE FROM episodes WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"downloads", `DELETE FROM downloads WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"downloads.episode_id", `UPDATE downloads SET episode_id = NULL WHERE episode_id IS NOT NULL AND episode_id NOT IN (SELECT id FROM episodes)`},
		{"media_items", `DELETE FROM media_items WHERE library_id NOT IN (SELECT id FROM libraries)`},
		{"refresh_tokens", `DELETE FROM refresh_tokens WHERE user_id NOT IN (SELECT id FROM users)`},
	}
	for _, oq := range orphanQueries {
		res, err := db.Exec(oq.query)
		if err != nil {
			slog.Warn("orphan cleanup failed", "table", oq.label, "error", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			slog.Info("cleaned up orphan records", "table", oq.label, "deleted", n)
		}
	}
}

// --- CRUD helpers ---

func getByID[T any](db *gorm.DB, id uint) (*T, error) {
	var result T
	if err := db.First(&result, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
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
		return ErrNotFound
	}
	return nil
}

func deleteByID(db *gorm.DB, model any, id uint) error {
	result := db.Delete(model, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
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

// --- Library ---

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
	return getByID[Library](s.db, id)
}

func (s *SQLiteStore) UpdateLibrary(lib *Library) error {
	return save(s.db, lib)
}

func (s *SQLiteStore) DeleteLibrary(id uint) error {
	return deleteByID(s.db, &Library{}, id)
}

// --- MediaItem ---

func (s *SQLiteStore) CreateMediaItem(item *MediaItem) error {
	return s.db.Create(item).Error
}

func (s *SQLiteStore) GetMediaItem(id uint) (*MediaItem, error) {
	return getByID[MediaItem](s.db, id)
}

func (s *SQLiteStore) UpdateMediaItem(item *MediaItem) error {
	return save(s.db, item)
}

func (s *SQLiteStore) DeleteMediaItem(id uint) error {
	return deleteByID(s.db, &MediaItem{}, id)
}

func (s *SQLiteStore) ListMediaItemsByLibrary(libraryID uint) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("library_id = ?", libraryID).Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListDiskMediaItemsByLibrary(libraryID uint) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("library_id = ? AND source = ?", libraryID, "disk").Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListNewMediaItemsByLibrary(libraryID uint) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("library_id = ? AND status = ?", libraryID, "new").Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) CountMediaItemsByLibrary(libraryID uint) (int64, error) {
	var count int64
	if err := s.db.Model(&MediaItem{}).Where("library_id = ?", libraryID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStore) ListMonitoredMediaItems() ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("monitored = ?", true).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListRecentMediaItems(limit int) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("source != ?", "disk").Order("created_at DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) MediaItemExistsByExternalID(libraryID uint, source string, externalID int) (bool, error) {
	var count int64
	err := s.db.Model(&MediaMetadata{}).
		Joins("JOIN media_items ON media_items.id = media_metadata.media_item_id").
		Where("media_items.library_id = ? AND media_metadata.source = ? AND media_metadata.external_id = ?", libraryID, source, externalID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// --- MediaMetadata ---

func (s *SQLiteStore) CreateMediaMetadata(meta *MediaMetadata) error {
	return s.db.Create(meta).Error
}

func (s *SQLiteStore) GetMediaMetadataByMediaItem(mediaItemID uint) (*MediaMetadata, error) {
	var meta MediaMetadata
	if err := s.db.Where("media_item_id = ?", mediaItemID).First(&meta).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &meta, nil
}

func (s *SQLiteStore) UpdateMediaMetadata(meta *MediaMetadata) error {
	return save(s.db, meta)
}

func (s *SQLiteStore) DeleteMediaMetadataByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&MediaMetadata{}).Error
}

func (s *SQLiteStore) ListMediaMetadataByMediaItemIDs(ids []uint) ([]MediaMetadata, error) {
	if len(ids) == 0 {
		return []MediaMetadata{}, nil
	}
	var metas []MediaMetadata
	if err := s.db.Where("media_item_id IN ?", ids).Find(&metas).Error; err != nil {
		return nil, err
	}
	return metas, nil
}

// --- MediaProfile ---

func (s *SQLiteStore) CreateMediaProfile(profile *MediaProfile) error {
	return s.db.Create(profile).Error
}

func (s *SQLiteStore) GetMediaProfile(id uint) (*MediaProfile, error) {
	return getByID[MediaProfile](s.db, id)
}

func (s *SQLiteStore) ListMediaProfiles() ([]MediaProfile, error) {
	var profiles []MediaProfile
	if err := s.db.Order("name ASC").Find(&profiles).Error; err != nil {
		return nil, err
	}
	return profiles, nil
}

func (s *SQLiteStore) UpdateMediaProfile(profile *MediaProfile) error {
	return save(s.db, profile)
}

func (s *SQLiteStore) DeleteMediaProfile(id uint) error {
	return deleteByID(s.db, &MediaProfile{}, id)
}

// --- MediaFile ---

func (s *SQLiteStore) CreateMediaFile(file *MediaFile) error {
	return s.db.Create(file).Error
}

func (s *SQLiteStore) GetMediaFile(id uint) (*MediaFile, error) {
	return getByID[MediaFile](s.db, id)
}

func (s *SQLiteStore) UpdateMediaFile(file *MediaFile) error {
	return save(s.db, file)
}

func (s *SQLiteStore) ListMediaFilesByMediaItem(mediaItemID uint) ([]MediaFile, error) {
	var files []MediaFile
	if err := s.db.Where("media_item_id = ?", mediaItemID).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (s *SQLiteStore) ListMediaFilesByLibrary(libraryID uint) ([]MediaFile, error) {
	var files []MediaFile
	if err := s.db.
		Joins("JOIN media_items ON media_items.id = media_files.media_item_id").
		Where("media_items.library_id = ?", libraryID).
		Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (s *SQLiteStore) DeleteMediaFile(id uint) error {
	return deleteByID(s.db, &MediaFile{}, id)
}

func (s *SQLiteStore) DeleteMediaFilesByPaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return s.db.Where("path IN ?", paths).Delete(&MediaFile{}).Error
}

// --- SeasonMonitor ---

func (s *SQLiteStore) CreateSeasonMonitor(monitor *SeasonMonitor) error {
	return s.db.Select("MediaItemID", "SeasonNumber", "Monitored").Create(monitor).Error
}

func (s *SQLiteStore) ListSeasonMonitorsByMediaItem(mediaItemID uint) ([]SeasonMonitor, error) {
	var monitors []SeasonMonitor
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("season_number ASC").Find(&monitors).Error; err != nil {
		return nil, err
	}
	return monitors, nil
}

func (s *SQLiteStore) UpdateSeasonMonitor(monitor *SeasonMonitor) error {
	return save(s.db, monitor)
}

// --- Episode ---

func (s *SQLiteStore) CreateEpisode(episode *Episode) error {
	return s.db.Create(episode).Error
}

func (s *SQLiteStore) ListEpisodesByMediaItem(mediaItemID uint) ([]Episode, error) {
	var episodes []Episode
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("season_number ASC, episode_number ASC").Find(&episodes).Error; err != nil {
		return nil, err
	}
	return episodes, nil
}

func (s *SQLiteStore) DeleteEpisodesByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&Episode{}).Error
}

// --- EpisodeMonitor ---

func (s *SQLiteStore) ListEpisodeMonitorsByMediaItem(mediaItemID uint) ([]EpisodeMonitor, error) {
	var monitors []EpisodeMonitor
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("season_number ASC, episode_number ASC").Find(&monitors).Error; err != nil {
		return nil, err
	}
	return monitors, nil
}

func (s *SQLiteStore) UpsertEpisodeMonitor(monitor *EpisodeMonitor) error {
	var existing EpisodeMonitor
	err := s.db.Where("media_item_id = ? AND season_number = ? AND episode_number = ?",
		monitor.MediaItemID, monitor.SeasonNumber, monitor.EpisodeNumber).First(&existing).Error
	if err == nil {
		existing.Monitored = monitor.Monitored
		return s.db.Save(&existing).Error
	}
	return s.db.Create(monitor).Error
}

func (s *SQLiteStore) DeleteEpisodeMonitorsBySeason(mediaItemID uint, seasonNumber int) error {
	return s.db.Where("media_item_id = ? AND season_number = ?", mediaItemID, seasonNumber).Delete(&EpisodeMonitor{}).Error
}

func (s *SQLiteStore) DeleteEpisodeMonitorsByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&EpisodeMonitor{}).Error
}

// --- Settings ---

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

func (s *SQLiteStore) DeleteSettingsByPrefix(prefix string) error {
	return s.db.Where("key LIKE ?", prefix+"%").Delete(&Setting{}).Error
}

func (s *SQLiteStore) ListSettingsByPrefix(prefix string) ([]Setting, error) {
	var settings []Setting
	if err := s.db.Where("key LIKE ?", prefix+"%").Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

// --- JobRecords ---

func (s *SQLiteStore) CreateJobRecord(record *JobRecord) error {
	return s.db.Create(record).Error
}

func (s *SQLiteStore) ListJobRecords(limit int) ([]JobRecord, error) {
	var records []JobRecord
	if err := s.db.Order("completed_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (s *SQLiteStore) DeleteOldJobRecords(keep int) error {
	return s.db.Exec(
		"DELETE FROM job_records WHERE id NOT IN (SELECT id FROM job_records ORDER BY completed_at DESC LIMIT ?)",
		keep,
	).Error
}

func (s *SQLiteStore) MaxJobRecordID() (uint, error) {
	var maxID uint
	if err := s.db.Model(&JobRecord{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return 0, err
	}
	return maxID, nil
}

// --- Indexer ---

func (s *SQLiteStore) CreateIndexer(indexer *Indexer) error {
	return s.db.Create(indexer).Error
}

func (s *SQLiteStore) GetIndexer(id uint) (*Indexer, error) {
	return getByID[Indexer](s.db, id)
}

func (s *SQLiteStore) ListIndexers() ([]Indexer, error) {
	var indexers []Indexer
	if err := s.db.Order("priority DESC, name ASC").Find(&indexers).Error; err != nil {
		return nil, err
	}
	return indexers, nil
}

func (s *SQLiteStore) UpdateIndexer(indexer *Indexer) error {
	return save(s.db, indexer)
}

func (s *SQLiteStore) DeleteIndexer(id uint) error {
	return deleteByID(s.db, &Indexer{}, id)
}

// --- Download ---

func (s *SQLiteStore) CreateDownload(download *Download) error {
	return s.db.Create(download).Error
}

func (s *SQLiteStore) GetDownload(id uint) (*Download, error) {
	return getByID[Download](s.db, id)
}

func (s *SQLiteStore) UpdateDownload(download *Download) error {
	return save(s.db, download)
}

func (s *SQLiteStore) ListDownloads(mediaItemID *uint, status *string) ([]Download, error) {
	var downloads []Download
	q := s.db.Order("created_at DESC")
	if mediaItemID != nil {
		q = q.Where("media_item_id = ?", *mediaItemID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if err := q.Find(&downloads).Error; err != nil {
		return nil, err
	}
	return downloads, nil
}

func (s *SQLiteStore) DeleteDownload(id uint) error {
	return deleteByID(s.db, &Download{}, id)
}

// --- User ---

func (s *SQLiteStore) CreateUser(user *User) error {
	return s.db.Create(user).Error
}

func (s *SQLiteStore) GetUser(id uint) (*User, error) {
	return getByID[User](s.db, id)
}

func (s *SQLiteStore) GetUserByEmail(email string) (*User, error) {
	var user User
	if err := s.db.Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *SQLiteStore) ListUsers() ([]User, error) {
	var users []User
	if err := s.db.Order("email ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (s *SQLiteStore) UpdateUser(user *User) error {
	return save(s.db, user)
}

func (s *SQLiteStore) DeleteUser(id uint) error {
	return deleteByID(s.db, &User{}, id)
}

func (s *SQLiteStore) CountUsers() (int64, error) {
	var count int64
	if err := s.db.Model(&User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// --- RefreshToken ---

func (s *SQLiteStore) CreateRefreshToken(token *RefreshToken) error {
	return s.db.Create(token).Error
}

func (s *SQLiteStore) GetRefreshTokenByToken(token string) (*RefreshToken, error) {
	var rt RefreshToken
	if err := s.db.Where("token = ?", token).First(&rt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &rt, nil
}

func (s *SQLiteStore) DeleteRefreshToken(token string) error {
	result := s.db.Where("token = ?", token).Delete(&RefreshToken{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (s *SQLiteStore) DeleteRefreshTokensByUser(userID uint) error {
	return s.db.Where("user_id = ?", userID).Delete(&RefreshToken{}).Error
}

func (s *SQLiteStore) DeleteExpiredRefreshTokens() error {
	return s.db.Where("expires_at < ?", time.Now()).Delete(&RefreshToken{}).Error
}

// --- WatchedItem ---

func (s *SQLiteStore) CreateWatchedItem(item *WatchedItem) error {
	return s.db.Create(item).Error
}

func (s *SQLiteStore) DeleteWatchedItem(id uint) error {
	return deleteByID(s.db, &WatchedItem{}, id)
}

func (s *SQLiteStore) ListWatchedItems() ([]WatchedItem, error) {
	var items []WatchedItem
	if err := s.db.Order("watched_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListWatchedItemsByUser(userID uint) ([]WatchedItem, error) {
	var items []WatchedItem
	if err := s.db.Where("user_id = ?", userID).Order("watched_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) GetWatchedBySourceExternal(userID *uint, source string, externalID int) (*WatchedItem, error) {
	var item WatchedItem
	q := s.db.Where("source = ? AND external_id = ?", source, externalID)
	if userID != nil {
		q = q.Where("user_id = ?", *userID)
	}
	if err := q.First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) ClearWatchedMediaItemID(mediaItemID uint) error {
	return s.db.Model(&WatchedItem{}).Where("media_item_id = ?", mediaItemID).Update("media_item_id", nil).Error
}

func (s *SQLiteStore) WithTx(fn func(Store) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		return fn(&SQLiteStore{db: tx})
	})
}

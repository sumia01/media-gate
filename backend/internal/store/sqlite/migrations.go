package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// migrations is the ordered list of schema migration functions.
// Each function migrates from version N to N+1 (index 0 = version 0→1).
// New migrations are appended to the end — never reorder or remove entries.
var migrations = []func(*sql.DB) error{
	migrateV1, // 0→1: FK constraint rebuilds + orphan cleanup (existing tables)
	migrateV2, // 1→2: watched_items — add media_item_id FK with SET NULL
	migrateV3, // 2→3: explicit season monitoring — add monitor_new_seasons column + backfill SeasonMonitor rows
	migrateV4, // 3→4: episode_monitors table (AutoMigrate handles creation, this is a no-op placeholder)
	migrateV5, // 4→5: subtitles table (AutoMigrate handles creation, this is a no-op placeholder)
	migrateV6, // 5→6: media_profiles.language_mode — backfill existing rows with 'or'
	migrateV7, // 6→7: users.is_admin — promote the first-created user to admin
	migrateV8, // 7→8: download_blocklists table (created here — NOT in AutoMigrate)
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

	return nil
}

// migrateV2 rebuilds watched_items to add the media_item_id FK with ON DELETE SET NULL.
func migrateV2(db *sql.DB) error {
	if tableHasFKOnColumn(db, "watched_items", "media_item_id") {
		return nil
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
// explicit SeasonMonitor rows for all monitored series.
func migrateV3(db *sql.DB) error {
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

// migrateV4 is a no-op placeholder — AutoMigrate already creates the episode_monitors table.
func migrateV4(_ *sql.DB) error {
	return nil
}

// migrateV5 is a no-op placeholder — AutoMigrate already creates the subtitles table.
func migrateV5(_ *sql.DB) error {
	return nil
}

// rebuildTable recreates a table using SQLite's 12-step ALTER TABLE procedure.
func rebuildTable(db *sql.DB, table, newDDL, columns string) error {
	slog.Info("rebuilding table", "table", table)

	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign_keys for %s: %w", table, err)
	}
	defer func() {
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			slog.Error("failed to re-enable foreign_keys", "table", table, "error", err)
		}
	}()

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
			_ = tx.Rollback()
			return fmt.Errorf("rebuild %s: %w", table, err)
		}
	}

	return tx.Commit()
}

func tableHasForeignKeys(db *sql.DB, table string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list('%s')", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

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
		{"download_blocklists", `DELETE FROM download_blocklists WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"subtitles", `DELETE FROM subtitles WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
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

// migrateV6 backfills language_mode='or' for existing media profiles.
// AutoMigrate adds the column with DEFAULT 'or', but we explicitly set
// the value to ensure no NULLs remain.
func migrateV6(db *sql.DB) error {
	_, err := db.Exec(`UPDATE media_profiles SET language_mode = 'or' WHERE language_mode IS NULL OR language_mode = ''`)
	return err
}

// migrateV7 promotes the first-created user (lowest ID) to admin.
// AutoMigrate adds the is_admin column with DEFAULT false; this migration
// ensures the original user gets admin privileges.
func migrateV7(db *sql.DB) error {
	_, err := db.Exec(`UPDATE users SET is_admin = true WHERE id = (SELECT MIN(id) FROM users)`)
	return err
}

// migrateV8 creates the download_blocklists table. Unlike episode_monitors and
// subtitles, this model is NOT registered with AutoMigrate (its owning fix does
// not touch sqlite.go), so the table must be created explicitly here. The FK to
// media_items uses ON DELETE CASCADE to mirror the GORM constraint tag.
func migrateV8(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS "download_blocklists" (
		"id" integer PRIMARY KEY AUTOINCREMENT,
		"media_item_id" integer NOT NULL REFERENCES "media_items"("id") ON DELETE CASCADE,
		"download_url" text NOT NULL,
		"title" text,
		"fail_count" integer NOT NULL DEFAULT 0,
		"last_error" text,
		"last_failed_at" datetime,
		"created_at" datetime,
		"updated_at" datetime
	)`)
	if err != nil {
		return fmt.Errorf("creating download_blocklists table: %w", err)
	}

	indexDDLs := []string{
		`CREATE INDEX IF NOT EXISTS "idx_download_blocklists_media_item_id" ON "download_blocklists"("media_item_id")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_blocklist_item_url" ON "download_blocklists"("media_item_id","download_url")`,
	}
	for _, ddl := range indexDDLs {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("creating download_blocklists index: %w", err)
		}
	}
	return nil
}

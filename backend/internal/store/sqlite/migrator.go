package sqlite

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const (
	migrationsDir   = "migrations"
	migrationsTable = "schema_migrations"
	// baselineVersion is the golang-migrate version number of 0001_baseline — the
	// migration that encodes the complete pre-golang-migrate schema. Existing
	// databases are Force()d to this version so the baseline SQL is never re-run
	// against them.
	baselineVersion = 1
	// baselineSchemaVersion is the OLD settings.schema_version that 0001_baseline
	// reproduces. A legacy database is only safe to adopt (stamp the baseline)
	// once it reached this version under the old AutoMigrate + V1..V9 system;
	// below it the DB is missing schema that the (now-removed) old migrations
	// would have added, so we refuse rather than silently stamp it complete.
	baselineSchemaVersion = 9
)

// glebarezDriver is a golang-migrate database.Driver implemented directly over
// the GORM/glebarez *sql.DB.
//
// We cannot use golang-migrate's stock database/sqlite driver: it blank-imports
// modernc.org/sqlite, which registers the same "sqlite" database/sql driver name
// as glebarez/go-sqlite (our GORM driver) and panics at init ("sql: Register
// called twice for driver sqlite"). The CGO sqlite3 driver is likewise off the
// table. This adapter runs migrations through the very connection glebarez
// already owns — no second driver, no CGO. It is a thin, real adapter (not a
// mock): every method delegates to the shared *sql.DB.
type glebarezDriver struct {
	db *sql.DB
}

func newGlebarezDriver(db *sql.DB) (*glebarezDriver, error) {
	d := &glebarezDriver{db: db}
	if err := d.ensureVersionTable(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *glebarezDriver) ensureVersionTable() error {
	_, err := d.db.Exec(fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS "%s" (version BIGINT NOT NULL, dirty BOOLEAN NOT NULL)`,
		migrationsTable))
	return err
}

// Open is required by the interface but unused — we always construct via
// NewWithInstance. Fail loudly if something routes through it.
func (d *glebarezDriver) Open(string) (database.Driver, error) {
	return nil, errors.New("glebarezDriver: Open is unsupported; use NewWithInstance")
}

// Close is a deliberate no-op: the *sql.DB is owned by GORM, not the migrator.
func (d *glebarezDriver) Close() error { return nil }

// Lock/Unlock are no-ops: migrations run once at startup in a single process.
func (d *glebarezDriver) Lock() error   { return nil }
func (d *glebarezDriver) Unlock() error { return nil }

// Run applies one migration file, atomically. glebarez/go-sqlite (a modernc fork)
// executes every ";"-separated statement in a single Exec — verified — so the
// whole file runs in one call, wrapped in a transaction so a mid-file failure
// rolls back cleanly instead of leaving a half-built schema stamped dirty.
// SQLite DDL is transactional. Caveat: "PRAGMA foreign_keys" is a no-op inside a
// transaction, so a future migration that must rebuild a table with FK
// enforcement disabled has to arrange that outside this path.
func (d *glebarezDriver) Run(migration io.Reader) error {
	script, err := io.ReadAll(migration)
	if err != nil {
		return err
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(string(script)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("running migration: %w", err)
	}
	return tx.Commit()
}

// SetVersion records the active version, mirroring golang-migrate's stock
// semantics: a single row that is deleted and re-inserted.
func (d *glebarezDriver) SetVersion(version int, dirty bool) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM "%s"`, migrationsTable)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if version >= 0 || (version == database.NilVersion && dirty) {
		if _, err := tx.Exec(
			fmt.Sprintf(`INSERT INTO "%s" (version, dirty) VALUES (?, ?)`, migrationsTable),
			version, dirty,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (d *glebarezDriver) Version() (int, bool, error) {
	var version int
	var dirty bool
	err := d.db.QueryRow(
		fmt.Sprintf(`SELECT version, dirty FROM "%s" LIMIT 1`, migrationsTable),
	).Scan(&version, &dirty)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return database.NilVersion, false, nil
	case err != nil:
		return 0, false, err
	default:
		return version, dirty, nil
	}
}

// Drop removes every user table. Only reachable via migrate.Drop(), which the
// app never calls; implemented for interface completeness.
func (d *glebarezDriver) Drop() error {
	rows, err := d.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	if _, err := d.db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() { _, _ = d.db.Exec("PRAGMA foreign_keys = ON") }()
	for _, t := range tables {
		if _, err := d.db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, t)); err != nil {
			return err
		}
	}
	return nil
}

// runSchemaMigrations brings the schema up to date via golang-migrate, using the
// embedded SQL files and the glebarez-backed driver above.
//
// Zero data loss on adoption: a database built by the OLD GORM AutoMigrate +
// V1..V9 system already has the full schema but no schema_migrations row. We
// detect that and Force() the baseline version WITHOUT executing the baseline
// SQL, so existing data is never touched. Fresh databases (no app tables) run
// the baseline to build everything. Databases already tracked by golang-migrate
// simply apply any pending migrations (or no-op).
func runSchemaMigrations(db *sql.DB) error {
	driver, err := newGlebarezDriver(db)
	if err != nil {
		return fmt.Errorf("init migrate driver: %w", err)
	}

	src, err := iofs.New(migrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "glebarez", driver)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}

	force, err := adoptionDecision(db)
	if err != nil {
		return err
	}
	if force {
		slog.Info("adopting existing pre-golang-migrate database: stamping baseline schema version without running it", "version", baselineVersion)
		if err := m.Force(baselineVersion); err != nil {
			return fmt.Errorf("forcing baseline version: %w", err)
		}
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// adoptionDecision decides whether to Force() the baseline version for an
// existing database, or return an error when the database is unsafe to adopt.
//
//   - No media_items table  → fresh install; return (false, nil) so Up() builds
//     everything from the baseline.
//   - schema_migrations already has a row → already managed by golang-migrate;
//     return (false, nil) so Up() just applies pending migrations.
//   - media_items present, no schema_migrations row → a pre-golang-migrate DB.
//     It is only safe to stamp the baseline if it reached the old
//     schema_version 9 that the baseline reproduces. Below that it is missing
//     schema the removed V1..V9 migrations would have added and AutoMigrate no
//     longer reconciles, so we REFUSE (error) rather than stamp it complete —
//     failing loudly with data intact instead of corrupting silently.
func adoptionDecision(db *sql.DB) (forceBaseline bool, err error) {
	if !tableExists(db, "media_items") {
		return false, nil
	}
	var v int
	rowErr := db.QueryRow(fmt.Sprintf(`SELECT version FROM "%s" LIMIT 1`, migrationsTable)).Scan(&v)
	if rowErr == nil {
		return false, nil // golang-migrate already tracks this DB
	}
	if !errors.Is(rowErr, sql.ErrNoRows) {
		return false, fmt.Errorf("reading %s: %w", migrationsTable, rowErr)
	}
	old := oldSchemaVersion(db)
	if old >= baselineSchemaVersion {
		return true, nil
	}
	return false, fmt.Errorf(
		"refusing to adopt database at legacy schema_version %d: the golang-migrate baseline reproduces schema_version %d; first upgrade through an earlier media-gate release (one that still ran AutoMigrate + V1..V9) to reach it, then retry",
		old, baselineSchemaVersion)
}

// oldSchemaVersion reads the version marker written by the removed pre-golang-
// migrate migration system (settings.schema_version). Returns 0 when absent.
func oldSchemaVersion(db *sql.DB) int {
	var val string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&val); err != nil {
		return 0
	}
	v, _ := strconv.Atoi(val)
	return v
}

// applyLegacyRenames performs pre-baseline, idempotent fixups for very old
// databases whose column/table names predate the current schema. Errors are
// ignored: on fresh installs the tables don't exist yet, and on already-migrated
// installs the renames/additions are duplicates. Preserves the exact behavior the
// old New() had before AutoMigrate ran.
func applyLegacyRenames(db *sql.DB) {
	stmts := []string{
		"ALTER TABLE quality_profiles RENAME TO media_profiles",
		"ALTER TABLE media_profiles ADD COLUMN languages TEXT DEFAULT ''",
		"ALTER TABLE libraries RENAME COLUMN quality_profile_id TO media_profile_id",
		"ALTER TABLE media_items RENAME COLUMN quality_profile_id TO media_profile_id",
		"UPDATE media_items SET status = 'available' WHERE status = 'matched'",
	}
	for _, s := range stmts {
		_, _ = db.Exec(s)
	}
}

func tableExists(db *sql.DB, name string) bool {
	var got string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, name,
	).Scan(&got)
	return err == nil
}

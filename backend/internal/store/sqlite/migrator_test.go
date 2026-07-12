package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// TestAdoptExistingDatabasePreservesData proves the zero-data-loss adoption path:
// a database that already has the full schema and data but NO schema_migrations
// row (i.e. one built by the old AutoMigrate + V1..V9 system) is adopted by
// stamping the baseline version — WITHOUT re-running the baseline SQL and WITHOUT
// touching any data. If Force() were skipped, Up() would try to run 0001_baseline
// against existing tables and fail with "table already exists".
func TestAdoptExistingDatabasePreservesData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// Boot 1: build schema + seed data.
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 1): %v", err)
	}
	lib := &store.Library{Name: "lib", Path: dir, MediaType: "series"}
	if err := s1.CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	item := &store.MediaItem{
		LibraryID: lib.ID, Title: "Silo", MediaType: "series",
		Status: "new", Source: "disk", PreferredRelease: "ETHEL",
	}
	if err := s1.CreateMediaItem(item); err != nil {
		t.Fatalf("CreateMediaItem: %v", err)
	}

	// Simulate a real pre-golang-migrate v9 database: it carries the old
	// schema_version marker (9) and has no golang-migrate tracking table, but
	// keeps every other table and all data intact.
	sqlDB, _ := s1.db.DB()
	if _, err := sqlDB.Exec(
		`INSERT INTO settings (key, value, sensitive, created_at, updated_at)
		 VALUES ('schema_version', '9', 0, datetime('now'), datetime('now'))`,
	); err != nil {
		t.Fatalf("seeding legacy schema_version: %v", err)
	}
	if _, err := sqlDB.Exec(`DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("dropping schema_migrations: %v", err)
	}
	_ = s1.Close()

	// Boot 2: reopen. Must ADOPT (Force baseline), not re-run it.
	s2, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 2, adoption): %v", err)
	}
	defer s2.Close()

	got, err := s2.GetMediaItem(item.ID)
	if err != nil {
		t.Fatalf("GetMediaItem after adoption: %v", err)
	}
	if got.Title != "Silo" {
		t.Errorf("title changed on adoption: got %q, want %q", got.Title, "Silo")
	}
	if got.PreferredRelease != "ETHEL" {
		t.Errorf("preferred_release changed on adoption: got %q, want %q", got.PreferredRelease, "ETHEL")
	}

	// The version must now be stamped at the baseline.
	sqlDB2, _ := s2.db.DB()
	var v int
	if err := sqlDB2.QueryRow(`SELECT version FROM schema_migrations LIMIT 1`).Scan(&v); err != nil {
		t.Fatalf("reading schema_migrations after adoption: %v", err)
	}
	if v != baselineVersion {
		t.Errorf("adopted version = %d, want %d", v, baselineVersion)
	}
}

// TestAdoptBelowBaselineRefusesWithoutDataLoss proves the safety guard: a legacy
// database below the baseline's schema_version (i.e. one that never reached the
// old v9 and is missing schema the removed V1..V9 migrations would have added)
// is REFUSED rather than silently stamped complete. New() returns an error and
// the data is left untouched — fail loudly, lose nothing.
func TestAdoptBelowBaselineRefusesWithoutDataLoss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s1, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 1): %v", err)
	}
	lib := &store.Library{Name: "lib", Path: dir, MediaType: "series"}
	if err := s1.CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Silo", MediaType: "series", Status: "new", Source: "disk"}
	if err := s1.CreateMediaItem(item); err != nil {
		t.Fatalf("CreateMediaItem: %v", err)
	}

	// Make it look like a stale pre-v9 legacy DB: old marker at 5, no tracking table.
	sqlDB, _ := s1.db.DB()
	if _, err := sqlDB.Exec(
		`INSERT INTO settings (key, value, sensitive, created_at, updated_at)
		 VALUES ('schema_version', '5', 0, datetime('now'), datetime('now'))`,
	); err != nil {
		t.Fatalf("seeding legacy schema_version: %v", err)
	}
	if _, err := sqlDB.Exec(`DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("dropping schema_migrations: %v", err)
	}
	_ = s1.Close()

	// Boot 2 must REFUSE (error), not adopt.
	s2, err := New(path)
	if err == nil {
		_ = s2.Close()
		t.Fatalf("New (boot 2) adopted a below-baseline DB; want an error")
	}

	// Data must be untouched — reopen with a raw connection and verify the row.
	raw := openRawForTest(t, path)
	defer raw.Close()
	var title string
	if err := raw.QueryRow(`SELECT title FROM media_items WHERE id = ?`, item.ID).Scan(&title); err != nil {
		t.Fatalf("reading media_items after refused adoption: %v", err)
	}
	if title != "Silo" {
		t.Errorf("data changed after refused adoption: title = %q, want %q", title, "Silo")
	}
}

// TestFreshInstallSchema guards the fresh-install schema: the baseline must be
// stamped at the baseline version and must include model columns that the old
// system dropped/omitted (notably media_metadata.trailer_url, which was missing
// on a first boot under AutoMigrate).
func TestFreshInstallSchema(t *testing.T) {
	s := newTestStore(t)
	sqlDB, _ := s.db.DB()

	var v int
	if err := sqlDB.QueryRow(`SELECT version FROM schema_migrations LIMIT 1`).Scan(&v); err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}
	if v != baselineVersion {
		t.Errorf("fresh install version = %d, want %d", v, baselineVersion)
	}

	mustHaveColumn(t, sqlDB, "media_metadata", "trailer_url")
	mustHaveColumn(t, sqlDB, "media_items", "preferred_release")
	mustHaveColumn(t, sqlDB, "media_items", "monitor_new_seasons")
}

// openRawForTest opens a bare *sql.DB against a DB file (bypassing New()), so a
// test can inspect data after New() intentionally fails. The "sqlite" driver is
// registered transitively via the glebarez GORM driver.
func openRawForTest(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	return db
}

func mustHaveColumn(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info('" + table + "')")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt *string
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		if name == column {
			return
		}
	}
	t.Errorf("column %s.%s missing from fresh-install schema", table, column)
}

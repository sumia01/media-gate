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

	// The version must now be stamped at the latest migration: Force(baseline)
	// skips re-running 0001's SQL, but Up() still applies everything after it.
	sqlDB2, _ := s2.db.DB()
	var v int
	if err := sqlDB2.QueryRow(`SELECT version FROM schema_migrations LIMIT 1`).Scan(&v); err != nil {
		t.Fatalf("reading schema_migrations after adoption: %v", err)
	}
	if v != latestMigrationVersion {
		t.Errorf("adopted version = %d, want %d", v, latestMigrationVersion)
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
	if v != latestMigrationVersion {
		t.Errorf("fresh install version = %d, want %d", v, latestMigrationVersion)
	}

	mustHaveColumn(t, sqlDB, "media_metadata", "trailer_url")
	mustHaveColumn(t, sqlDB, "media_items", "preferred_release")
	mustHaveColumn(t, sqlDB, "media_items", "monitor_new_seasons")
}

// TestCleanupRaceOrphanedDownloads_0002 exercises the actual embedded 0002
// migration SQL (not a reimplementation of its logic) against a seeded
// "buggy" downloads table. It proves the DELETE removes only a row with
// positive proof of a successful replacement for the same episode, and
// leaves everything else alone: a genuinely failed episode with no
// replacement, a movie (no episode_id), and a failure that carries a real
// error message.
func TestCleanupRaceOrphanedDownloads_0002(t *testing.T) {
	s := newTestStore(t)
	sqlDB, _ := s.db.DB()

	lib := &store.Library{Name: "lib", Path: t.TempDir(), MediaType: "series"}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: "series", Status: "available", Source: "disk"}
	if err := s.CreateMediaItem(item); err != nil {
		t.Fatalf("CreateMediaItem: %v", err)
	}

	// episode_id has a real FK to episodes(id), so seed actual rows rather
	// than arbitrary numbers.
	epSuperseded := &store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1}
	epNoReplacement := &store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 2}
	epGenuineFailure := &store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 3}
	for _, ep := range []*store.Episode{epSuperseded, epNoReplacement, epGenuineFailure} {
		if err := s.CreateEpisode(ep); err != nil {
			t.Fatalf("CreateEpisode: %v", err)
		}
	}

	insert := func(id uint, episodeID any, status, hash, lastError string, retryCount, linked int) {
		if _, err := sqlDB.Exec(
			`INSERT INTO downloads (id, media_item_id, episode_id, indexer_id, indexer_name, title, download_url, status, client_torrent_hash, last_error, retry_count, linked_to_library, created_at, updated_at)
			 VALUES (?, ?, ?, 1, 'idx', 'title', 'url', ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
			id, item.ID, episodeID, status, hash, lastError, retryCount, linked,
		); err != nil {
			t.Fatalf("seed download %d: %v", id, err)
		}
	}

	insert(1, epSuperseded.ID, "failed", "", "", 0, 0)                    // race-orphaned, superseded by 2 -> DELETE
	insert(2, epSuperseded.ID, "completed", "abc", "", 0, 1)              // the successful replacement
	insert(3, epNoReplacement.ID, "failed", "", "", 0, 0)                 // failed, no replacement -> KEEP
	insert(4, nil, "failed", "", "", 0, 0)                                // movie (no episode_id) -> KEEP
	insert(5, epGenuineFailure.ID, "failed", "", "some real error", 0, 0) // genuine failure -> KEEP
	insert(6, epGenuineFailure.ID, "completed", "def", "", 0, 1)

	script, err := migrationsFS.ReadFile(migrationsDir + "/0002_cleanup_race_orphaned_downloads.up.sql")
	if err != nil {
		t.Fatalf("reading 0002 migration: %v", err)
	}
	if _, err := sqlDB.Exec(string(script)); err != nil {
		t.Fatalf("running 0002 migration: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id FROM downloads ORDER BY id`)
	if err != nil {
		t.Fatalf("query remaining: %v", err)
	}
	defer rows.Close()
	var remaining []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		remaining = append(remaining, id)
	}

	want := map[int]bool{2: true, 3: true, 4: true, 5: true, 6: true}
	if len(remaining) != len(want) {
		t.Fatalf("remaining download IDs = %v, want the 5 IDs in %v (only row 1 should be deleted)", remaining, want)
	}
	for _, id := range remaining {
		if !want[id] {
			t.Errorf("unexpected survivor id=%d", id)
		}
	}
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

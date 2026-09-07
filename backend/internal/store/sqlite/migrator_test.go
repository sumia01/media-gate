package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
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
	// media_metadata is seeded too: adoption manipulates that table's schema, so
	// without a row here a rebuild that wiped every metadata row (the exact
	// failure ADR-125 exists to prevent) would still leave this test green.
	meta := &store.MediaMetadata{
		MediaItemID: item.ID, Source: "tvdb", ExternalID: 1234,
		Title: "Silo", TrailerURL: "https://youtu.be/abc",
	}
	if err := s1.CreateMediaMetadata(meta); err != nil {
		t.Fatalf("CreateMediaMetadata: %v", err)
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
	// Boot 1 created a *current* schema, but a genuine v9 database predates every
	// post-baseline migration and therefore carries none of their columns. Strip
	// them back off so the fixture is a faithful legacy database; without this the
	// test would assert that adoption survives re-applying a DDL migration onto a
	// column that already exists — a situation no real legacy database can reach,
	// since the AutoMigrate era that produced un-stamped databases ended before
	// any of these columns existed in the model.
	//
	// Extend this list whenever a post-baseline migration adds schema.
	for _, stmt := range []string{
		`ALTER TABLE media_metadata DROP COLUMN content_ratings`,
		`ALTER TABLE downloads DROP COLUMN downloaded_at`,
		`DROP TABLE monitor_decisions`,
		`DROP INDEX idx_episodes_air_date`,
		`DROP TABLE media_requests`,
		`DROP TABLE media_activity`,
		`ALTER TABLE media_items DROP COLUMN deletion_pending`,
		`DROP INDEX idx_watched_user_source_type_ext`,
		`CREATE UNIQUE INDEX idx_watched_user_source_ext ON watched_items(user_id, source, external_id)`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("stripping post-baseline schema (%s): %v", stmt, err)
		}
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

	gotMeta, err := s2.GetMediaMetadataByMediaItem(item.ID)
	if err != nil {
		t.Fatalf("GetMediaMetadataByMediaItem after adoption: %v", err)
	}
	if gotMeta.Title != "Silo" || gotMeta.ExternalID != 1234 {
		t.Errorf("metadata changed on adoption: got title %q external %d", gotMeta.Title, gotMeta.ExternalID)
	}
	if gotMeta.TrailerURL != "https://youtu.be/abc" {
		t.Errorf("trailer_url changed on adoption: got %q", gotMeta.TrailerURL)
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
	if v != 10 || latestMigrationVersion != 10 {
		t.Errorf("fresh install version = %d and latestMigrationVersion = %d, want both 10", v, latestMigrationVersion)
	}

	mustHaveColumn(t, sqlDB, "media_metadata", "trailer_url")
	mustHaveColumn(t, sqlDB, "media_metadata", "content_ratings")
	mustHaveColumn(t, sqlDB, "media_items", "preferred_release")
	mustHaveColumn(t, sqlDB, "media_items", "monitor_new_seasons")
	mustHaveColumn(t, sqlDB, "downloads", "downloaded_at")
	mustHaveColumn(t, sqlDB, "monitor_decisions", "input_updated_at")
	mustHaveColumn(t, sqlDB, "media_requests", "requested_at")
	mustHaveColumn(t, sqlDB, "media_activity", "recorded_at")
	mustHaveColumn(t, sqlDB, "media_items", "deletion_pending")
}

func TestMediaActivityMigrationFromVersionNineDoesNotBackfill(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "version9.db")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 1): %v", err)
	}
	item := mustCreateMediaItem(t, s1)
	user := &store.User{Email: "existing-requester@example.com", PasswordHash: "hash"}
	if err := s1.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s1.CreateMediaRequest(&store.MediaRequest{
		MediaItemID: item.ID,
		UserID:      &user.ID,
		Scope:       store.MediaRequestScopeMedia,
	}); err != nil {
		t.Fatalf("CreateMediaRequest: %v", err)
	}
	watchedMovie := &store.WatchedItem{
		UserID: user.ID, Source: "tmdb", ExternalID: 42, Title: "Movie", MediaType: "movie",
	}
	if err := s1.CreateWatchedItem(watchedMovie); err != nil {
		t.Fatalf("CreateWatchedItem: %v", err)
	}

	sqlDB, _ := s1.db.DB()
	down, err := migrationsFS.ReadFile(migrationsDir + "/0010_media_activity.down.sql")
	if err != nil {
		t.Fatalf("reading 0010 down migration: %v", err)
	}
	if _, err := sqlDB.Exec(string(down)); err != nil {
		t.Fatalf("restoring version 9 schema: %v", err)
	}
	if hasColumn(t, sqlDB, "media_items", "deletion_pending") {
		t.Fatal("version 9 schema retained deletion_pending")
	}
	if _, err := sqlDB.Exec(`UPDATE schema_migrations SET version = 9, dirty = 0`); err != nil {
		t.Fatalf("resetting migration version: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close (boot 1): %v", err)
	}

	s2, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 2): %v", err)
	}
	defer s2.Close()
	sqlDB, _ = s2.db.DB()
	var version, activityCount int
	if err := sqlDB.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("reading migrated version: %v", err)
	}
	if version != 10 {
		t.Fatalf("migrated version = %d, want 10", version)
	}
	if err := sqlDB.QueryRow(`SELECT count(*) FROM media_activity`).Scan(&activityCount); err != nil {
		t.Fatalf("counting migrated activity: %v", err)
	}
	if activityCount != 0 {
		t.Fatalf("version 9 upgrade fabricated %d activity rows", activityCount)
	}
	if !hasColumn(t, sqlDB, "media_items", "deletion_pending") {
		t.Fatal("version 9 upgrade did not add deletion_pending")
	}
	upgradedItem, err := s2.GetMediaItem(item.ID)
	if err != nil || upgradedItem.DeletionPending {
		t.Fatalf("version 9 item deletion claim = %+v, %v; want false", upgradedItem, err)
	}
	if err := s2.CreateWatchedItem(&store.WatchedItem{
		UserID: user.ID, Source: "tmdb", ExternalID: 42, Title: "Series", MediaType: "series",
	}); err != nil {
		t.Fatalf("creating same-id series watched row after version 9 upgrade: %v", err)
	}
	requests, err := s2.ListMediaRequestsByMediaItem(item.ID)
	if err != nil || len(requests) != 1 || requests[0].UserID == nil || *requests[0].UserID != user.ID {
		t.Fatalf("existing request changed during upgrade: %+v, %v", requests, err)
	}
}

func TestMediaActivityMigrationRollbackAndReapply(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := &store.User{Email: "watched-migration@example.com", PasswordHash: "hash"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateWatchedItem(&store.WatchedItem{
		UserID: user.ID, Source: "tmdb", ExternalID: 42, Title: "Movie", MediaType: "movie",
	}); err != nil {
		t.Fatalf("CreateWatchedItem: %v", err)
	}
	activity, err := store.NewSystemMediaActivity(
		item,
		"migration-test",
		store.MediaActivityActionMetadataChanged,
		"before-rollback",
		store.MediaActivityDetails{Reason: "test"},
	)
	if err != nil {
		t.Fatalf("NewSystemMediaActivity: %v", err)
	}
	if err := s.AppendMediaActivity(activity); err != nil {
		t.Fatalf("AppendMediaActivity: %v", err)
	}

	sqlDB, _ := s.db.DB()
	driver, err := newGlebarezDriver(sqlDB)
	if err != nil {
		t.Fatalf("newGlebarezDriver: %v", err)
	}
	source, err := iofs.New(migrationsFS, migrationsDir)
	if err != nil {
		t.Fatalf("opening migration source: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "glebarez", driver)
	if err != nil {
		t.Fatalf("creating migrator: %v", err)
	}
	defer m.Close()

	if err := m.Steps(-1); err != nil {
		t.Fatalf("rolling back 0010: %v", err)
	}
	if tableExists(sqlDB, "media_activity") {
		t.Fatal("media_activity still exists after rolling back 0010")
	}
	if hasColumn(t, sqlDB, "media_items", "deletion_pending") {
		t.Fatal("deletion_pending still exists after rolling back 0010")
	}
	if version, _, err := driver.Version(); err != nil || version != 9 {
		t.Fatalf("version after rollback = %d, %v; want 9", version, err)
	}
	conflictingType := &store.WatchedItem{
		UserID: user.ID, Source: "tmdb", ExternalID: 42, Title: "Series", MediaType: "series",
	}
	if err := s.CreateWatchedItem(conflictingType); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("same-id movie/series under version 9 index error = %v, want ErrDuplicate", err)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("reapplying 0010: %v", err)
	}
	if !tableExists(sqlDB, "media_activity") {
		t.Fatal("media_activity missing after reapplying 0010")
	}
	if !hasColumn(t, sqlDB, "media_items", "deletion_pending") {
		t.Fatal("deletion_pending missing after reapplying 0010")
	}
	if current, err := s.GetMediaItem(item.ID); err != nil || current.DeletionPending {
		t.Fatalf("reapplied deletion claim = %+v, %v; want false", current, err)
	}
	if version, _, err := driver.Version(); err != nil || version != 10 {
		t.Fatalf("version after reapply = %d, %v; want 10", version, err)
	}
	var count int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM media_activity`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("activity after rollback/reapply = %d, %v; want empty replacement table", count, err)
	}
	activity.OperationID = "after-reapply"
	if err := s.AppendMediaActivity(activity); err != nil {
		t.Fatalf("AppendMediaActivity after reapply: %v", err)
	}
	if err := s.CreateWatchedItem(conflictingType); err != nil {
		t.Fatalf("same-id movie/series after reapply: %v", err)
	}
}

func TestMediaActivityMigrationRollbackRefusesIdentityCollision(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := &store.User{Email: "watched-collision@example.com", PasswordHash: "hash"}
	if err := s.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	for _, mediaType := range []string{"movie", "series"} {
		if err := s.CreateWatchedItem(&store.WatchedItem{
			UserID: user.ID, Source: "tmdb", ExternalID: 42, Title: mediaType, MediaType: mediaType,
		}); err != nil {
			t.Fatalf("seeding %s watched row: %v", mediaType, err)
		}
	}
	activity, err := store.NewSystemMediaActivity(
		item,
		"migration-test",
		store.MediaActivityActionMetadataChanged,
		"preserve-on-refusal",
		store.MediaActivityDetails{Reason: "test"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMediaActivity(activity); err != nil {
		t.Fatal(err)
	}

	sqlDB, _ := s.db.DB()
	driver, err := newGlebarezDriver(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	source, err := iofs.New(migrationsFS, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "glebarez", driver)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if err := m.Steps(-1); err == nil {
		t.Fatal("rollback with old-key movie/series collision succeeded")
	}
	if version, dirty, err := driver.Version(); err != nil || version != 10 || dirty {
		t.Fatalf("version after refused rollback = %d dirty=%v err=%v; want clean 10", version, dirty, err)
	}
	if !tableExists(sqlDB, "media_activity") {
		t.Fatal("refused rollback dropped media_activity")
	}
	var activityCount, watchedCount int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM media_activity`).Scan(&activityCount); err != nil || activityCount != 1 {
		t.Fatalf("activity after refused rollback = %d, %v; want 1", activityCount, err)
	}
	if err := sqlDB.QueryRow(`SELECT count(*) FROM watched_items WHERE user_id = ? AND source = 'tmdb' AND external_id = 42`, user.ID).Scan(&watchedCount); err != nil || watchedCount != 2 {
		t.Fatalf("watched rows after refused rollback = %d, %v; want 2", watchedCount, err)
	}
	var currentIndexCount, oldIndexCount int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_watched_user_source_type_ext'`).Scan(&currentIndexCount); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_watched_user_source_ext'`).Scan(&oldIndexCount); err != nil {
		t.Fatal(err)
	}
	if currentIndexCount != 1 || oldIndexCount != 0 {
		t.Fatalf("indexes after refused rollback: current=%d old=%d", currentIndexCount, oldIndexCount)
	}
}

func TestDownloadedAtMigrationPreservesExistingDownloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 1): %v", err)
	}
	item := mustCreateMediaItem(t, s1)
	dl := &store.Download{
		MediaItemID: item.ID,
		IndexerID:   1,
		IndexerName: "idx",
		Title:       "Show.S01E01.1080p",
		DownloadURL: "magnet:?x",
		Status:      "completed",
	}
	if err := s1.CreateDownload(dl); err != nil {
		t.Fatalf("CreateDownload: %v", err)
	}

	sqlDB, _ := s1.db.DB()
	// Recreate version 3, including removal of tables/indexes added afterward.
	for _, stmt := range []string{
		`ALTER TABLE downloads DROP COLUMN downloaded_at`,
		`DROP TABLE monitor_decisions`,
		`DROP INDEX idx_episodes_air_date`,
		`DROP TABLE media_requests`,
		`DROP TABLE media_activity`,
		`ALTER TABLE media_items DROP COLUMN deletion_pending`,
		`DROP INDEX idx_watched_user_source_type_ext`,
		`CREATE UNIQUE INDEX idx_watched_user_source_ext ON watched_items(user_id, source, external_id)`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("stripping post-v3 schema (%s): %v", stmt, err)
		}
	}
	if _, err := sqlDB.Exec(`UPDATE schema_migrations SET version = 3, dirty = 0`); err != nil {
		t.Fatalf("resetting migration version: %v", err)
	}
	_ = s1.Close()

	s2, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 2): %v", err)
	}
	defer s2.Close()

	got, err := s2.GetDownload(dl.ID)
	if err != nil {
		t.Fatalf("GetDownload after migration: %v", err)
	}
	if got.Title != dl.Title || got.Status != dl.Status {
		t.Errorf("download changed during migration: got title %q status %q", got.Title, got.Status)
	}
	if got.DownloadedAt != nil {
		t.Errorf("DownloadedAt = %v, want nil for an existing row", got.DownloadedAt)
	}
}

func TestMediaRequestsMigrationPreservesExistingData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 1): %v", err)
	}
	item := mustCreateMediaItem(t, s1)
	user := &store.User{Email: "requester@example.com", PasswordHash: "hash"}
	if err := s1.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sqlDB, _ := s1.db.DB()
	if _, err := sqlDB.Exec(`DROP TABLE media_requests`); err != nil {
		t.Fatalf("dropping media_requests: %v", err)
	}
	if _, err := sqlDB.Exec(`DROP TABLE media_activity`); err != nil {
		t.Fatalf("dropping media_activity: %v", err)
	}
	if _, err := sqlDB.Exec(`ALTER TABLE media_items DROP COLUMN deletion_pending`); err != nil {
		t.Fatalf("dropping deletion_pending: %v", err)
	}
	if _, err := sqlDB.Exec(`DROP INDEX idx_watched_user_source_type_ext`); err != nil {
		t.Fatalf("dropping watched media-type index: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE UNIQUE INDEX idx_watched_user_source_ext ON watched_items(user_id, source, external_id)`); err != nil {
		t.Fatalf("restoring version 7 watched index: %v", err)
	}
	if _, err := sqlDB.Exec(`UPDATE schema_migrations SET version = 7, dirty = 0`); err != nil {
		t.Fatalf("resetting migration version: %v", err)
	}
	_ = s1.Close()

	s2, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 2): %v", err)
	}
	defer s2.Close()
	if _, err := s2.GetMediaItem(item.ID); err != nil {
		t.Fatalf("GetMediaItem after migration: %v", err)
	}
	if _, err := s2.GetUser(user.ID); err != nil {
		t.Fatalf("GetUser after migration: %v", err)
	}
	if err := s2.CreateMediaRequest(&store.MediaRequest{
		MediaItemID: item.ID,
		UserID:      &user.ID,
		Scope:       "media",
	}); err != nil {
		t.Fatalf("CreateMediaRequest after migration: %v", err)
	}
}

func TestRequestIntentScopesMigrationPreservesVersionEightRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 1): %v", err)
	}
	item := mustCreateMediaItem(t, s1)
	user := &store.User{Email: "requester-v8@example.com", PasswordHash: "hash"}
	if err := s1.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	deletedUser := &store.User{Email: "deleted-v8@example.com", PasswordHash: "hash"}
	if err := s1.CreateUser(deletedUser); err != nil {
		t.Fatalf("CreateUser deleted fixture: %v", err)
	}

	sqlDB, _ := s1.db.DB()
	if _, err := sqlDB.Exec(`DROP TABLE media_activity`); err != nil {
		t.Fatalf("dropping media_activity: %v", err)
	}
	if _, err := sqlDB.Exec(`ALTER TABLE media_items DROP COLUMN deletion_pending`); err != nil {
		t.Fatalf("dropping deletion_pending: %v", err)
	}
	if _, err := sqlDB.Exec(`DROP INDEX idx_watched_user_source_type_ext`); err != nil {
		t.Fatalf("dropping watched media-type index: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE UNIQUE INDEX idx_watched_user_source_ext ON watched_items(user_id, source, external_id)`); err != nil {
		t.Fatalf("restoring version 8 watched index: %v", err)
	}
	down, err := migrationsFS.ReadFile(migrationsDir + "/0009_request_intent_scopes.down.sql")
	if err != nil {
		t.Fatalf("reading 0009 down migration: %v", err)
	}
	if _, err := sqlDB.Exec(string(down)); err != nil {
		t.Fatalf("restoring version 8 request schema: %v", err)
	}
	if _, err := sqlDB.Exec(`UPDATE schema_migrations SET version = 8, dirty = 0`); err != nil {
		t.Fatalf("resetting migration version: %v", err)
	}
	requestedAt := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	season, episode := 2, 4
	for _, request := range []*store.MediaRequest{
		{MediaItemID: item.ID, UserID: &user.ID, Scope: store.MediaRequestScopeMedia, RequestedAt: requestedAt},
		{MediaItemID: item.ID, UserID: &user.ID, Scope: store.MediaRequestScopeSeason, SeasonNumber: &season, RequestedAt: requestedAt},
		{MediaItemID: item.ID, UserID: &deletedUser.ID, Scope: store.MediaRequestScopeEpisode, SeasonNumber: &season, EpisodeNumber: &episode, RequestedAt: requestedAt},
	} {
		if err := s1.CreateMediaRequest(request); err != nil {
			t.Fatalf("seeding version 8 request: %v", err)
		}
	}
	if err := s1.DeleteUser(deletedUser.ID); err != nil {
		t.Fatalf("deleting request user: %v", err)
	}
	before, err := s1.ListMediaRequestsByMediaItem(item.ID)
	if err != nil {
		t.Fatalf("listing version 8 requests: %v", err)
	}
	_ = s1.Close()

	s2, err := New(path)
	if err != nil {
		t.Fatalf("New (boot 2): %v", err)
	}
	defer s2.Close()
	requests, err := s2.ListMediaRequestsByMediaItem(item.ID)
	if err != nil || len(requests) != len(before) {
		t.Fatalf("version 8 requests after migration = %+v, %v", requests, err)
	}
	for i := range before {
		if requests[i].ID != before[i].ID || requests[i].Scope != before[i].Scope ||
			!equalOptionalInt(requests[i].SeasonNumber, before[i].SeasonNumber) ||
			!equalOptionalInt(requests[i].EpisodeNumber, before[i].EpisodeNumber) ||
			!equalOptionalUint(requests[i].UserID, before[i].UserID) ||
			!requests[i].RequestedAt.Equal(before[i].RequestedAt) {
			t.Errorf("request %d changed: before=%+v after=%+v", i, before[i], requests[i])
		}
	}
	if err := s2.CreateMediaRequest(&store.MediaRequest{
		MediaItemID: item.ID,
		UserID:      &user.ID,
		Scope:       store.MediaRequestScopeFutureSeasons,
	}); err != nil {
		t.Fatalf("creating future-season request after migration: %v", err)
	}
}

func equalOptionalInt(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func equalOptionalUint(left, right *uint) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
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
	if !hasColumn(t, db, table, column) {
		t.Errorf("column %s.%s missing from fresh-install schema", table, column)
	}
}

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
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
			return true
		}
	}
	return false
}

package sqlite

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/sumia01/media-gate/internal/store"
)

func TestForceReleaseMigrationDefaultsAndPreservesSettings(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	if !item.ForceRelease {
		t.Fatal("new media item should default to force mode")
	}
	item.PreferredRelease, item.ForceRelease = "ETHEL, FLUX", false
	if err := s.UpdateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	before := *item
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
	if err := m.Migrate(11); err != nil {
		t.Fatalf("restoring version 11 schema: %v", err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("rolling back force-release migration: %v", err)
	}
	if hasColumn(t, sqlDB, "media_items", "force_release") {
		t.Fatal("rollback retained force_release")
	}
	if version, dirty, err := driver.Version(); err != nil || version != 10 || dirty {
		t.Fatalf("rollback version = %d dirty=%v err=%v", version, dirty, err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("upgrading from version 10: %v", err)
	}
	got, err := s.GetMediaItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ForceRelease || got.PreferredRelease != before.PreferredRelease || got.Title != before.Title ||
		!got.UpdatedAt.Equal(before.UpdatedAt) || !got.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("migration changed settings or failed to default on: %+v", got)
	}
	if version, dirty, err := driver.Version(); err != nil || version != latestMigrationVersion || dirty {
		t.Fatalf("upgrade version = %d dirty=%v err=%v", version, dirty, err)
	}
	// Verify the database default and constraints independently of GORM defaults.
	if _, err := sqlDB.Exec(`INSERT INTO media_items (library_id, title, media_type) VALUES (?, 'Raw default', 'movie')`, item.LibraryID); err != nil {
		t.Fatal(err)
	}
	var forced bool
	if err := sqlDB.QueryRow(`SELECT force_release FROM media_items WHERE title = 'Raw default'`).Scan(&forced); err != nil || !forced {
		t.Fatalf("SQL insert default = %v, %v", forced, err)
	}
	for _, invalid := range []any{nil, 2} {
		if _, err := sqlDB.Exec(`UPDATE media_items SET force_release = ? WHERE id = ?`, invalid, item.ID); err == nil {
			t.Fatalf("invalid force_release %v accepted", invalid)
		}
	}
	var activities int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM media_activity`).Scan(&activities); err != nil || activities != 0 {
		t.Fatalf("migration invented activity: count=%d err=%v", activities, err)
	}
	// Both true and false remain updateable after the upgrade.
	got.ForceRelease = false
	if err := s.UpdateMediaItem(got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMediaItem(item.ID)
	if err != nil || got.ForceRelease {
		t.Fatalf("explicit false did not persist: %+v, %v", got, err)
	}
	if _, err := s.GetLibrary(item.LibraryID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMediaItem(&store.MediaItem{LibraryID: item.LibraryID + 1000, Title: "Invalid parent", MediaType: "movie"}); err == nil {
		t.Fatal("migration disabled foreign-key enforcement")
	}
}

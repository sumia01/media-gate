package sqlite

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/sumia01/media-gate/internal/store"
)

func TestMonitorDecisionPersistenceOverwriteAndCascade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	item := mustCreateMediaItem(t, s)
	if _, err := s.GetMonitorDecision(item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing decision error = %v", err)
	}
	sn, en, downloadID := 1, 2, uint(42)
	inputUpdatedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	first := &store.MonitorDecision{
		MediaItemID: item.ID, CheckedAt: time.Now().UTC().Truncate(time.Second),
		InputUpdatedAt: &inputUpdatedAt,
		Outcome:        "grabbed", Summary: "1 download queued.", Truncated: true,
		Details: []store.MonitorDecisionDetail{{
			SeasonNumber: &sn, EpisodeNumber: &en, DownloadID: &downloadID,
			Outcome: "grabbed", Explanation: "Queued.", SelectedTitle: "Show.S01E02.1080p",
			TotalResults: 10, RejectedResults: 3, BlockedResults: 1,
		}},
	}
	if err := s.UpsertMonitorDecision(first); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetMonitorDecision(item.ID)
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Fatalf("reopened decision = %+v, %v; want %+v", got, err, first)
	}
	second := &store.MonitorDecision{
		MediaItemID: item.ID, CheckedAt: first.CheckedAt.Add(time.Minute),
		Outcome: "no_results", Summary: "No results returned.",
	}
	nextInput := inputUpdatedAt.Add(time.Minute)
	for _, input := range []*time.Time{&nextInput, nil} {
		second.InputUpdatedAt = input
		if err := s.UpsertMonitorDecision(second); err != nil {
			t.Fatal(err)
		}
		got, err = s.GetMonitorDecision(item.ID)
		if err != nil || !reflect.DeepEqual(got, second) || got.Truncated || len(got.Details) != 0 {
			t.Fatalf("overwrite = %+v, %v; want %+v", got, err, second)
		}
	}
	var count int64
	if err := s.db.Model(&store.MonitorDecision{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("snapshot count = %d, %v", count, err)
	}
	if err := s.DeleteMediaItem(item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMonitorDecision(item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("decision survived cascade: %v", err)
	}
	if err := s.UpsertMonitorDecision(second); err == nil {
		t.Fatal("snapshot insert without media item should fail foreign key constraint")
	}
}

func TestMonitorDecisionMigrationFreshAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "fresh"
		if legacy {
			name = "legacy adoption"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "migration.db")
			if legacy {
				raw := openRawForTest(t, path)
				baseline, err := migrationsFS.ReadFile(migrationsDir + "/0001_baseline.up.sql")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := raw.Exec(string(baseline)); err != nil {
					t.Fatal(err)
				}
				if _, err := raw.Exec(`INSERT INTO settings (key, value) VALUES ('schema_version', '9');
					INSERT INTO libraries (id, name, path, media_type) VALUES (1, 'Existing', '/existing', 'series');
					INSERT INTO media_items (id, library_id, title, media_type) VALUES (1, 1, 'Existing show', 'series')`); err != nil {
					t.Fatal(err)
				}
				_ = raw.Close()
			}
			s, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			sqlDB, _ := s.db.DB()
			for _, column := range []string{"media_item_id", "checked_at", "input_updated_at", "outcome", "summary", "details", "truncated"} {
				mustHaveColumn(t, sqlDB, "monitor_decisions", column)
			}
			var version int
			if err := sqlDB.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil || version != latestMigrationVersion {
				t.Fatalf("migration version = %d, %v; want %d", version, err, latestMigrationVersion)
			}
			if legacy {
				item, err := s.GetMediaItem(1)
				if err != nil || item.Title != "Existing show" {
					t.Fatalf("legacy data changed: %+v, %v", item, err)
				}
				if _, err := s.GetMonitorDecision(item.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("legacy check was fabricated: %v", err)
				}
			}
			for _, direction := range []string{"down", "up"} {
				script, err := migrationsFS.ReadFile(migrationsDir + "/0005_monitor_decisions." + direction + ".sql")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := sqlDB.Exec(string(script)); err != nil {
					t.Fatal(err)
				}
				if tableExists(sqlDB, "monitor_decisions") != (direction == "up") {
					t.Fatalf("migration %s did not change table", direction)
				}
			}
		})
	}
}

func TestMonitorDecisionInputMigrationFromVersion6(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version6.db")
	raw := openRawForTest(t, path)
	driver, err := newGlebarezDriver(raw)
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
	if err := m.Migrate(6); err != nil {
		t.Fatal(err)
	}
	checked := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if _, err := raw.Exec(`
		INSERT INTO libraries (id, name, path, media_type) VALUES (1, 'Existing', '/existing', 'series');
		INSERT INTO media_items (id, library_id, title, media_type) VALUES (1, 1, 'Existing show', 'series');
		INSERT INTO monitor_decisions (media_item_id, checked_at, outcome, summary, details, truncated)
		VALUES (1, ?, 'no_results', 'No results.', '[]', 1)`, checked); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sqlDB, _ := s.db.DB()
	var version int
	if err := sqlDB.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil || version != latestMigrationVersion {
		t.Fatalf("migration version = %d, %v; want %d", version, err, latestMigrationVersion)
	}
	want := &store.MonitorDecision{
		MediaItemID: 1, CheckedAt: checked, Outcome: "no_results", Summary: "No results.",
		Details: []store.MonitorDecisionDetail{}, Truncated: true,
	}
	got, err := s.GetMonitorDecision(1)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("version6 snapshot changed or freshness fabricated: %+v, %v", got, err)
	}
	input := checked.Add(-time.Hour)
	got.InputUpdatedAt = &input
	if err := s.UpsertMonitorDecision(got); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"down", "up"} {
		script, err := migrationsFS.ReadFile(migrationsDir + "/0007_monitor_decision_inputs." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sqlDB.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetMonitorDecision(1)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("migration %s lost snapshot data or invented freshness: %+v, %v", direction, got, err)
		}
	}
	// The upgraded row can acquire a known input version on its next real check.
	want.InputUpdatedAt = &input
	if err := s.UpsertMonitorDecision(want); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMonitorDecision(1)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("next check did not persist input freshness: %+v, %v", got, err)
	}
}

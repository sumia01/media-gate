package sqlite

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/sumia01/media-gate/internal/store"
)

func TestReleaseKeywordsAreCanonicalPrivateBoundedAndLiteral(t *testing.T) {
	s := newTestStore(t)
	a := &store.User{Email: "a@example.test", PasswordHash: "hash"}
	b := &store.User{Email: "b@example.test", PasswordHash: "hash"}
	for _, user := range []*store.User{a, b} {
		if err := s.CreateUser(user); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RememberReleaseKeywords(a.ID, []string{" Ethel ", "ETHEL", "EtHeL", "Flux", "", "%team", "_team", `\team`, strings.Repeat("x", 257)}); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberReleaseKeywords(b.ID, []string{"Other"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user   uint
		prefix string
		want   []string
	}{
		{a.ID, "ET", []string{"ethel"}},
		{a.ID, " eTh ", []string{"ethel"}},
		{a.ID, "Fl", []string{"flux"}},
		{a.ID, "%", []string{"%team"}},
		{a.ID, "_", []string{"_team"}},
		{a.ID, `\`, []string{`\team`}},
		{a.ID, "x", []string{"flux"}},
		{a.ID, " ", []string{}},
		{b.ID, "ET", []string{}},
	} {
		got, err := s.ListReleaseKeywords(tc.user, tc.prefix)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("user %d prefix %q: got %v, %v; want %v", tc.user, tc.prefix, got, err, tc.want)
		}
	}
	var many []string
	for i := 0; i < 20; i++ {
		many = append(many, fmt.Sprintf("team%02d", i))
	}
	if err := s.RememberReleaseKeywords(a.ID, many); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListReleaseKeywords(a.ID, "TEAM")
	if err != nil || !reflect.DeepEqual(got, many[:store.MaxReleaseKeywordSuggestions]) {
		t.Fatalf("bounded ordered suggestions = %v, %v", got, err)
	}
	if err := s.RememberReleaseKeywords(a.ID+1000, []string{"orphan"}); err == nil {
		t.Fatal("orphan history accepted")
	}
	sqlDB, _ := s.db.DB()
	for _, keyword := range []string{"", strings.Repeat("x", 257), "ETHEL"} {
		if _, err := sqlDB.Exec(`INSERT INTO release_keywords (user_id, keyword) VALUES (?, ?)`, a.ID, keyword); err == nil {
			t.Fatalf("constraint accepted invalid/duplicate keyword %q", keyword)
		}
	}
	if err := s.DeleteUser(a.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListReleaseKeywords(a.ID, "e"); err != nil || len(got) != 0 {
		t.Fatalf("user deletion retained history: %v, %v", got, err)
	}
}

func TestReleaseKeywordsFuzzySearchBeforeLimiting(t *testing.T) {
	s := newTestStore(t)
	user := &store.User{Email: "fuzzy@example.test", PasswordHash: "hash"}
	other := &store.User{Email: "private@example.test", PasswordHash: "hash"}
	for _, actor := range []*store.User{user, other} {
		if err := s.CreateUser(actor); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"ethel", "flux"}
	for i := 0; i < 30; i++ {
		names = append(names, fmt.Sprintf("aaa%02d", i))
	}
	if err := s.RememberReleaseKeywords(user.ID, names); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ query, want string }{
		{"thel", "ethel"}, {"ETL", "ethel"}, {"ehtel", "ethel"}, {"flx", "flux"}, {"fluz", "flux"},
	} {
		got, err := s.ListReleaseKeywords(user.ID, tc.query)
		if err != nil || len(got) == 0 || got[0] != tc.want {
			t.Fatalf("query %q = %v, %v; want %s first", tc.query, got, err, tc.want)
		}
		if got, err := s.ListReleaseKeywords(other.ID, tc.query); err != nil || len(got) != 0 {
			t.Fatalf("fuzzy search leaked private vocabulary: %v, %v", got, err)
		}
	}
}

func TestReleaseKeywordsRollbackAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := &store.User{Email: "restart@example.test", PasswordHash: "hash"}
	if err := s.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	abort := errors.New("abort")
	err = s.WithTx(func(tx store.Store) error {
		if err := tx.RememberReleaseKeywords(user.ID, []string{"ETHEL"}); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if got, err := s.ListReleaseKeywords(user.ID, "e"); err != nil || len(got) != 0 {
		t.Fatalf("rolled back history remained: %v, %v", got, err)
	}
	if err := s.RememberReleaseKeywords(user.ID, []string{"ETHEL"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.ListReleaseKeywords(user.ID, "E"); err != nil || !reflect.DeepEqual(got, []string{"ethel"}) {
		t.Fatalf("history lost on restart: %v, %v", got, err)
	}
}

func TestReleaseKeywordsMigrationRollbackAndUpgrade(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	item.PreferredRelease = "ETHEL"
	if err := s.UpdateMediaItem(item); err != nil {
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
	if err := m.Migrate(11); err != nil {
		t.Fatal(err)
	}
	if tableExists(sqlDB, "release_keywords") {
		t.Fatal("downgrade retained history table")
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMediaItem(item.ID)
	if err != nil || got.PreferredRelease != "ETHEL" || !got.ForceRelease || !got.UpdatedAt.Equal(item.UpdatedAt) {
		t.Fatalf("upgrade changed existing settings: %+v, %v", got, err)
	}
	var count int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM release_keywords`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("upgrade fabricated user attribution: %d, %v", count, err)
	}
	if version, dirty, err := driver.Version(); err != nil || version != 12 || dirty {
		t.Fatalf("upgrade version = %d dirty=%v err=%v", version, dirty, err)
	}
}

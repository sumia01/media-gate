package watched

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

type fixture struct {
	store   store.Store
	service *Service
	users   [2]*store.User
	library *store.Library
	media   *store.MediaItem
	pub     *recordingPublisher
}

type recordingPublisher struct {
	events []eventbus.Event
}

func (p *recordingPublisher) Publish(eventType eventbus.EventType, payload any) {
	p.events = append(p.events, eventbus.Event{Type: eventType, Payload: payload})
}

type appendFailStore struct {
	store.Store
	err error
}

type modeOverrideStore struct {
	store.Store
	value string
	err   error
}

func (s *appendFailStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&appendFailStore{Store: tx, err: s.err})
	})
}

func (s *appendFailStore) AppendMediaActivity(*store.MediaActivity) error {
	return s.err
}

func (s *modeOverrideStore) GetSetting(string) (*store.Setting, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &store.Setting{Key: settings.KeyWatchedListMode, Value: s.value}, nil
}

func (s *modeOverrideStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		return fn(&modeOverrideStore{Store: tx, value: s.value, err: s.err})
	})
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "watched.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	f := &fixture{store: st, pub: &recordingPublisher{}}
	for i, email := range []string{"one@example.com", "two@example.com"} {
		f.users[i] = &store.User{Email: email, PasswordHash: "hash", FirstName: email[:3]}
		if err := st.CreateUser(f.users[i]); err != nil {
			t.Fatal(err)
		}
	}
	f.library = &store.Library{Name: "Movies", Path: t.TempDir(), MediaType: "movie"}
	if err := st.CreateLibrary(f.library); err != nil {
		t.Fatal(err)
	}
	f.media = createMedia(t, st, f.library.ID, "Current Library Title", "movie", "tmdb", 100)
	f.service = NewService(st, f.pub)
	return f
}

func createMedia(t *testing.T, st store.Store, libraryID uint, title, mediaType, source string, externalID int) *store.MediaItem {
	t.Helper()
	item := &store.MediaItem{LibraryID: libraryID, Title: title, MediaType: mediaType, Status: "available", Source: "disk"}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaMetadata(&store.MediaMetadata{
		MediaItemID: item.ID, Source: source, ExternalID: externalID, Title: title,
	}); err != nil {
		t.Fatal(err)
	}
	return item
}

func watchedCandidate(source, mediaType string, externalID int) *store.WatchedItem {
	return &store.WatchedItem{Source: source, MediaType: mediaType, ExternalID: externalID, Title: "Provider Title"}
}

func setMode(t *testing.T, st store.Store, mode string) {
	t.Helper()
	if err := st.SetSetting(&store.Setting{Key: settings.KeyWatchedListMode, Value: mode}); err != nil {
		t.Fatal(err)
	}
}

func activities(t *testing.T, st store.Store, mediaItemID, viewerUserID uint) []store.MediaActivityAttribution {
	t.Helper()
	rows, hasMore, err := st.ListMediaActivityPage(mediaItemID, viewerUserID, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore {
		t.Fatal("unexpected additional activity page")
	}
	return rows
}

func TestGlobalWatchedActivityIsSharedAndAnyUserCanDelete(t *testing.T) {
	f := newFixture(t)
	created, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100))
	if err != nil {
		t.Fatal(err)
	}
	if created.MediaItemID == nil || *created.MediaItemID != f.media.ID {
		t.Fatalf("automatic link = %v, want %d", created.MediaItemID, f.media.ID)
	}

	rows := activities(t, f.store, f.media.ID, f.users[1].ID)
	if len(rows) != 1 || rows[0].Action != store.MediaActivityActionWatchedMarked || rows[0].Visibility != store.MediaActivityVisibilityShared {
		t.Fatalf("shared marked activity = %+v", rows)
	}
	if rows[0].MediaTitle != f.media.Title || rows[0].ActorUserID == nil || *rows[0].ActorUserID != f.users[0].ID {
		t.Fatalf("marked attribution = %+v", rows[0])
	}
	var details store.MediaActivityDetails
	if err := json.Unmarshal([]byte(rows[0].Details), &details); err != nil {
		t.Fatal(err)
	}
	if details.Target == nil || details.Target.Scope != store.MediaActivityScopeMedia {
		t.Fatalf("marked target = %+v", details.Target)
	}

	if err := f.service.Delete(f.users[1].ID, created.ID); err != nil {
		t.Fatalf("global non-creator delete: %v", err)
	}
	if _, err := f.store.GetWatchedItem(created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted watched item error = %v", err)
	}
	rows = activities(t, f.store, f.media.ID, f.users[0].ID)
	if len(rows) != 2 || rows[0].Action != store.MediaActivityActionWatchedUnmarked || rows[0].Visibility != store.MediaActivityVisibilityShared || rows[0].ActorUserID == nil || *rows[0].ActorUserID != f.users[1].ID {
		t.Fatalf("shared unmarked activity = %+v", rows)
	}
	if len(f.pub.events) != 2 {
		t.Fatalf("shared activity events = %d, want 2", len(f.pub.events))
	}
	for _, event := range f.pub.events {
		payload, ok := event.Payload.(eventbus.MediaActivityPayload)
		if event.Type != eventbus.MediaActivityAdded || !ok || payload.MediaItemID != f.media.ID {
			t.Fatalf("shared activity event = %+v", event)
		}
	}
}

func TestWatchedDuplicateScopeIsAtomicAndModeSpecific(t *testing.T) {
	f := newFixture(t)
	errs := make(chan error, 2)
	for _, user := range f.users {
		go func(userID uint) {
			_, err := f.service.Create(userID, watchedCandidate("tmdb", "movie", 100))
			errs <- err
		}(user.ID)
	}
	var succeeded, duplicated int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, store.ErrDuplicate):
			duplicated++
		default:
			t.Fatalf("concurrent global create error = %v", err)
		}
	}
	if succeeded != 1 || duplicated != 1 {
		t.Fatalf("concurrent global creates: succeeded=%d duplicated=%d", succeeded, duplicated)
	}
	items, err := f.store.ListWatchedItems()
	if err != nil || len(items) != 1 {
		t.Fatalf("global watched rows = %+v, %v", items, err)
	}
	if err := f.service.Delete(f.users[0].ID, items[0].ID); err != nil {
		t.Fatal(err)
	}

	setMode(t, f.store, "per_user")
	for _, user := range f.users {
		if _, err := f.service.Create(user.ID, watchedCandidate("tmdb", "movie", 100)); err != nil {
			t.Fatalf("per-user create for %d: %v", user.ID, err)
		}
	}
	items, err = f.store.ListWatchedItems()
	if err != nil || len(items) != 2 {
		t.Fatalf("per-user watched rows = %+v, %v", items, err)
	}
}

func TestGlobalDeleteAfterPerUserModeRemovesEveryExactDuplicate(t *testing.T) {
	f := newFixture(t)
	setMode(t, f.store, modePerUser)
	created := make([]*store.WatchedItem, 0, len(f.users))
	for _, user := range f.users {
		item, err := f.service.Create(user.ID, watchedCandidate("tmdb", "movie", 100))
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, item)
	}
	if len(f.pub.events) != 0 {
		t.Fatalf("private marks published %d global events", len(f.pub.events))
	}

	setMode(t, f.store, modeGlobal)
	if err := f.service.Delete(f.users[1].ID, created[0].ID); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ListWatchedItems()
	if err != nil || len(items) != 0 {
		t.Fatalf("global unmark left duplicate rows: %+v, %v", items, err)
	}
	for _, user := range f.users {
		rows := activities(t, f.store, f.media.ID, user.ID)
		var sharedUnmarks int
		for _, row := range rows {
			if row.Action == store.MediaActivityActionWatchedUnmarked && row.Visibility == store.MediaActivityVisibilityShared {
				sharedUnmarks++
			}
		}
		if sharedUnmarks != 1 {
			t.Fatalf("user %d sees %d shared unmarks in %+v", user.ID, sharedUnmarks, rows)
		}
	}
	if len(f.pub.events) != 1 {
		t.Fatalf("global unmark published %d events, want 1", len(f.pub.events))
	}
}

func TestPerUserWatchedActivityIsPrivateAndOwnerOnly(t *testing.T) {
	f := newFixture(t)
	setMode(t, f.store, "per_user")
	created, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100))
	if err != nil {
		t.Fatal(err)
	}
	if rows := activities(t, f.store, f.media.ID, f.users[1].ID); len(rows) != 0 {
		t.Fatalf("other user saw private activity: %+v", rows)
	}
	if rows := activities(t, f.store, f.media.ID, f.users[0].ID); len(rows) != 1 || rows[0].Visibility != store.MediaActivityVisibilityActorOnly {
		t.Fatalf("owner private activity = %+v", rows)
	}
	if err := f.service.Delete(f.users[1].ID, created.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-owner delete error = %v, want ErrForbidden", err)
	}
	if _, err := f.store.GetWatchedItem(created.ID); err != nil {
		t.Fatalf("non-owner deleted row: %v", err)
	}
	if err := f.service.Delete(f.users[0].ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if rows := activities(t, f.store, f.media.ID, f.users[0].ID); len(rows) != 2 || rows[0].Visibility != store.MediaActivityVisibilityActorOnly {
		t.Fatalf("owner private unmark activity = %+v", rows)
	}
	if len(f.pub.events) != 0 {
		t.Fatalf("private activity published %d global events", len(f.pub.events))
	}
}

func TestWatchedActivityUsesActionTimeMode(t *testing.T) {
	f := newFixture(t)
	created, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100))
	if err != nil {
		t.Fatal(err)
	}
	setMode(t, f.store, "per_user")
	if err := f.service.Delete(f.users[0].ID, created.ID); err != nil {
		t.Fatal(err)
	}

	ownerRows := activities(t, f.store, f.media.ID, f.users[0].ID)
	if len(ownerRows) != 2 || ownerRows[0].Visibility != store.MediaActivityVisibilityActorOnly || ownerRows[1].Visibility != store.MediaActivityVisibilityShared {
		t.Fatalf("action-time visibility = %+v", ownerRows)
	}
	otherRows := activities(t, f.store, f.media.ID, f.users[1].ID)
	if len(otherRows) != 1 || otherRows[0].Action != store.MediaActivityActionWatchedMarked {
		t.Fatalf("other-user action-time visibility = %+v", otherRows)
	}
	if len(f.pub.events) != 1 {
		t.Fatalf("published action-time events = %d, want 1", len(f.pub.events))
	}
}

func TestSuppliedMediaItemMustMatchCurrentExactIdentity(t *testing.T) {
	f := newFixture(t)
	missingID := f.media.ID + 1000
	tests := []struct {
		name      string
		candidate *store.WatchedItem
	}{
		{name: "missing", candidate: func() *store.WatchedItem {
			item := watchedCandidate("tmdb", "movie", 100)
			item.MediaItemID = &missingID
			return item
		}()},
		{name: "source", candidate: func() *store.WatchedItem {
			item := watchedCandidate("tvdb", "movie", 100)
			item.MediaItemID = &f.media.ID
			return item
		}()},
		{name: "external id", candidate: func() *store.WatchedItem {
			item := watchedCandidate("tmdb", "movie", 101)
			item.MediaItemID = &f.media.ID
			return item
		}()},
		{name: "media type", candidate: func() *store.WatchedItem {
			item := watchedCandidate("tmdb", "series", 100)
			item.MediaItemID = &f.media.ID
			return item
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := f.service.Create(f.users[0].ID, tt.candidate); !errors.Is(err, ErrMediaItemMismatch) {
				t.Fatalf("error = %v, want ErrMediaItemMismatch", err)
			}
		})
	}
	items, err := f.store.ListWatchedItems()
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid links persisted watched rows: %+v, %v", items, err)
	}
	if rows := activities(t, f.store, f.media.ID, f.users[0].ID); len(rows) != 0 {
		t.Fatalf("invalid links appended activity: %+v", rows)
	}
}

func TestAutomaticLinkingRequiresOneExactMetadataMatch(t *testing.T) {
	f := newFixture(t)
	second := createMedia(t, f.store, f.library.ID, "Ambiguous One", "movie", "tmdb", 200)
	third := createMedia(t, f.store, f.library.ID, "Ambiguous Two", "movie", "tmdb", 200)

	ambiguous, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 200))
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.MediaItemID != nil {
		t.Fatalf("ambiguous row linked to %d", *ambiguous.MediaItemID)
	}
	unlinked, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 999))
	if err != nil {
		t.Fatal(err)
	}
	if unlinked.MediaItemID != nil {
		t.Fatalf("unknown row linked to %d", *unlinked.MediaItemID)
	}
	for _, item := range []*store.MediaItem{second, third} {
		if rows := activities(t, f.store, item.ID, f.users[0].ID); len(rows) != 0 {
			t.Fatalf("unlinked watched row added activity for media %d: %+v", item.ID, rows)
		}
	}
	if len(f.pub.events) != 0 {
		t.Fatalf("unlinked watched rows published %d events", len(f.pub.events))
	}
}

func TestDeleteDoesNotAttributeStaleOrUnmatchedLinks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stale func(t *testing.T, f *fixture)
	}{
		{
			name: "rematched",
			stale: func(t *testing.T, f *fixture) {
				metadata, err := f.store.GetMediaMetadataByMediaItem(f.media.ID)
				if err != nil {
					t.Fatal(err)
				}
				metadata.ExternalID = 101
				if err := f.store.UpdateMediaMetadata(metadata); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unmatched",
			stale: func(t *testing.T, f *fixture) {
				if err := f.store.DeleteMediaMetadataByMediaItem(f.media.ID); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			created, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100))
			if err != nil {
				t.Fatal(err)
			}
			tc.stale(t, f)
			if err := f.service.Delete(f.users[0].ID, created.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.GetWatchedItem(created.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("stale watched row was not deleted: %v", err)
			}
			rows := activities(t, f.store, f.media.ID, f.users[0].ID)
			if len(rows) != 1 || rows[0].Action != store.MediaActivityActionWatchedMarked {
				t.Fatalf("stale link appended an unmark: %+v", rows)
			}
			if len(f.pub.events) != 1 {
				t.Fatalf("stale link published %d events, want only the original mark", len(f.pub.events))
			}
		})
	}
}

func TestWatchedModeReadsFailClosed(t *testing.T) {
	f := newFixture(t)
	modeErr := errors.New("mode read failed")
	for _, tc := range []struct {
		name  string
		store store.Store
		want  error
	}{
		{name: "database error", store: &modeOverrideStore{Store: f.store, err: modeErr}, want: modeErr},
		{name: "unknown value", store: &modeOverrideStore{Store: f.store, value: "public"}, want: ErrInvalidMode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(tc.store, nil)
			if _, err := service.List(f.users[0].ID); !errors.Is(err, tc.want) {
				t.Fatalf("List error = %v, want %v", err, tc.want)
			}
			if _, err := service.Check(f.users[0].ID, "tmdb", "movie", 100); !errors.Is(err, tc.want) {
				t.Fatalf("Check error = %v, want %v", err, tc.want)
			}
			if _, err := service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100)); !errors.Is(err, tc.want) {
				t.Fatalf("Create error = %v, want %v", err, tc.want)
			}
		})
	}
	if items, err := f.store.ListWatchedItems(); err != nil || len(items) != 0 {
		t.Fatalf("failed mode reads exposed or changed watched state: %+v, %v", items, err)
	}
}

func TestWatchedIdentityValidation(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name       string
		source     string
		mediaType  string
		externalID int
	}{
		{name: "source", source: "imdb", mediaType: "movie", externalID: 1},
		{name: "media type", source: "tmdb", mediaType: "episode", externalID: 1},
		{name: "zero external id", source: "tmdb", mediaType: "movie", externalID: 0},
		{name: "negative external id", source: "tvdb", mediaType: "series", externalID: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := watchedCandidate(tc.source, tc.mediaType, tc.externalID)
			if _, err := f.service.Create(f.users[0].ID, candidate); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("Create error = %v, want ErrInvalidIdentity", err)
			}
			if _, err := f.service.Check(f.users[0].ID, tc.source, tc.mediaType, tc.externalID); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("Check error = %v, want ErrInvalidIdentity", err)
			}
		})
	}
}

func TestActivityAppendFailureRollsBackWatchedMutation(t *testing.T) {
	f := newFixture(t)
	appendErr := errors.New("append failed")
	failing := NewService(&appendFailStore{Store: f.store, err: appendErr}, f.pub)
	if _, err := failing.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100)); !errors.Is(err, appendErr) {
		t.Fatalf("create error = %v, want append failure", err)
	}
	if _, err := f.store.GetWatchedBySourceExternal(nil, "tmdb", "movie", 100); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("create rollback lookup error = %v", err)
	}

	created, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := failing.Delete(f.users[0].ID, created.ID); !errors.Is(err, appendErr) {
		t.Fatalf("delete error = %v, want append failure", err)
	}
	if _, err := f.store.GetWatchedItem(created.ID); err != nil {
		t.Fatalf("delete append failure did not roll back row: %v", err)
	}
	rows := activities(t, f.store, f.media.ID, f.users[0].ID)
	if len(rows) != 1 || rows[0].Action != store.MediaActivityActionWatchedMarked {
		t.Fatalf("activity after append rollbacks = %+v", rows)
	}
}

func TestDeletedActorHidesPrivateActivityAndAnonymizesSharedActivity(t *testing.T) {
	f := newFixture(t)
	created, err := f.service.Create(f.users[0].ID, watchedCandidate("tmdb", "movie", 100))
	if err != nil {
		t.Fatal(err)
	}
	setMode(t, f.store, "per_user")
	if err := f.service.Delete(f.users[0].ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteUser(f.users[0].ID); err != nil {
		t.Fatal(err)
	}

	rows := activities(t, f.store, f.media.ID, f.users[1].ID)
	if len(rows) != 1 || rows[0].Visibility != store.MediaActivityVisibilityShared || rows[0].ActorUserID != nil || rows[0].FirstName != "" || rows[0].Email != "" {
		t.Fatalf("activity after actor deletion = %+v", rows)
	}
	if rows := activities(t, f.store, f.media.ID, f.users[0].ID); len(rows) != 1 {
		t.Fatalf("deleted actor could still see private activity: %+v", rows)
	}
}

func TestWatchedMutationsRequireLiveActor(t *testing.T) {
	f := newFixture(t)
	actorID := f.users[0].ID
	if err := f.store.DeleteUser(actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Create(actorID, watchedCandidate("tmdb", "movie", 100)); !errors.Is(err, store.ErrActivityActorNotFound) {
		t.Fatalf("deleted actor create error = %v", err)
	}
	if items, err := f.store.ListWatchedItems(); err != nil || len(items) != 0 {
		t.Fatalf("deleted actor changed watched rows: %+v, %v", items, err)
	}
}

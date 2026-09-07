package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

func TestAppendMediaActivityValidatesActorCombinations(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := mustCreateActivityUser(t, s, "actor@example.com")

	for _, activity := range []*store.MediaActivity{
		newActivityFixture(item, user.ID, "valid-user", `{"reason":"manual"}`),
		newActivityFixture(item, user.ID, "valid-private-user", `{"reason":"manual"}`),
		newActivityFixture(item, user.ID, "valid-system", `{"reason":"automatic"}`),
	} {
		switch activity.OperationID {
		case "valid-private-user":
			activity.Visibility = store.MediaActivityVisibilityActorOnly
		case "valid-system":
			activity.ActorKind = store.MediaActivityActorSystem
			activity.ActorUserID = nil
			activity.ActorComponent = "monitor"
		}
		if err := s.AppendMediaActivity(activity); err != nil {
			t.Fatalf("AppendMediaActivity(%s): %v", activity.OperationID, err)
		}
	}

	zero := uint(0)
	missing := user.ID + 1000
	tests := []struct {
		name   string
		mutate func(*store.MediaActivity)
		want   error
	}{
		{"user without ID", func(a *store.MediaActivity) { a.ActorUserID = nil }, store.ErrInvalidMediaActivity},
		{"user with zero ID", func(a *store.MediaActivity) { a.ActorUserID = &zero }, store.ErrInvalidMediaActivity},
		{"user with component", func(a *store.MediaActivity) { a.ActorComponent = "api" }, store.ErrInvalidMediaActivity},
		{"system with user ID", func(a *store.MediaActivity) {
			a.ActorKind = store.MediaActivityActorSystem
			a.ActorComponent = "monitor"
		}, store.ErrInvalidMediaActivity},
		{"system without component", func(a *store.MediaActivity) {
			a.ActorKind = store.MediaActivityActorSystem
			a.ActorUserID = nil
		}, store.ErrInvalidMediaActivity},
		{"private system activity", func(a *store.MediaActivity) {
			a.ActorKind = store.MediaActivityActorSystem
			a.ActorUserID = nil
			a.ActorComponent = "monitor"
			a.Visibility = store.MediaActivityVisibilityActorOnly
		}, store.ErrInvalidMediaActivity},
		{"unknown actor kind", func(a *store.MediaActivity) { a.ActorKind = "robot" }, store.ErrInvalidMediaActivity},
		{"missing user", func(a *store.MediaActivity) { a.ActorUserID = &missing }, store.ErrActivityActorNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity := newActivityFixture(item, user.ID, tt.name, `{"reason":"test"}`)
			tt.mutate(activity)
			if err := s.AppendMediaActivity(activity); !errors.Is(err, tt.want) {
				t.Fatalf("AppendMediaActivity() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAppendMediaActivityEnforcesPayloadBounds(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := mustCreateActivityUser(t, s, "bounds@example.com")

	exactPayload := `{"reason":"` + strings.Repeat("x", store.MediaActivityMaxDetailsBytes-len(`{"reason":""}`)) + `"}`
	if len(exactPayload) != store.MediaActivityMaxDetailsBytes {
		t.Fatalf("test payload length = %d, want %d", len(exactPayload), store.MediaActivityMaxDetailsBytes)
	}
	if err := s.AppendMediaActivity(newActivityFixture(item, user.ID, "exact-32k", exactPayload)); err != nil {
		t.Fatalf("AppendMediaActivity at 32 KiB: %v", err)
	}
	tooLarge := exactPayload[:len(exactPayload)-2] + "x" + exactPayload[len(exactPayload)-2:]
	if err := s.AppendMediaActivity(newActivityFixture(item, user.ID, "over-32k", tooLarge)); !errors.Is(err, store.ErrInvalidMediaActivity) {
		t.Fatalf("AppendMediaActivity over 32 KiB = %v, want ErrInvalidMediaActivity", err)
	}

	atLimit := store.MediaActivityDetails{
		Targets:           make([]store.MediaActivityTarget, store.MediaActivityMaxDetails),
		MonitoringChanges: make([]store.MediaActivityMonitoringChange, store.MediaActivityMaxDetails),
		FieldChanges:      make([]store.MediaActivityFieldChange, store.MediaActivityMaxDetails),
	}
	for i := 0; i < store.MediaActivityMaxDetails; i++ {
		atLimit.Targets[i].Scope = store.MediaActivityScopeMedia
		atLimit.MonitoringChanges[i].Target.Scope = store.MediaActivityScopeMedia
		atLimit.FieldChanges[i].Field = "monitored"
	}
	if err := s.AppendMediaActivity(newActivityFixture(item, user.ID, "collections-at-limit", marshalActivityDetails(t, atLimit))); err != nil {
		t.Fatalf("AppendMediaActivity with 50 entries per collection: %v", err)
	}

	tests := []struct {
		name    string
		details store.MediaActivityDetails
	}{
		{"targets", store.MediaActivityDetails{Targets: make([]store.MediaActivityTarget, store.MediaActivityMaxDetails+1)}},
		{"monitoring changes", store.MediaActivityDetails{MonitoringChanges: make([]store.MediaActivityMonitoringChange, store.MediaActivityMaxDetails+1)}},
		{"field changes", store.MediaActivityDetails{FieldChanges: make([]store.MediaActivityFieldChange, store.MediaActivityMaxDetails+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity := newActivityFixture(item, user.ID, "too-many-"+tt.name, marshalActivityDetails(t, tt.details))
			if err := s.AppendMediaActivity(activity); !errors.Is(err, store.ErrInvalidMediaActivity) {
				t.Fatalf("AppendMediaActivity() error = %v, want ErrInvalidMediaActivity", err)
			}
		})
	}
}

func TestAppendMediaActivityAssignsServerUTCTimeAndRollsBack(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := mustCreateActivityUser(t, s, "timestamp@example.com")
	activity := newActivityFixture(item, user.ID, "server-time", `{"reason":"test"}`)
	activity.RecordedAt = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.FixedZone("client", 9*60*60))
	before := time.Now().UTC()
	if err := s.AppendMediaActivity(activity); err != nil {
		t.Fatalf("AppendMediaActivity: %v", err)
	}
	after := time.Now().UTC()
	if activity.RecordedAt.Location() != time.UTC || activity.RecordedAt.Before(before) || activity.RecordedAt.After(after) {
		t.Fatalf("RecordedAt = %v (%v), want server UTC time in [%v, %v]", activity.RecordedAt, activity.RecordedAt.Location(), before, after)
	}
	rows, _, err := s.ListMediaActivityPage(item.ID, user.ID, nil, 10)
	if err != nil || len(rows) != 1 || !rows[0].RecordedAt.Equal(activity.RecordedAt) {
		t.Fatalf("persisted server timestamp = %+v, %v; want %v", rows, err, activity.RecordedAt)
	}

	rollbackErr := errors.New("rollback activity")
	rolledBack := newActivityFixture(item, user.ID, "rolled-back", `{"reason":"test"}`)
	err = s.WithTx(func(tx store.Store) error {
		if err := tx.AppendMediaActivity(rolledBack); err != nil {
			return err
		}
		inside, _, err := tx.ListMediaActivityPage(item.ID, user.ID, nil, 10)
		if err != nil || len(inside) != 2 {
			return fmt.Errorf("activity not visible inside transaction: rows=%d err=%w", len(inside), err)
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("WithTx error = %v, want rollback marker", err)
	}
	rows, _, err = s.ListMediaActivityPage(item.ID, user.ID, nil, 10)
	if err != nil || len(rows) != 1 || rows[0].OperationID != "server-time" {
		t.Fatalf("activity after rollback = %+v, %v; want only committed row", rows, err)
	}
}

func TestListMediaActivityOrdersByDescendingIDWhenTimestampsTie(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := mustCreateActivityUser(t, s, "ordering@example.com")
	var inserted []uint
	for i := 1; i <= 4; i++ {
		activity := newActivityFixture(item, user.ID, fmt.Sprintf("ordered-%d", i), `{"reason":"test"}`)
		if err := s.AppendMediaActivity(activity); err != nil {
			t.Fatalf("AppendMediaActivity %d: %v", i, err)
		}
		inserted = append(inserted, activity.ID)
	}
	tiedAt := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	if err := s.db.Table("media_activity").Where("media_item_id = ?", item.ID).Update("recorded_at", tiedAt).Error; err != nil {
		t.Fatalf("tying timestamps: %v", err)
	}

	rows, hasMore, err := s.ListMediaActivityPage(item.ID, user.ID, nil, 10)
	if err != nil || hasMore {
		t.Fatalf("ListMediaActivityPage = hasMore %v, %v", hasMore, err)
	}
	want := []uint{inserted[3], inserted[2], inserted[1], inserted[0]}
	assertActivityIDs(t, rows, want)
	for _, row := range rows {
		if !row.RecordedAt.Equal(tiedAt) {
			t.Errorf("RecordedAt = %v, want tied timestamp %v", row.RecordedAt, tiedAt)
		}
	}
}

func TestListMediaActivityUsesExclusiveCursorAcrossConcurrentInserts(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := mustCreateActivityUser(t, s, "pagination@example.com")
	var inserted []uint
	for i := 1; i <= 5; i++ {
		activity := newActivityFixture(item, user.ID, fmt.Sprintf("page-%d", i), `{"reason":"test"}`)
		if err := s.AppendMediaActivity(activity); err != nil {
			t.Fatalf("AppendMediaActivity %d: %v", i, err)
		}
		inserted = append(inserted, activity.ID)
	}

	first, hasMore, err := s.ListMediaActivityPage(item.ID, user.ID, nil, 2)
	if err != nil || !hasMore {
		t.Fatalf("first page = %+v, hasMore %v, %v", first, hasMore, err)
	}
	assertActivityIDs(t, first, []uint{inserted[4], inserted[3]})

	newest := newActivityFixture(item, user.ID, "inserted-between-pages", `{"reason":"test"}`)
	if err := s.AppendMediaActivity(newest); err != nil {
		t.Fatalf("AppendMediaActivity between pages: %v", err)
	}
	before := first[len(first)-1].ID
	second, hasMore, err := s.ListMediaActivityPage(item.ID, user.ID, &before, 2)
	if err != nil || !hasMore {
		t.Fatalf("second page = %+v, hasMore %v, %v", second, hasMore, err)
	}
	assertActivityIDs(t, second, []uint{inserted[2], inserted[1]})
	before = second[len(second)-1].ID
	third, hasMore, err := s.ListMediaActivityPage(item.ID, user.ID, &before, 2)
	if err != nil || hasMore {
		t.Fatalf("third page = %+v, hasMore %v, %v", third, hasMore, err)
	}
	assertActivityIDs(t, third, []uint{inserted[0]})
}

func TestListMediaActivityFiltersVisibilityBeforeLimit(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	viewer := mustCreateActivityUser(t, s, "viewer@example.com")
	other := mustCreateActivityUser(t, s, "other@example.com")
	var shared []uint
	for i := 1; i <= 3; i++ {
		activity := newActivityFixture(item, other.ID, fmt.Sprintf("shared-%d", i), `{"reason":"test"}`)
		if err := s.AppendMediaActivity(activity); err != nil {
			t.Fatalf("AppendMediaActivity shared %d: %v", i, err)
		}
		shared = append(shared, activity.ID)
	}
	for i := 1; i <= 4; i++ {
		activity := newActivityFixture(item, other.ID, fmt.Sprintf("private-%d", i), `{"reason":"test"}`)
		activity.Visibility = store.MediaActivityVisibilityActorOnly
		if err := s.AppendMediaActivity(activity); err != nil {
			t.Fatalf("AppendMediaActivity private %d: %v", i, err)
		}
	}

	rows, hasMore, err := s.ListMediaActivityPage(item.ID, viewer.ID, nil, 2)
	if err != nil || !hasMore {
		t.Fatalf("visible page = %+v, hasMore %v, %v", rows, hasMore, err)
	}
	assertActivityIDs(t, rows, []uint{shared[2], shared[1]})
}

func TestMediaActivityUserAnonymizationAndMediaCascade(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	actor := mustCreateActivityUser(t, s, "deleted@example.com")
	actor.FirstName = "Delete"
	actor.LastName = "Me"
	if err := s.UpdateUser(actor); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	viewer := mustCreateActivityUser(t, s, "remaining@example.com")
	shared := newActivityFixture(item, actor.ID, "shared-before-delete", `{"reason":"test"}`)
	private := newActivityFixture(item, actor.ID, "private-before-delete", `{"reason":"test"}`)
	private.Visibility = store.MediaActivityVisibilityActorOnly
	for _, activity := range []*store.MediaActivity{shared, private} {
		if err := s.AppendMediaActivity(activity); err != nil {
			t.Fatalf("AppendMediaActivity(%s): %v", activity.OperationID, err)
		}
	}
	beforeDelete, _, err := s.ListMediaActivityPage(item.ID, actor.ID, nil, 10)
	if err != nil || len(beforeDelete) != 2 || beforeDelete[0].FirstName != "Delete" || beforeDelete[0].Email != actor.Email {
		t.Fatalf("actor attribution before deletion = %+v, %v", beforeDelete, err)
	}

	if err := s.DeleteUser(actor.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	rows, hasMore, err := s.ListMediaActivityPage(item.ID, viewer.ID, nil, 10)
	if err != nil || hasMore || len(rows) != 1 {
		t.Fatalf("activity after user deletion = %+v, hasMore %v, %v", rows, hasMore, err)
	}
	if rows[0].ID != shared.ID || rows[0].ActorUserID != nil || rows[0].FirstName != "" || rows[0].LastName != "" || rows[0].Email != "" {
		t.Fatalf("shared activity was not anonymized: %+v", rows[0])
	}
	var retained, anonymized int64
	if err := s.db.Table("media_activity").Where("media_item_id = ?", item.ID).Count(&retained).Error; err != nil {
		t.Fatalf("counting retained activity: %v", err)
	}
	if err := s.db.Table("media_activity").Where("media_item_id = ? AND actor_user_id IS NULL", item.ID).Count(&anonymized).Error; err != nil {
		t.Fatalf("counting anonymized activity: %v", err)
	}
	if retained != 2 || anonymized != 2 {
		t.Fatalf("retained/anonymized rows = %d/%d, want 2/2", retained, anonymized)
	}

	if err := s.DeleteMediaItem(item.ID); err != nil {
		t.Fatalf("DeleteMediaItem: %v", err)
	}
	rows, hasMore, err = s.ListMediaActivityPage(item.ID, viewer.ID, nil, 10)
	if err != nil || hasMore || len(rows) != 0 {
		t.Fatalf("activity survived media deletion = %+v, hasMore %v, %v", rows, hasMore, err)
	}
	if err := s.db.Table("media_activity").Where("media_item_id = ?", item.ID).Count(&retained).Error; err != nil || retained != 0 {
		t.Fatalf("activity count after media deletion = %d, %v; want 0", retained, err)
	}
}

func mustCreateActivityUser(t *testing.T, s *SQLiteStore, email string) *store.User {
	t.Helper()
	user := &store.User{Email: email, PasswordHash: "hash"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return user
}

func newActivityFixture(item *store.MediaItem, userID uint, operationID, details string) *store.MediaActivity {
	return &store.MediaActivity{
		MediaItemID:    item.ID,
		ActorKind:      store.MediaActivityActorUser,
		ActorUserID:    &userID,
		Action:         store.MediaActivityActionSettingsChanged,
		OperationID:    operationID,
		Visibility:     store.MediaActivityVisibilityShared,
		MediaTitle:     item.Title,
		DetailsVersion: store.MediaActivityDetailsVersion,
		Details:        details,
	}
}

func marshalActivityDetails(t *testing.T, details store.MediaActivityDetails) string {
	t.Helper()
	payload, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(payload)
}

func assertActivityIDs(t *testing.T, rows []store.MediaActivityAttribution, want []uint) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("activity IDs length = %d, want %d; rows=%+v", len(rows), len(want), rows)
	}
	for i := range want {
		if rows[i].ID != want[i] {
			t.Fatalf("activity ID at %d = %d, want %d; rows=%+v", i, rows[i].ID, want[i], rows)
		}
	}
}

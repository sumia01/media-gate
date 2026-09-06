package sqlite

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

func TestSetMonitorSearchStartedAt(t *testing.T) {
	old := time.Now().Add(-time.Hour).UTC()
	now := time.Now().UTC()
	for _, tc := range []struct {
		name      string
		monitored bool
		existing  *time.Time
		startedAt *time.Time
		want      *time.Time
	}{
		{name: "start", monitored: true, startedAt: &now, want: &now},
		{name: "preserve first search", monitored: true, existing: &old, startedAt: &now, want: &old},
		{name: "disabled", startedAt: &now},
		{name: "disabled existing", existing: &old, startedAt: &now, want: &old},
		{name: "clear", monitored: true, existing: &old},
		{name: "clear disabled", existing: &old},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			item := mustCreateMediaItem(t, s)
			profile := &store.MediaProfile{Name: "New profile"}
			if err := s.CreateMediaProfile(profile); err != nil {
				t.Fatal(err)
			}
			item.Monitored, item.MonitorSearchStartedAt = tc.monitored, tc.existing
			item.MediaProfileID, item.PreferredRelease = &profile.ID, "NEW"
			if err := s.UpdateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			before, err := s.GetMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetMonitorSearchStartedAt(item.ID, tc.startedAt); err != nil {
				t.Fatal(err)
			}
			after, err := s.GetMediaItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			marker := after.MonitorSearchStartedAt
			if (marker == nil) != (tc.want == nil) || (marker != nil && tc.want != nil && !marker.Equal(*tc.want)) {
				t.Errorf("marker=%v, want %v", marker, tc.want)
			}
			after.MonitorSearchStartedAt = before.MonitorSearchStartedAt
			if !reflect.DeepEqual(before, after) {
				t.Errorf("marker write changed other columns: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestSetMonitorSearchStartedAtDoesNotRecreateDeletedItem(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	if err := s.DeleteMediaItem(item.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, marker := range []*time.Time{&now, nil} {
		if err := s.SetMonitorSearchStartedAt(item.ID, marker); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetMediaItem(item.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("marker write recreated parent: %v", err)
		}
	}
}

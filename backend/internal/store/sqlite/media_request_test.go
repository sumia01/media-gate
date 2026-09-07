package sqlite

import (
	"errors"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func TestMediaRequestAttributionLifecycle(t *testing.T) {
	s := newTestStore(t)
	item := mustCreateMediaItem(t, s)
	user := &store.User{Email: "agnes@example.com", PasswordHash: "hash", FirstName: "Agnes", LastName: "Sumi"}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	season := 2
	request := &store.MediaRequest{
		MediaItemID:  item.ID,
		UserID:       &user.ID,
		Scope:        "season",
		SeasonNumber: &season,
	}
	if err := s.CreateMediaRequest(request); err != nil {
		t.Fatalf("CreateMediaRequest: %v", err)
	}
	if err := s.CreateMediaRequest(&store.MediaRequest{
		MediaItemID:  item.ID,
		UserID:       &user.ID,
		Scope:        "season",
		SeasonNumber: &season,
	}); err != nil {
		t.Fatalf("duplicate CreateMediaRequest: %v", err)
	}

	requests, err := s.ListMediaRequestsByMediaItem(item.ID)
	if err != nil {
		t.Fatalf("ListMediaRequestsByMediaItem: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %+v, want one idempotent attribution", requests)
	}
	if requests[0].FirstName != "Agnes" || requests[0].LastName != "Sumi" || requests[0].RequestedAt.IsZero() {
		t.Errorf("requester projection = %+v", requests[0])
	}

	if err := s.DeleteUser(user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	requests, err = s.ListMediaRequestsByMediaItem(item.ID)
	if err != nil {
		t.Fatalf("ListMediaRequestsByMediaItem after user deletion: %v", err)
	}
	if len(requests) != 1 || requests[0].UserID != nil {
		t.Fatalf("request after user deletion = %+v, want retained row with nil user", requests)
	}
	if err := s.CreateMediaRequest(request); !errors.Is(err, store.ErrRequesterNotFound) {
		t.Fatalf("CreateMediaRequest for deleted user = %v, want ErrRequesterNotFound", err)
	}

	if err := s.DeleteMediaItem(item.ID); err != nil {
		t.Fatalf("DeleteMediaItem: %v", err)
	}
	requests, err = s.ListMediaRequestsByMediaItem(item.ID)
	if err != nil {
		t.Fatalf("ListMediaRequestsByMediaItem after media deletion: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("requests after media deletion = %+v, want cascade deletion", requests)
	}
}

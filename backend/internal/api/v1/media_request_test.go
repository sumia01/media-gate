package apiv1

import (
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

func TestMediaRequestsToAPIUsesDisplayNameFallbacks(t *testing.T) {
	userID := uint(4)
	requestedAt := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	requests := []store.MediaRequestAttribution{
		{
			MediaRequest: store.MediaRequest{UserID: &userID, Scope: "media", RequestedAt: requestedAt},
			FirstName:    "Attila",
			LastName:     "Sumi",
		},
		{
			MediaRequest: store.MediaRequest{Scope: "season", RequestedAt: requestedAt},
		},
		{
			MediaRequest: store.MediaRequest{UserID: &userID, Scope: "episode", RequestedAt: requestedAt},
			Email:        "requester@example.com",
		},
	}

	got := *mediaRequestsToAPI(requests)
	if got[0].Requester.Name != "Attila Sumi" || got[0].Requester.Id == nil || *got[0].Requester.Id != 4 {
		t.Errorf("named requester = %+v", got[0].Requester)
	}
	if got[1].Requester.Name != "Deleted user" || got[1].Requester.Id != nil {
		t.Errorf("deleted requester = %+v", got[1].Requester)
	}
	if got[2].Requester.Name != "requester@example.com" {
		t.Errorf("email fallback requester = %+v", got[2].Requester)
	}
}

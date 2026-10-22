package matching

import (
	"fmt"

	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

type requestAttributionInput struct {
	requesterID     *uint
	scopes          []requestScope
	mediaAdded      bool
	before          *mediasync.MonitoringState
	assignedProfile *store.MediaProfile
}

// recordRequestAttribution participates in the caller's transaction after its
// media/monitor writes. It owns intent and activity recording, not transaction
// lifetime or post-commit publication. Existing items supply their before-state;
// new items use the same disabled baseline as before the item existed.
func recordRequestAttribution(tx store.Store, item *store.MediaItem, input requestAttributionInput) (bool, error) {
	if err := createMediaRequests(tx, item.ID, input.requesterID, input.scopes); err != nil {
		return false, err
	}
	if input.requesterID == nil {
		return false, nil
	}
	after, err := mediasync.SnapshotMonitoring(tx, item.ID)
	if err != nil {
		return false, fmt.Errorf("snapshotting monitoring after request: %w", err)
	}
	before := input.before
	if input.mediaAdded {
		before = mediasync.NewInitialMonitoringState(*item)
	}
	if err := appendRequestActivity(tx, item, *input.requesterID, input.scopes, input.mediaAdded, before, after, input.assignedProfile); err != nil {
		return false, err
	}
	return true, nil
}

func createMediaRequests(st store.Store, mediaItemID uint, requesterID *uint, scopes []requestScope) error {
	if requesterID == nil {
		return nil
	}
	for _, scope := range scopes {
		request := &store.MediaRequest{
			MediaItemID: mediaItemID,
			UserID:      requesterID,
			Scope:       scope.scope,
		}
		if scope.scope == store.MediaRequestScopeSeason || scope.scope == store.MediaRequestScopeEpisode {
			seasonNumber := scope.seasonNumber
			request.SeasonNumber = &seasonNumber
		}
		if scope.scope == store.MediaRequestScopeEpisode {
			episodeNumber := scope.episodeNumber
			request.EpisodeNumber = &episodeNumber
		}
		if err := st.CreateMediaRequest(request); err != nil {
			return fmt.Errorf("recording media request: %w", err)
		}
	}
	return nil
}

func appendRequestActivity(st store.Store, item *store.MediaItem, requesterID uint, scopes []requestScope, mediaAdded bool, before, after *mediasync.MonitoringState, assignedProfile *store.MediaProfile) error {
	details := mediasync.BuildMonitoringActivityDetails(before, after)
	details.MediaAdded = &mediaAdded
	targets := make([]store.MediaActivityTarget, 0, min(len(scopes), store.MediaActivityMaxDetails))
	for _, scope := range scopes {
		if len(targets) == store.MediaActivityMaxDetails {
			details.Truncated = true
			if len(scopes) > details.Total {
				details.Total = len(scopes)
			}
			break
		}
		target := store.MediaActivityTarget{Scope: scope.scope}
		if scope.scope == store.MediaRequestScopeSeason || scope.scope == store.MediaRequestScopeEpisode {
			seasonNumber := scope.seasonNumber
			target.SeasonNumber = &seasonNumber
		}
		if scope.scope == store.MediaRequestScopeEpisode {
			episodeNumber := scope.episodeNumber
			target.EpisodeNumber = &episodeNumber
		}
		targets = append(targets, target)
	}
	if len(targets) == 1 {
		details.Target = &targets[0]
	} else {
		details.Targets = targets
	}

	operationID, err := store.NewMediaActivityOperationID()
	if err != nil {
		return err
	}
	activity, err := store.NewUserMediaActivity(item, requesterID, store.MediaActivityActionRequestMade, operationID, store.MediaActivityVisibilityShared, details)
	if err != nil {
		return fmt.Errorf("creating request activity: %w", err)
	}
	if err := st.AppendMediaActivity(activity); err != nil {
		return fmt.Errorf("recording request activity: %w", err)
	}
	if assignedProfile != nil {
		label := requestedProfileActivityLabel(assignedProfile)
		settingsDetails := store.MediaActivityDetails{
			FieldChanges: []store.MediaActivityFieldChange{{Field: "media_profile", After: &label}},
			Total:        1,
		}
		settingsActivity, err := store.NewUserMediaActivity(item, requesterID, store.MediaActivityActionSettingsChanged, operationID, store.MediaActivityVisibilityShared, settingsDetails)
		if err != nil {
			return fmt.Errorf("creating request profile activity: %w", err)
		}
		if err := st.AppendMediaActivity(settingsActivity); err != nil {
			return fmt.Errorf("recording request profile activity: %w", err)
		}
	}
	return nil
}

func requestedProfileActivityLabel(profile *store.MediaProfile) string {
	suffix := fmt.Sprintf(" (ID %d)", profile.ID)
	name := store.BoundMediaActivityText(profile.Name, store.MediaActivityMaxTitleBytes-len(suffix))
	return name + suffix
}

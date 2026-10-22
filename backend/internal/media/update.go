package media

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

var ErrMediaProfileNotFound = errors.New("media profile not found")

// UpdateInput contains optional media settings. Nil fields are left unchanged;
// non-nil empty monitor slices still request monitoring recalculation.
type UpdateInput struct {
	MediaProfileID    *uint
	Monitored         *bool
	MonitorNewSeasons *bool
	PreferredRelease  *string
	ForceRelease      *bool
	SeasonMonitors    []mediasync.SeasonMonitorInput
	EpisodeMonitors   []mediasync.EpisodeMonitorInput
}

// UpdateMediaItem commits settings, monitoring, requester attribution, and
// activity together, then recalculates status and publishes refresh hints.
func (s *Service) UpdateMediaItem(userID, itemID uint, input UpdateInput) (*store.MediaItem, error) {
	monitoringMutation := input.Monitored != nil || input.MonitorNewSeasons != nil || input.SeasonMonitors != nil || input.EpisodeMonitors != nil
	changedSeasons := make([]int, len(input.SeasonMonitors))
	for i, season := range input.SeasonMonitors {
		changedSeasons[i] = season.SeasonNumber
	}
	changedEpisodes := make([]mediasync.EpisodeRef, len(input.EpisodeMonitors))
	for i, episode := range input.EpisodeMonitors {
		changedEpisodes[i] = mediasync.EpisodeRef{SeasonNumber: episode.SeasonNumber, EpisodeNumber: episode.EpisodeNumber}
	}

	var item *store.MediaItem
	requestAttributed := false
	activityAdded := false
	err := s.store.WithTx(func(tx store.Store) error {
		if err := validateMediaActivityActor(tx, userID); err != nil {
			return err
		}
		current, err := tx.GetMediaItem(itemID)
		if err != nil {
			return err
		}
		if current.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		beforeItem := *current
		var before *mediasync.MonitoringState
		if monitoringMutation {
			before, err = mediasync.SnapshotMonitoring(tx, current.ID)
			if err != nil {
				return err
			}
		}

		if input.MediaProfileID != nil {
			if _, err := tx.GetMediaProfile(*input.MediaProfileID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return ErrMediaProfileNotFound
				}
				return err
			}
			current.MediaProfileID = input.MediaProfileID
		}
		if input.Monitored != nil {
			current.Monitored = *input.Monitored
			if !*input.Monitored {
				if err := tx.DeleteEpisodeMonitorsByMediaItem(current.ID); err != nil {
					return err
				}
			}
		}
		if input.MonitorNewSeasons != nil {
			current.MonitorNewSeasons = *input.MonitorNewSeasons
		}
		if input.PreferredRelease != nil {
			keywords := fileparse.ParseReleaseKeywords(*input.PreferredRelease)
			current.PreferredRelease = strings.Join(keywords, ", ")
			if err := tx.RememberReleaseKeywords(userID, keywords); err != nil {
				return err
			}
		}
		if input.ForceRelease != nil {
			current.ForceRelease = *input.ForceRelease
		}
		if err := tx.UpdateMediaItem(current); err != nil {
			return err
		}

		txSync := mediasync.NewService(tx)
		if len(input.SeasonMonitors) > 0 {
			if err := txSync.UpsertSeasonMonitors(current.ID, input.SeasonMonitors); err != nil {
				return err
			}
		}
		if len(input.EpisodeMonitors) > 0 {
			if err := txSync.UpsertEpisodeMonitors(current.ID, input.EpisodeMonitors); err != nil {
				return err
			}
		}
		var monitoringDetails store.MediaActivityDetails
		if monitoringMutation {
			after, err := mediasync.SnapshotMonitoring(tx, current.ID)
			if err != nil {
				return err
			}
			requestAttributed, err = mediasync.RecordMonitoringTransitions(tx, userID, before, after, changedSeasons, changedEpisodes)
			if err != nil {
				return err
			}
			monitoringDetails = mediasync.BuildMonitoringActivityDetails(before, after)
		}

		afterItem, err := tx.GetMediaItem(current.ID)
		if err != nil {
			return err
		}
		fieldChanges := make([]store.MediaActivityFieldChange, 0, 3)
		if input.MediaProfileID != nil && !sameUintPointers(beforeItem.MediaProfileID, afterItem.MediaProfileID) {
			var beforeProfile *store.MediaProfile
			if beforeItem.MediaProfileID != nil {
				beforeProfile, err = tx.GetMediaProfile(*beforeItem.MediaProfileID)
				if err != nil {
					return err
				}
			}
			afterProfile, err := tx.GetMediaProfile(*afterItem.MediaProfileID)
			if err != nil {
				return err
			}
			fieldChanges = append(fieldChanges, store.MediaActivityFieldChange{
				Field: "media_profile", Before: mediaProfileActivityLabel(beforeProfile), After: mediaProfileActivityLabel(afterProfile),
			})
		}
		if input.PreferredRelease != nil && beforeItem.PreferredRelease != afterItem.PreferredRelease {
			fieldChanges = append(fieldChanges, store.MediaActivityFieldChange{
				Field: "preferred_release", Before: stringPointer(beforeItem.PreferredRelease), After: stringPointer(afterItem.PreferredRelease),
			})
		}
		if input.ForceRelease != nil && beforeItem.ForceRelease != afterItem.ForceRelease {
			fieldChanges = append(fieldChanges, store.MediaActivityFieldChange{
				Field: "force_release", Before: stringPointer(fmt.Sprint(beforeItem.ForceRelease)), After: stringPointer(fmt.Sprint(afterItem.ForceRelease)),
			})
		}

		if monitoringDetails.Total > 0 || len(fieldChanges) > 0 {
			operationID, err := store.NewMediaActivityOperationID()
			if err != nil {
				return err
			}
			if monitoringDetails.Total > 0 {
				if err := appendUserMediaActivity(tx, afterItem, userID, store.MediaActivityActionMonitoringChanged, operationID, monitoringDetails); err != nil {
					return err
				}
			}
			if len(fieldChanges) > 0 {
				settingsDetails := store.MediaActivityDetails{FieldChanges: fieldChanges, Total: len(fieldChanges)}
				if err := appendUserMediaActivity(tx, afterItem, userID, store.MediaActivityActionSettingsChanged, operationID, settingsDetails); err != nil {
					return err
				}
			}
			activityAdded = true
		}
		item = afterItem
		return nil
	})
	if err != nil {
		return nil, err
	}

	if monitoringMutation {
		s.recalcStatusAfterMonitorChange(item.ID)
		if fresh, err := s.store.GetMediaItem(item.ID); err == nil {
			item = fresh
		}
	}
	if requestAttributed {
		s.syncSvc.PublishRequestAttribution(item)
	}
	if activityAdded {
		s.syncSvc.PublishActivityInvalidation(item.ID)
	}
	return item, nil
}

func appendUserMediaActivity(tx store.Store, item *store.MediaItem, userID uint, action, operationID string, details store.MediaActivityDetails) error {
	activity, err := store.NewUserMediaActivity(item, userID, action, operationID, store.MediaActivityVisibilityShared, details)
	if err != nil {
		return err
	}
	return tx.AppendMediaActivity(activity)
}

func mediaProfileActivityLabel(profile *store.MediaProfile) *string {
	if profile == nil {
		return nil
	}
	suffix := fmt.Sprintf(" (ID %d)", profile.ID)
	label := store.BoundMediaActivityText(profile.Name, store.MediaActivityMaxTitleBytes-len(suffix)) + suffix
	return &label
}

func sameUintPointers(a, b *uint) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func stringPointer(value string) *string {
	bounded := store.BoundMediaActivityText(value, store.MediaActivityMaxTitleBytes)
	return &bounded
}

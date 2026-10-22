package media

import (
	"log/slog"
	"time"

	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

// UpdateSeasonMonitor saves the season setting and clears its episode overrides,
// even when the setting is unchanged. Attribution and activity commit with it.
func (s *Service) UpdateSeasonMonitor(userID, itemID uint, input mediasync.SeasonMonitorInput) (*store.SeasonMonitor, error) {
	var sm store.SeasonMonitor
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
		before, err := mediasync.SnapshotMonitoring(tx, current.ID)
		if err != nil {
			return err
		}
		monitors, err := tx.ListSeasonMonitorsByMediaItem(current.ID)
		if err != nil {
			return err
		}
		for _, monitor := range monitors {
			if monitor.SeasonNumber == input.SeasonNumber {
				sm = monitor
				break
			}
		}
		sm.MediaItemID = current.ID
		sm.SeasonNumber = input.SeasonNumber
		sm.Monitored = input.Monitored
		if sm.ID == 0 {
			err = tx.CreateSeasonMonitor(&sm)
		} else {
			err = tx.UpdateSeasonMonitor(&sm)
		}
		if err != nil {
			return err
		}
		// Episodes now inherit from the season setting.
		if err := tx.DeleteEpisodeMonitorsBySeason(current.ID, input.SeasonNumber); err != nil {
			return err
		}
		// Touch the transaction's fresh parent even if status stays unchanged.
		current.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateMediaItem(current); err != nil {
			return err
		}
		after, err := mediasync.SnapshotMonitoring(tx, current.ID)
		if err != nil {
			return err
		}
		requestAttributed, err = mediasync.RecordMonitoringTransitions(tx, userID, before, after, []int{input.SeasonNumber}, nil)
		if err != nil {
			return err
		}
		details := mediasync.BuildMonitoringActivityDetails(before, after)
		if details.Total > 0 {
			operationID, err := store.NewMediaActivityOperationID()
			if err != nil {
				return err
			}
			if err := appendUserMediaActivity(tx, current, userID, store.MediaActivityActionMonitoringChanged, operationID, details); err != nil {
				return err
			}
			activityAdded = true
		}
		item = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.recalcStatusAfterMonitorChange(itemID)
	if requestAttributed {
		s.syncSvc.PublishRequestAttribution(item)
	}
	if activityAdded {
		s.syncSvc.PublishActivityInvalidation(item.ID)
	}
	return &sm, nil
}

// UpdateEpisodeMonitor saves one override without changing its siblings.
// Attribution and activity commit together with the parent's freshness timestamp.
func (s *Service) UpdateEpisodeMonitor(userID, itemID uint, input mediasync.EpisodeMonitorInput) error {
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
		before, err := mediasync.SnapshotMonitoring(tx, current.ID)
		if err != nil {
			return err
		}
		if err := tx.UpsertEpisodeMonitor(&store.EpisodeMonitor{
			MediaItemID:   current.ID,
			SeasonNumber:  input.SeasonNumber,
			EpisodeNumber: input.EpisodeNumber,
			Monitored:     input.Monitored,
		}); err != nil {
			return err
		}
		// Keep saved monitor decisions visibly stale after a reload, too.
		current.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateMediaItem(current); err != nil {
			return err
		}
		after, err := mediasync.SnapshotMonitoring(tx, current.ID)
		if err != nil {
			return err
		}
		changed := []mediasync.EpisodeRef{{SeasonNumber: input.SeasonNumber, EpisodeNumber: input.EpisodeNumber}}
		requestAttributed, err = mediasync.RecordMonitoringTransitions(tx, userID, before, after, nil, changed)
		if err != nil {
			return err
		}
		details := mediasync.BuildMonitoringActivityDetails(before, after)
		if details.Total > 0 {
			operationID, err := store.NewMediaActivityOperationID()
			if err != nil {
				return err
			}
			if err := appendUserMediaActivity(tx, current, userID, store.MediaActivityActionMonitoringChanged, operationID, details); err != nil {
				return err
			}
			activityAdded = true
		}
		item = current
		return nil
	})
	if err != nil {
		return err
	}
	s.recalcStatusAfterMonitorChange(itemID)
	if requestAttributed {
		s.syncSvc.PublishRequestAttribution(item)
	}
	if activityAdded {
		s.syncSvc.PublishActivityInvalidation(item.ID)
	}
	return nil
}

// recalcStatusAfterMonitorChange is best-effort: the mutation already committed.
func (s *Service) recalcStatusAfterMonitorChange(itemID uint) {
	if err := s.syncSvc.RecalcMediaItemStatus(itemID); err != nil {
		slog.Warn("monitor update: status recalc failed", "media_item_id", itemID, "error", err)
	}
}

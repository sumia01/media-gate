package apiv1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

var (
	errMediaActivityActorMissing = errors.New("authenticated media actor missing from context")
	errMediaProfileNotFound      = errors.New("media profile not found")
)

// recalcStatusAfterMonitorChange refreshes the item's persisted status after a
// monitoring change (the status state machine is monitoring-aware). Failures
// are logged, not returned — the monitor update itself already succeeded.
func (h *Handlers) recalcStatusAfterMonitorChange(itemID uint) {
	if err := h.syncSvc.RecalcMediaItemStatus(itemID); err != nil {
		slog.Warn("monitor update: status recalc failed", "media_item_id", itemID, "error", err)
	}
}

func mediaActivityActorID(ctx context.Context) (uint, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok || userID == 0 {
		return 0, errMediaActivityActorMissing
	}
	return userID, nil
}

func validateMediaActivityActor(tx store.Store, userID uint) error {
	if _, err := tx.GetUser(userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrActivityActorNotFound
		}
		return err
	}
	return nil
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

func (h *Handlers) GetMediaItem(_ context.Context, req GetMediaItemRequestObject) (GetMediaItemResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}
	meta, _ := h.store.GetMediaMetadataByMediaItem(item.ID)
	requests, err := h.store.ListMediaRequestsByMediaItem(item.ID)
	if err != nil {
		return nil, err
	}
	apiItem := h.withRatings(mediaItemToAPI(item, meta), meta)
	apiItem.Requests = mediaRequestsToAPI(requests)

	return GetMediaItem200JSONResponse(apiItem), nil
}

func (h *Handlers) UpdateMediaItem(ctx context.Context, req UpdateMediaItemRequestObject) (UpdateMediaItemResponseObject, error) {
	userID, err := mediaActivityActorID(ctx)
	if err != nil {
		return nil, err
	}

	var profileID *uint
	if req.Body.MediaProfileId != nil {
		id := uint(*req.Body.MediaProfileId)
		profileID = &id
	}

	monitoringMutation := req.Body.Monitored != nil || req.Body.MonitorNewSeasons != nil || req.Body.SeasonMonitors != nil || req.Body.EpisodeMonitors != nil
	seasonInputs := make([]mediasync.SeasonMonitorInput, 0)
	changedSeasons := make([]int, 0)
	if req.Body.SeasonMonitors != nil {
		seasonInputs = make([]mediasync.SeasonMonitorInput, len(*req.Body.SeasonMonitors))
		changedSeasons = make([]int, len(*req.Body.SeasonMonitors))
		for i, season := range *req.Body.SeasonMonitors {
			seasonInputs[i] = mediasync.SeasonMonitorInput{SeasonNumber: season.SeasonNumber, Monitored: season.Monitored}
			changedSeasons[i] = season.SeasonNumber
		}
	}
	episodeInputs := make([]mediasync.EpisodeMonitorInput, 0)
	changedEpisodes := make([]mediasync.EpisodeRef, 0)
	if req.Body.EpisodeMonitors != nil {
		episodeInputs = make([]mediasync.EpisodeMonitorInput, len(*req.Body.EpisodeMonitors))
		changedEpisodes = make([]mediasync.EpisodeRef, len(*req.Body.EpisodeMonitors))
		for i, episode := range *req.Body.EpisodeMonitors {
			episodeInputs[i] = mediasync.EpisodeMonitorInput{
				SeasonNumber:  episode.SeasonNumber,
				EpisodeNumber: episode.EpisodeNumber,
				Monitored:     episode.Monitored,
			}
			changedEpisodes[i] = mediasync.EpisodeRef{SeasonNumber: episode.SeasonNumber, EpisodeNumber: episode.EpisodeNumber}
		}
	}

	var item *store.MediaItem
	requestAttributed := false
	activityAdded := false
	err = h.store.WithTx(func(tx store.Store) error {
		if err := validateMediaActivityActor(tx, userID); err != nil {
			return err
		}
		current, err := tx.GetMediaItem(uint(req.Id))
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

		if profileID != nil {
			if _, err := tx.GetMediaProfile(*profileID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return errMediaProfileNotFound
				}
				return err
			}
			current.MediaProfileID = profileID
		}
		if req.Body.Monitored != nil {
			current.Monitored = *req.Body.Monitored
			if !*req.Body.Monitored {
				if err := tx.DeleteEpisodeMonitorsByMediaItem(current.ID); err != nil {
					return err
				}
			}
		}
		if req.Body.MonitorNewSeasons != nil {
			current.MonitorNewSeasons = *req.Body.MonitorNewSeasons
		}
		if req.Body.PreferredRelease != nil {
			current.PreferredRelease = *req.Body.PreferredRelease
		}
		if err := tx.UpdateMediaItem(current); err != nil {
			return err
		}

		txSync := mediasync.NewService(tx)
		if len(seasonInputs) > 0 {
			if err := txSync.UpsertSeasonMonitors(current.ID, seasonInputs); err != nil {
				return err
			}
		}
		if len(episodeInputs) > 0 {
			if err := txSync.UpsertEpisodeMonitors(current.ID, episodeInputs); err != nil {
				return err
			}
		}
		var monitoringDetails store.MediaActivityDetails
		var after *mediasync.MonitoringState
		if monitoringMutation {
			after, err = mediasync.SnapshotMonitoring(tx, current.ID)
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
		fieldChanges := make([]store.MediaActivityFieldChange, 0, 2)
		if profileID != nil && !sameUintPointers(beforeItem.MediaProfileID, afterItem.MediaProfileID) {
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
		if req.Body.PreferredRelease != nil && beforeItem.PreferredRelease != afterItem.PreferredRelease {
			fieldChanges = append(fieldChanges, store.MediaActivityFieldChange{
				Field: "preferred_release", Before: stringPointer(beforeItem.PreferredRelease), After: stringPointer(afterItem.PreferredRelease),
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
		if errors.Is(err, errMediaProfileNotFound) {
			return UpdateMediaItem404JSONResponse{Code: http.StatusNotFound, Message: "media profile not found"}, nil
		}
		if errors.Is(err, store.ErrNotFound) {
			return UpdateMediaItem404JSONResponse{Code: http.StatusNotFound, Message: "media item not found"}, nil
		}
		return nil, err
	}

	if monitoringMutation {
		h.recalcStatusAfterMonitorChange(item.ID)
		if fresh, err := h.store.GetMediaItem(item.ID); err == nil {
			item = fresh
		}
	}
	if requestAttributed {
		h.syncSvc.PublishRequestAttribution(item)
	}
	if activityAdded {
		h.syncSvc.PublishActivityInvalidation(item.ID)
	}

	meta, _ := h.store.GetMediaMetadataByMediaItem(item.ID)
	requests, err := h.store.ListMediaRequestsByMediaItem(item.ID)
	if err != nil {
		return nil, err
	}
	apiItem := h.withRatings(mediaItemToAPI(item, meta), meta)
	apiItem.Requests = mediaRequestsToAPI(requests)
	return UpdateMediaItem200JSONResponse(apiItem), nil
}

func (h *Handlers) DeleteMediaItem(ctx context.Context, req DeleteMediaItemRequestObject) (DeleteMediaItemResponseObject, error) {
	userID, err := mediaActivityActorID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.mediaSvc.DeleteMediaItem(userID, uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}
	return DeleteMediaItem204Response{}, nil
}

func (h *Handlers) SearchMediaCandidates(_ context.Context, req SearchMediaCandidatesRequestObject) (SearchMediaCandidatesResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return SearchMediaCandidates404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	query := item.Title
	if req.Params.Query != nil && *req.Params.Query != "" {
		query = *req.Params.Query
	}

	var source string
	if req.Params.Source != nil {
		source = string(*req.Params.Source)
	}

	candidates, err := h.matchSvc.SearchCandidates(query, item.MediaType, item.Year, source)
	if err != nil {
		return nil, err
	}

	return SearchMediaCandidates200JSONResponse{Candidates: candidatesToAPI(candidates)}, nil
}

func (h *Handlers) ManualMatch(ctx context.Context, req ManualMatchRequestObject) (ManualMatchResponseObject, error) {
	userID, err := mediaActivityActorID(ctx)
	if err != nil {
		return nil, err
	}

	item, meta, err := h.matchSvc.ManualMatch(uint(req.Id), string(req.Body.Source), req.Body.ExternalId, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ManualMatch404JSONResponse{Code: http.StatusNotFound, Message: "media item not found"}, nil
		}
		return nil, err
	}

	return ManualMatch200JSONResponse(h.withRatings(mediaItemToAPI(item, meta), meta)), nil
}

func (h *Handlers) UnmatchMedia(ctx context.Context, req UnmatchMediaRequestObject) (UnmatchMediaResponseObject, error) {
	userID, err := mediaActivityActorID(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.matchSvc.Unmatch(uint(req.Id), userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UnmatchMedia404JSONResponse{Code: http.StatusNotFound, Message: "media item not found"}, nil
		}
		return nil, err
	}

	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		return nil, err
	}

	return UnmatchMedia200JSONResponse(mediaItemToAPI(item, nil)), nil
}

func (h *Handlers) ResyncMediaItem(ctx context.Context, req ResyncMediaItemRequestObject) (ResyncMediaItemResponseObject, error) {
	userID, err := mediaActivityActorID(ctx)
	if err != nil {
		return nil, err
	}
	updated, added, removed, err := h.syncSvc.ResyncMediaItemForUser(userID, uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ResyncMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	return ResyncMediaItem200JSONResponse{
		Updated: updated,
		Added:   added,
		Removed: removed,
	}, nil
}

func (h *Handlers) ListMediaFiles(_ context.Context, req ListMediaFilesRequestObject) (ListMediaFilesResponseObject, error) {
	_, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListMediaFiles404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	files, err := h.store.ListMediaFilesByMediaItem(uint(req.Id))
	if err != nil {
		return nil, err
	}

	apiFiles := make([]MediaFile, len(files))
	for i := range files {
		apiFiles[i] = mediaFileToAPI(&files[i])
	}

	// Sort by season number, then episode number, then filename
	sort.Slice(apiFiles, func(i, j int) bool {
		si := derefInt(apiFiles[i].SeasonNumber)
		sj := derefInt(apiFiles[j].SeasonNumber)
		if si != sj {
			return si < sj
		}
		ei := derefInt(apiFiles[i].EpisodeNumber)
		ej := derefInt(apiFiles[j].EpisodeNumber)
		if ei != ej {
			return ei < ej
		}
		return apiFiles[i].FileName < apiFiles[j].FileName
	})

	return ListMediaFiles200JSONResponse{Files: apiFiles}, nil
}

func (h *Handlers) ListMediaEpisodes(_ context.Context, req ListMediaEpisodesRequestObject) (ListMediaEpisodesResponseObject, error) {
	itemID := uint(req.Id)
	_, err := h.store.GetMediaItem(itemID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListMediaEpisodes404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	data, err := h.syncSvc.AssembleEpisodes(itemID)
	if err != nil {
		return nil, err
	}

	seasons := make([]SeasonSummary, len(data))
	for i, s := range data {
		eps := make([]Episode, len(s.Episodes))
		for j, es := range s.Episodes {
			ep := es.Episode
			hasFile := es.HasFile
			monitored := es.Monitored
			eps[j] = Episode{
				Id:            int64(ep.ID),
				MediaItemId:   int64(ep.MediaItemID),
				SeasonNumber:  ep.SeasonNumber,
				EpisodeNumber: ep.EpisodeNumber,
				HasFile:       &hasFile,
				Monitored:     &monitored,
			}
			if ep.Title != "" {
				eps[j].Title = &ep.Title
			}
			if ep.Overview != "" {
				eps[j].Overview = &ep.Overview
			}
			if ep.AirDate != "" {
				eps[j].AirDate = &ep.AirDate
			}
			if ep.Runtime != nil {
				eps[j].Runtime = ep.Runtime
			}
			if es.DownloadStatus != "" {
				ds := EpisodeDownloadStatus(es.DownloadStatus)
				eps[j].DownloadStatus = &ds
			}
		}
		seasons[i] = SeasonSummary{
			SeasonNumber:      s.SeasonNumber,
			TotalEpisodes:     s.TotalEpisodes,
			AvailableEpisodes: s.AvailableEpisodes,
			Monitored:         s.Monitored,
			Episodes:          &eps,
		}
	}

	return ListMediaEpisodes200JSONResponse{Seasons: seasons}, nil
}

func (h *Handlers) ListSeasonMonitors(_ context.Context, req ListSeasonMonitorsRequestObject) (ListSeasonMonitorsResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListSeasonMonitors404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}
	_ = item

	monitors, err := h.store.ListSeasonMonitorsByMediaItem(uint(req.Id))
	if err != nil {
		return nil, err
	}

	apiMonitors := make([]SeasonMonitor, len(monitors))
	for i, m := range monitors {
		apiMonitors[i] = SeasonMonitor{
			Id:           int64(m.ID),
			MediaItemId:  int64(m.MediaItemID),
			SeasonNumber: m.SeasonNumber,
			Monitored:    m.Monitored,
		}
	}

	return ListSeasonMonitors200JSONResponse{Monitors: apiMonitors}, nil
}

func (h *Handlers) UpdateSeasonMonitor(ctx context.Context, req UpdateSeasonMonitorRequestObject) (UpdateSeasonMonitorResponseObject, error) {
	userID, err := mediaActivityActorID(ctx)
	if err != nil {
		return nil, err
	}
	var sm store.SeasonMonitor
	var item *store.MediaItem
	requestAttributed := false
	activityAdded := false
	err = h.store.WithTx(func(tx store.Store) error {
		if err := validateMediaActivityActor(tx, userID); err != nil {
			return err
		}
		current, err := tx.GetMediaItem(uint(req.Id))
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
			if monitor.SeasonNumber == req.SeasonNumber {
				sm = monitor
				break
			}
		}
		sm.MediaItemID = current.ID
		sm.SeasonNumber = req.SeasonNumber
		sm.Monitored = req.Body.Monitored
		if sm.ID == 0 {
			err = tx.CreateSeasonMonitor(&sm)
		} else {
			err = tx.UpdateSeasonMonitor(&sm)
		}
		if err != nil {
			return err
		}
		// Episodes now inherit from the season setting.
		if err := tx.DeleteEpisodeMonitorsBySeason(current.ID, req.SeasonNumber); err != nil {
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
		requestAttributed, err = mediasync.RecordMonitoringTransitions(tx, userID, before, after, []int{req.SeasonNumber}, nil)
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
		if errors.Is(err, store.ErrNotFound) {
			return UpdateSeasonMonitor404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}
	h.recalcStatusAfterMonitorChange(uint(req.Id))
	if requestAttributed {
		h.syncSvc.PublishRequestAttribution(item)
	}
	if activityAdded {
		h.syncSvc.PublishActivityInvalidation(item.ID)
	}

	return UpdateSeasonMonitor200JSONResponse(SeasonMonitor{
		Id:           int64(sm.ID),
		MediaItemId:  int64(sm.MediaItemID),
		SeasonNumber: sm.SeasonNumber,
		Monitored:    sm.Monitored,
	}), nil
}

func (h *Handlers) UpdateEpisodeMonitor(ctx context.Context, req UpdateEpisodeMonitorRequestObject) (UpdateEpisodeMonitorResponseObject, error) {
	userID, err := mediaActivityActorID(ctx)
	if err != nil {
		return nil, err
	}
	var item *store.MediaItem
	requestAttributed := false
	activityAdded := false
	err = h.store.WithTx(func(tx store.Store) error {
		if err := validateMediaActivityActor(tx, userID); err != nil {
			return err
		}
		current, err := tx.GetMediaItem(uint(req.Id))
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
			SeasonNumber:  req.SeasonNumber,
			EpisodeNumber: req.EpisodeNumber,
			Monitored:     req.Body.Monitored,
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
		changed := []mediasync.EpisodeRef{{SeasonNumber: req.SeasonNumber, EpisodeNumber: req.EpisodeNumber}}
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
		if errors.Is(err, store.ErrNotFound) {
			return UpdateEpisodeMonitor404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}
	h.recalcStatusAfterMonitorChange(uint(req.Id))
	if requestAttributed {
		h.syncSvc.PublishRequestAttribution(item)
	}
	if activityAdded {
		h.syncSvc.PublishActivityInvalidation(item.ID)
	}

	return UpdateEpisodeMonitor200JSONResponse{Monitored: req.Body.Monitored}, nil
}

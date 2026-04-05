package apiv1

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

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

	return GetMediaItem200JSONResponse(mediaItemToAPI(item, meta)), nil
}

func (h *Handlers) UpdateMediaItem(_ context.Context, req UpdateMediaItemRequestObject) (UpdateMediaItemResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	if req.Body.MediaProfileId != nil {
		profileID := uint(*req.Body.MediaProfileId)
		if _, err := h.store.GetMediaProfile(profileID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return UpdateMediaItem404JSONResponse{
					Code:    http.StatusNotFound,
					Message: "media profile not found",
				}, nil
			}
			return nil, err
		}
		item.MediaProfileID = &profileID
	}

	if req.Body.Monitored != nil {
		item.Monitored = *req.Body.Monitored
	}

	if err := h.store.UpdateMediaItem(item); err != nil {
		return nil, err
	}

	// Upsert season monitors if provided.
	if req.Body.SeasonMonitors != nil {
		monitors := make([]mediasync.SeasonMonitorInput, len(*req.Body.SeasonMonitors))
		for i, sm := range *req.Body.SeasonMonitors {
			monitors[i] = mediasync.SeasonMonitorInput{SeasonNumber: sm.SeasonNumber, Monitored: sm.Monitored}
		}
		if err := h.syncSvc.UpsertSeasonMonitors(item.ID, monitors); err != nil {
			return nil, err
		}
	}

	meta, _ := h.store.GetMediaMetadataByMediaItem(item.ID)
	return UpdateMediaItem200JSONResponse(mediaItemToAPI(item, meta)), nil
}

func (h *Handlers) DeleteMediaItem(_ context.Context, req DeleteMediaItemRequestObject) (DeleteMediaItemResponseObject, error) {
	if err := h.mediaSvc.DeleteMediaItem(uint(req.Id)); err != nil {
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

func (h *Handlers) ManualMatch(_ context.Context, req ManualMatchRequestObject) (ManualMatchResponseObject, error) {
	_, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ManualMatch404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	item, meta, err := h.matchSvc.ManualMatch(uint(req.Id), string(req.Body.Source), req.Body.ExternalId)
	if err != nil {
		return nil, err
	}

	return ManualMatch200JSONResponse(mediaItemToAPI(item, meta)), nil
}

func (h *Handlers) UnmatchMedia(_ context.Context, req UnmatchMediaRequestObject) (UnmatchMediaResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UnmatchMedia404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	if err := h.matchSvc.Unmatch(item.ID); err != nil {
		return nil, err
	}

	item, err = h.store.GetMediaItem(item.ID)
	if err != nil {
		return nil, err
	}

	return UnmatchMedia200JSONResponse(mediaItemToAPI(item, nil)), nil
}

func (h *Handlers) ResyncMediaItem(_ context.Context, req ResyncMediaItemRequestObject) (ResyncMediaItemResponseObject, error) {
	_, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ResyncMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	updated, added, removed, err := h.syncSvc.ResyncMediaItem(uint(req.Id))
	if err != nil {
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
			eps[j] = Episode{
				Id:            int64(ep.ID),
				MediaItemId:   int64(ep.MediaItemID),
				SeasonNumber:  ep.SeasonNumber,
				EpisodeNumber: ep.EpisodeNumber,
				HasFile:       &hasFile,
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

func (h *Handlers) UpdateSeasonMonitor(_ context.Context, req UpdateSeasonMonitorRequestObject) (UpdateSeasonMonitorResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateSeasonMonitor404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}
	_ = item

	// Look for existing SeasonMonitor for this season.
	monitors, err := h.store.ListSeasonMonitorsByMediaItem(uint(req.Id))
	if err != nil {
		return nil, err
	}

	var existing *store.SeasonMonitor
	for i := range monitors {
		if monitors[i].SeasonNumber == req.SeasonNumber {
			existing = &monitors[i]
			break
		}
	}

	if existing != nil {
		existing.Monitored = req.Body.Monitored
		if err := h.store.UpdateSeasonMonitor(existing); err != nil {
			return nil, err
		}
		return UpdateSeasonMonitor200JSONResponse(SeasonMonitor{
			Id:           int64(existing.ID),
			MediaItemId:  int64(existing.MediaItemID),
			SeasonNumber: existing.SeasonNumber,
			Monitored:    existing.Monitored,
		}), nil
	}

	// Create new SeasonMonitor.
	sm := &store.SeasonMonitor{
		MediaItemID:  uint(req.Id),
		SeasonNumber: req.SeasonNumber,
		Monitored:    req.Body.Monitored,
	}
	if err := h.store.CreateSeasonMonitor(sm); err != nil {
		return nil, err
	}

	return UpdateSeasonMonitor200JSONResponse(SeasonMonitor{
		Id:           int64(sm.ID),
		MediaItemId:  int64(sm.MediaItemID),
		SeasonNumber: sm.SeasonNumber,
		Monitored:    sm.Monitored,
	}), nil
}

package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) CreateLibrary(_ context.Context, req CreateLibraryRequestObject) (CreateLibraryResponseObject, error) {
	lib := &store.Library{
		Name:      req.Body.Name,
		Path:      req.Body.Path,
		MediaType: string(req.Body.MediaType),
	}
	if req.Body.MediaProfileId != nil {
		pid := uint(*req.Body.MediaProfileId)
		lib.MediaProfileID = &pid
	}

	if err := h.lib.Create(lib); err != nil {
		return CreateLibrary400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateLibrary201JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) ListLibraries(_ context.Context, _ ListLibrariesRequestObject) (ListLibrariesResponseObject, error) {
	libs, err := h.lib.List()
	if err != nil {
		return nil, err
	}

	result := make(ListLibraries200JSONResponse, len(libs))
	for i := range libs {
		result[i] = libraryToAPI(&libs[i])
	}
	return result, nil
}

func (h *Handlers) GetLibrary(_ context.Context, req GetLibraryRequestObject) (GetLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	return GetLibrary200JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) UpdateLibrary(_ context.Context, req UpdateLibraryRequestObject) (UpdateLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	lib.Name = req.Body.Name
	lib.Path = req.Body.Path
	lib.MediaType = string(req.Body.MediaType)
	if req.Body.MediaProfileId != nil {
		pid := uint(*req.Body.MediaProfileId)
		lib.MediaProfileID = &pid
	} else {
		lib.MediaProfileID = nil
	}

	if err := h.lib.Update(lib); err != nil {
		return nil, err
	}

	return UpdateLibrary200JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) DeleteLibrary(_ context.Context, req DeleteLibraryRequestObject) (DeleteLibraryResponseObject, error) {
	id := uint(req.Id)

	// Clean up poster files before cascade-deleting the library.
	if err := h.mediaSvc.CleanupPostersForLibrary(id); err != nil {
		return nil, err
	}

	if err := h.lib.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	return DeleteLibrary204Response{}, nil
}

func (h *Handlers) ScanLibrary(_ context.Context, req ScanLibraryRequestObject) (ScanLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ScanLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	entries, err := h.lib.Scan(lib)
	if err != nil {
		return ScanLibrary400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	apiEntries := scanEntriesToAPI(entries)

	return ScanLibrary200JSONResponse{Entries: apiEntries}, nil
}

func (h *Handlers) BrowseFolder(_ context.Context, req BrowseFolderRequestObject) (BrowseFolderResponseObject, error) {
	var path string
	if req.Params.Path != nil {
		path = *req.Params.Path
	}

	browsedPath, entries, err := h.lib.Browse(path)
	if err != nil {
		return BrowseFolder400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	apiEntries := scanEntriesToAPI(entries)

	return BrowseFolder200JSONResponse{
		Path:    browsedPath,
		Entries: apiEntries,
	}, nil
}

func (h *Handlers) ListMediaItems(_ context.Context, req ListMediaItemsRequestObject) (ListMediaItemsResponseObject, error) {
	_, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListMediaItems404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	items, err := h.store.ListMediaItemsByLibrary(uint(req.Id))
	if err != nil {
		return nil, err
	}

	// Batch fetch metadata for all items
	ids := make([]uint, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	metas, err := h.store.ListMediaMetadataByMediaItemIDs(ids)
	if err != nil {
		return nil, err
	}
	metaMap := make(map[uint]*store.MediaMetadata, len(metas))
	for i := range metas {
		metaMap[metas[i].MediaItemID] = &metas[i]
	}

	apiItems := make([]MediaItem, len(items))
	for i, item := range items {
		apiItems[i] = mediaItemToAPI(&item, metaMap[item.ID])
	}

	return ListMediaItems200JSONResponse{
		Items: apiItems,
		Total: len(apiItems),
	}, nil
}

func (h *Handlers) TriggerSync(_ context.Context, req TriggerSyncRequestObject) (TriggerSyncResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TriggerSync404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	job, err := h.queue.Enqueue(jobqueue.JobTypeSyncLibrary, lib.ID, lib.Name)
	if err != nil {
		return TriggerSync409JSONResponse{
			Code:    http.StatusConflict,
			Message: err.Error(),
		}, nil
	}

	return TriggerSync202JSONResponse(jobToAPI(job)), nil
}

func (h *Handlers) TriggerMatch(_ context.Context, req TriggerMatchRequestObject) (TriggerMatchResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TriggerMatch404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	opts := jobqueue.EnqueueOpts{}
	if req.Params.FullRematch != nil && *req.Params.FullRematch {
		opts.FullRematch = true
	}

	job, err := h.queue.Enqueue(jobqueue.JobTypeMatchLibrary, lib.ID, lib.Name, opts)
	if err != nil {
		return TriggerMatch409JSONResponse{
			Code:    http.StatusConflict,
			Message: err.Error(),
		}, nil
	}

	return TriggerMatch202JSONResponse(jobToAPI(job)), nil
}

func (h *Handlers) SearchMediaForLibrary(_ context.Context, req SearchMediaForLibraryRequestObject) (SearchMediaForLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return SearchMediaForLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	candidates, err := h.matchSvc.SearchForLibrary(lib, req.Params.Query)
	if err != nil {
		return nil, err
	}

	return SearchMediaForLibrary200JSONResponse{Candidates: candidatesToAPI(candidates)}, nil
}

func (h *Handlers) AddMediaToLibrary(_ context.Context, req AddMediaToLibraryRequestObject) (AddMediaToLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return AddMediaToLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	addReq := matching.AddMediaRequest{
		Source:     string(req.Body.Source),
		ExternalID: req.Body.ExternalId,
		Monitored:  req.Body.Monitored,
	}
	if req.Body.MediaProfileId != nil {
		pid := uint(*req.Body.MediaProfileId)
		addReq.MediaProfileID = &pid
	}
	if req.Body.SeasonMonitors != nil {
		for _, sm := range *req.Body.SeasonMonitors {
			addReq.SeasonMonitors = append(addReq.SeasonMonitors, matching.SeasonMonitorReq{
				SeasonNumber: sm.SeasonNumber,
				Monitored:    sm.Monitored,
			})
		}
	}

	item, meta, err := h.matchSvc.AddMediaToLibraryFull(h.store, lib, addReq)
	if err != nil {
		if errors.Is(err, matching.ErrAlreadyExists) {
			return AddMediaToLibrary409JSONResponse{
				Code:    http.StatusConflict,
				Message: err.Error(),
			}, nil
		}
		return nil, err
	}

	return AddMediaToLibrary201JSONResponse(mediaItemToAPI(item, meta)), nil
}

func (h *Handlers) GlobalSearch(_ context.Context, req GlobalSearchRequestObject) (GlobalSearchResponseObject, error) {
	candidates, err := h.matchSvc.SearchCandidates(req.Params.Query, string(req.Params.MediaType), nil, "")
	if err != nil {
		return nil, err
	}

	return GlobalSearch200JSONResponse{Candidates: candidatesToAPI(candidates)}, nil
}

func (h *Handlers) GetExternalMediaDetail(_ context.Context, req GetExternalMediaDetailRequestObject) (GetExternalMediaDetailResponseObject, error) {
	detail, err := h.matchSvc.GetExternalDetail(string(req.Source), string(req.Params.MediaType), req.ExternalId)
	if err != nil {
		return nil, err
	}

	apiDetail := ExternalMediaDetail{
		Source:     ExternalMediaDetailSource(detail.Source),
		ExternalId: detail.ExternalID,
		Title:      detail.Title,
		MediaType:  ExternalMediaDetailMediaType(detail.MediaType),
	}
	if detail.Overview != "" {
		apiDetail.Overview = &detail.Overview
	}
	if detail.PosterURL != "" {
		apiDetail.PosterUrl = &detail.PosterURL
	}
	if detail.Year != nil {
		apiDetail.Year = detail.Year
	}
	if detail.Genres != "" {
		apiDetail.Genres = &detail.Genres
	}
	if detail.Status != "" {
		apiDetail.Status = &detail.Status
	}
	if detail.Runtime != nil {
		apiDetail.Runtime = detail.Runtime
	}
	if detail.Seasons != nil {
		apiDetail.Seasons = detail.Seasons
	}
	if detail.ImdbID != "" {
		apiDetail.ImdbId = &detail.ImdbID
	}
	if detail.Credits != "" {
		var credits []CreditPerson
		if err := json.Unmarshal([]byte(detail.Credits), &credits); err == nil {
			apiDetail.Credits = &credits
		}
	}

	return GetExternalMediaDetail200JSONResponse(apiDetail), nil
}

func (h *Handlers) GetExternalEpisodes(_ context.Context, req GetExternalEpisodesRequestObject) (GetExternalEpisodesResponseObject, error) {
	data, err := h.matchSvc.FetchExternalEpisodes(req.Source, req.ExternalId, req.Params.SeasonCount)
	if err != nil {
		return nil, err
	}

	seasons := make([]ExternalSeasonSummary, len(data))
	for i, s := range data {
		episodes := make([]ExternalEpisode, len(s.Episodes))
		for j, ep := range s.Episodes {
			episodes[j] = ExternalEpisode{
				SeasonNumber:  ep.SeasonNumber,
				EpisodeNumber: ep.EpisodeNumber,
			}
			if ep.Title != "" {
				episodes[j].Title = &ep.Title
			}
			if ep.AirDate != "" {
				episodes[j].AirDate = &ep.AirDate
			}
			if ep.Runtime > 0 {
				r := ep.Runtime
				episodes[j].Runtime = &r
			}
		}
		seasons[i] = ExternalSeasonSummary{
			SeasonNumber:  s.SeasonNumber,
			TotalEpisodes: s.TotalEpisodes,
			Episodes:      episodes,
		}
	}

	return GetExternalEpisodes200JSONResponse{Seasons: seasons}, nil
}

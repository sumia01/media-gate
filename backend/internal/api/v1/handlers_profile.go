package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) ListMediaProfiles(_ context.Context, _ ListMediaProfilesRequestObject) (ListMediaProfilesResponseObject, error) {
	profiles, err := h.store.ListMediaProfiles()
	if err != nil {
		return nil, err
	}

	apiProfiles := make([]MediaProfile, len(profiles))
	for i := range profiles {
		apiProfiles[i] = mediaProfileToAPI(&profiles[i])
	}

	return ListMediaProfiles200JSONResponse{Profiles: apiProfiles}, nil
}

func (h *Handlers) CreateMediaProfile(_ context.Context, req CreateMediaProfileRequestObject) (CreateMediaProfileResponseObject, error) {
	profile := &store.MediaProfile{}
	applyProfileFields(profile, req.Body.Name, req.Body.Resolutions, req.Body.Languages, req.Body.Sources, req.Body.ExcludeTags)

	if err := h.store.CreateMediaProfile(profile); err != nil {
		return CreateMediaProfile400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateMediaProfile201JSONResponse(mediaProfileToAPI(profile)), nil
}

func (h *Handlers) GetMediaProfile(_ context.Context, req GetMediaProfileRequestObject) (GetMediaProfileResponseObject, error) {
	profile, err := h.store.GetMediaProfile(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetMediaProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media profile not found",
			}, nil
		}
		return nil, err
	}

	return GetMediaProfile200JSONResponse(mediaProfileToAPI(profile)), nil
}

func (h *Handlers) UpdateMediaProfile(_ context.Context, req UpdateMediaProfileRequestObject) (UpdateMediaProfileResponseObject, error) {
	profile, err := h.store.GetMediaProfile(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateMediaProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media profile not found",
			}, nil
		}
		return nil, err
	}

	applyProfileFields(profile, req.Body.Name, req.Body.Resolutions, req.Body.Languages, req.Body.Sources, req.Body.ExcludeTags)

	if err := h.store.UpdateMediaProfile(profile); err != nil {
		return nil, err
	}

	return UpdateMediaProfile200JSONResponse(mediaProfileToAPI(profile)), nil
}

func (h *Handlers) DeleteMediaProfile(_ context.Context, req DeleteMediaProfileRequestObject) (DeleteMediaProfileResponseObject, error) {
	if err := h.store.DeleteMediaProfile(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteMediaProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media profile not found",
			}, nil
		}
		return nil, err
	}

	return DeleteMediaProfile204Response{}, nil
}

func (h *Handlers) TestMediaProfileSearch(ctx context.Context, req TestMediaProfileSearchRequestObject) (TestMediaProfileSearchResponseObject, error) {
	profile, err := h.store.GetMediaProfile(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TestMediaProfileSearch404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media profile not found",
			}, nil
		}
		return nil, err
	}

	searchType := "movie-search"
	if req.Params.MediaType == TestMediaProfileSearchParamsMediaTypeSeries {
		searchType = "tv-search"
	}

	params := indexer.SearchParams{
		Query: req.Params.Query,
		Type:  searchType,
		Limit: 150,
	}
	if req.Params.Season != nil {
		params.Season = *req.Params.Season
	}

	results, err := h.indexerSvc.Search(ctx, params)
	if err != nil {
		return nil, err
	}

	var globalTags []string
	if raw, err := h.settings.Get(settings.KeyGlobalExcludeTags); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &globalTags)
	}
	filtered := indexer.FilterByMediaProfile(results, profile, globalTags...)

	apiResults := make([]TorrentResult, len(filtered))
	for i := range filtered {
		apiResults[i] = torrentResultToAPI(&filtered[i])
	}

	return TestMediaProfileSearch200JSONResponse{
		ProfileName:   profile.Name,
		TotalResults:  len(results),
		FilteredCount: len(filtered),
		Results:       apiResults,
	}, nil
}

package apiv1

import (
	"context"
	"errors"
	"net/http"

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
	profile := mediaProfileFromAPI(req.Body.Name, req.Body.Resolutions, req.Body.Languages, req.Body.Sources, req.Body.ExcludeTags)

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

	updateMediaProfileFromAPI(profile, req.Body.Name, req.Body.Resolutions, req.Body.Languages, req.Body.Sources, req.Body.ExcludeTags)

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

package apiv1

import (
	"context"
	"errors"
	"net/http"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/watched"
)

func (h *Handlers) ListWatched(ctx context.Context, _ ListWatchedRequestObject) (ListWatchedResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	items, err := h.watchedSvc.List(userID)
	if err != nil {
		return nil, err
	}

	apiItems := make([]WatchedItem, len(items))
	for i, item := range items {
		apiItems[i] = watchedItemToAPI(&item)
	}
	return ListWatched200JSONResponse{Items: apiItems}, nil
}

func (h *Handlers) CreateWatched(ctx context.Context, req CreateWatchedRequestObject) (CreateWatchedResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}

	if req.Body == nil {
		return nil, errors.New("watched item body is required")
	}

	item := &store.WatchedItem{
		Source:     string(req.Body.Source),
		ExternalID: req.Body.ExternalId,
		ImdbID:     derefString(req.Body.ImdbId),
		Title:      req.Body.Title,
		MediaType:  string(req.Body.MediaType),
		Year:       req.Body.Year,
		PosterPath: derefString(req.Body.PosterPath),
	}
	if req.Body.MediaItemId != nil {
		if *req.Body.MediaItemId <= 0 {
			return nil, watched.ErrMediaItemMismatch
		}
		id := uint(*req.Body.MediaItemId)
		item.MediaItemID = &id
	}
	item, err := h.watchedSvc.Create(userID, item)
	if errors.Is(err, store.ErrDuplicate) {
		return CreateWatched409JSONResponse{Code: http.StatusConflict, Message: "already marked as watched"}, nil
	}
	if err != nil {
		return nil, err
	}
	resp := CreateWatched201JSONResponse(watchedItemToAPI(item))
	return resp, nil
}

func (h *Handlers) DeleteWatched(ctx context.Context, req DeleteWatchedRequestObject) (DeleteWatchedResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	if req.Id <= 0 {
		return DeleteWatched404JSONResponse{Code: http.StatusNotFound, Message: "watched item not found"}, nil
	}
	if err := h.watchedSvc.Delete(userID, uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, watched.ErrForbidden) {
			return DeleteWatched404JSONResponse{Code: http.StatusNotFound, Message: "watched item not found"}, nil
		}
		return nil, err
	}
	return DeleteWatched204Response{}, nil
}

func (h *Handlers) CheckWatched(ctx context.Context, req CheckWatchedRequestObject) (CheckWatchedResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	item, err := h.watchedSvc.Check(userID, string(req.Params.Source), string(req.Params.MediaType), req.Params.ExternalId)
	if errors.Is(err, store.ErrNotFound) {
		return CheckWatched200JSONResponse{Watched: false}, nil
	}
	if err != nil {
		return nil, err
	}
	id := int64(item.ID)
	return CheckWatched200JSONResponse{Watched: true, Id: &id}, nil
}

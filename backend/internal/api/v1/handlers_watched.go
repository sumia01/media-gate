package apiv1

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) ListWatched(ctx context.Context, _ ListWatchedRequestObject) (ListWatchedResponseObject, error) {
	mode := h.settings.GetWithDefault(settings.KeyWatchedListMode, "global")

	var items []store.WatchedItem
	var err error
	if mode == "per_user" {
		userID, ok := auth.UserIDFromContext(ctx)
		if !ok {
			return nil, errors.New("unauthenticated")
		}
		items, err = h.store.ListWatchedItemsByUser(userID)
	} else {
		items, err = h.store.ListWatchedItems()
	}
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

	mode := h.settings.GetWithDefault(settings.KeyWatchedListMode, "global")

	// Check for duplicate.
	var lookupUser *uint
	if mode == "per_user" {
		lookupUser = &userID
	}
	_, err := h.store.GetWatchedBySourceExternal(lookupUser, string(req.Body.Source), req.Body.ExternalId)
	if err == nil {
		return CreateWatched409JSONResponse{Code: http.StatusConflict, Message: "already marked as watched"}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	item := &store.WatchedItem{
		UserID:     userID,
		Source:     string(req.Body.Source),
		ExternalID: req.Body.ExternalId,
		ImdbID:     derefString(req.Body.ImdbId),
		Title:      req.Body.Title,
		MediaType:  string(req.Body.MediaType),
		Year:       req.Body.Year,
		PosterPath: derefString(req.Body.PosterPath),
		WatchedAt:  time.Now(),
	}
	if req.Body.MediaItemId != nil {
		id := uint(*req.Body.MediaItemId)
		item.MediaItemID = &id
	}
	if err := h.store.CreateWatchedItem(item); err != nil {
		return nil, err
	}
	resp := CreateWatched201JSONResponse(watchedItemToAPI(item))
	return resp, nil
}

func (h *Handlers) DeleteWatched(ctx context.Context, req DeleteWatchedRequestObject) (DeleteWatchedResponseObject, error) {
	if err := h.store.DeleteWatchedItem(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteWatched404JSONResponse{Code: http.StatusNotFound, Message: "watched item not found"}, nil
		}
		return nil, err
	}
	return DeleteWatched204Response{}, nil
}

func (h *Handlers) CheckWatched(ctx context.Context, req CheckWatchedRequestObject) (CheckWatchedResponseObject, error) {
	mode := h.settings.GetWithDefault(settings.KeyWatchedListMode, "global")

	var lookupUser *uint
	if mode == "per_user" {
		userID, ok := auth.UserIDFromContext(ctx)
		if !ok {
			return nil, errors.New("unauthenticated")
		}
		lookupUser = &userID
	}

	item, err := h.store.GetWatchedBySourceExternal(lookupUser, string(req.Params.Source), req.Params.ExternalId)
	if errors.Is(err, store.ErrNotFound) {
		return CheckWatched200JSONResponse{Watched: false}, nil
	}
	if err != nil {
		return nil, err
	}
	id := int64(item.ID)
	return CheckWatched200JSONResponse{Watched: true, Id: &id}, nil
}

package apiv1

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/watched"
)

type watchedCursor struct {
	Version   int        `json:"v"`
	ID        int64      `json:"id"`
	WatchedAt *time.Time `json:"at"`
}

func (h *Handlers) ListWatched(ctx context.Context, req ListWatchedRequestObject) (ListWatchedResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	var items []store.WatchedItem
	var err error
	response := ListWatched200JSONResponse{}
	if req.Params.Limit == nil {
		if req.Params.Before != nil {
			return ListWatched400JSONResponse{Code: 400, Message: "cursor requires limit"}, nil
		}
		items, err = h.watchedSvc.List(userID)
	} else {
		if *req.Params.Limit < 1 || *req.Params.Limit > store.MaxWatchedPageSize {
			return ListWatched400JSONResponse{Code: 400, Message: "limit must be between 1 and 100"}, nil
		}
		var before *store.WatchedItemCursor
		if req.Params.Before != nil {
			before, err = decodeWatchedCursor(*req.Params.Before)
			if err != nil {
				return ListWatched400JSONResponse{Code: 400, Message: "invalid watched cursor"}, nil
			}
		}
		var hasMore bool
		items, hasMore, err = h.watchedSvc.ListPage(userID, before, *req.Params.Limit)
		response.HasMore = &hasMore
		if err == nil && hasMore && len(items) > 0 {
			last := items[len(items)-1]
			if uint64(last.ID) > uint64(maxSQLiteID) {
				return nil, errors.New("watched id exceeds SQLite integer range")
			}
			payload, encodeErr := json.Marshal(watchedCursor{Version: 1, ID: int64(last.ID), WatchedAt: &last.WatchedAt})
			if encodeErr != nil {
				return nil, encodeErr
			}
			cursor := base64.RawURLEncoding.EncodeToString(payload)
			response.NextCursor = &cursor
		}
	}
	if err != nil {
		return nil, err
	}

	apiItems := make([]WatchedItem, len(items))
	for i, item := range items {
		apiItems[i] = watchedItemToAPI(&item)
	}
	response.Items = apiItems
	return response, nil
}

func decodeWatchedCursor(encoded string) (*store.WatchedItemCursor, error) {
	if encoded == "" || len(encoded) > 256 {
		return nil, errors.New("invalid watched cursor length")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("invalid watched cursor encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor watchedCursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid watched cursor payload")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid trailing watched cursor data")
	}
	if cursor.Version != 1 || cursor.ID <= 0 || cursor.WatchedAt == nil {
		return nil, errors.New("unsupported watched cursor")
	}
	return &store.WatchedItemCursor{WatchedAt: *cursor.WatchedAt, ID: uint(cursor.ID)}, nil
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

package apiv1

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
)

const (
	mediaActivityDefaultLimit = 30
	mediaActivityMaxLimit     = 100
	mediaActivityMaxCursor    = 256
	mediaActivityCursorV1     = 1
	maxSQLiteID               = int64(^uint64(0) >> 1)
)

type mediaActivityCursor struct {
	Version     int   `json:"v"`
	MediaItemID int64 `json:"mediaId"`
	BeforeID    int64 `json:"beforeId"`
}

func (h *Handlers) GetMediaActivity(ctx context.Context, req GetMediaActivityRequestObject) (GetMediaActivityResponseObject, error) {
	if req.Id <= 0 {
		return GetMediaActivity404JSONResponse{Code: 404, Message: "media item not found"}, nil
	}
	mediaItemID := uint(req.Id)
	if _, err := h.store.GetMediaItem(mediaItemID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetMediaActivity404JSONResponse{Code: 404, Message: "media item not found"}, nil
		}
		return nil, err
	}

	limit := mediaActivityDefaultLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > mediaActivityMaxLimit {
		return invalidMediaActivityRequest("limit must be between 1 and 100"), nil
	}

	var beforeID *uint
	if req.Params.Before != nil {
		cursor, err := decodeMediaActivityCursor(*req.Params.Before)
		if err != nil || cursor.MediaItemID != req.Id {
			return invalidMediaActivityRequest("invalid activity cursor"), nil
		}
		before := uint(cursor.BeforeID)
		beforeID = &before
	}

	viewerID, ok := auth.UserIDFromContext(ctx)
	if !ok || viewerID == 0 {
		return nil, errors.New("authenticated activity viewer missing from context")
	}
	rows, hasMore, err := h.store.ListMediaActivityPage(mediaItemID, viewerID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]MediaActivity, 0, len(rows))
	for i := range rows {
		items = append(items, mediaActivityToAPI(&rows[i]))
	}
	response := GetMediaActivity200JSONResponse{Items: items, HasMore: hasMore}
	if hasMore && len(rows) > 0 {
		if uint64(rows[len(rows)-1].ID) > uint64(maxSQLiteID) {
			return nil, errors.New("activity id exceeds SQLite integer range")
		}
		cursor, err := encodeMediaActivityCursor(mediaActivityCursor{
			Version: mediaActivityCursorV1, MediaItemID: req.Id, BeforeID: int64(rows[len(rows)-1].ID),
		})
		if err != nil {
			return nil, err
		}
		response.NextCursor = &cursor
	}
	return response, nil
}

func invalidMediaActivityRequest(message string) GetMediaActivity400JSONResponse {
	return GetMediaActivity400JSONResponse{Code: 400, Message: message}
}

func encodeMediaActivityCursor(cursor mediaActivityCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encoding activity cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeMediaActivityCursor(encoded string) (mediaActivityCursor, error) {
	if encoded == "" || len(encoded) > mediaActivityMaxCursor {
		return mediaActivityCursor{}, errors.New("invalid cursor length")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 || len(payload) > mediaActivityMaxCursor {
		return mediaActivityCursor{}, errors.New("invalid cursor encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor mediaActivityCursor
	if err := decoder.Decode(&cursor); err != nil {
		return mediaActivityCursor{}, errors.New("invalid cursor payload")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return mediaActivityCursor{}, errors.New("invalid trailing cursor data")
	}
	if cursor.Version != mediaActivityCursorV1 || cursor.MediaItemID <= 0 || cursor.BeforeID <= 0 {
		return mediaActivityCursor{}, errors.New("unsupported cursor")
	}
	return cursor, nil
}

func mediaActivityToAPI(row *store.MediaActivityAttribution) MediaActivity {
	actor := MediaActivityActor{Kind: MediaActivityActorKind(row.ActorKind)}
	if row.ActorKind == store.MediaActivityActorSystem {
		actor.Name = row.ActorComponent
		actor.Component = &row.ActorComponent
	} else {
		name := strings.TrimSpace(row.FirstName + " " + row.LastName)
		if name == "" {
			name = row.Email
		}
		if name == "" {
			name = "Deleted user"
		}
		actor.Name = name
		if row.ActorUserID != nil {
			id := int64(*row.ActorUserID)
			actor.UserId = &id
		}
	}
	result := MediaActivity{
		Id: int64(row.ID), RecordedAt: row.RecordedAt, Actor: actor,
		Action: row.Action, OperationId: row.OperationID, MediaTitle: row.MediaTitle,
		DetailsVersion: row.DetailsVersion,
	}
	if row.DetailsVersion == store.MediaActivityDetailsVersion {
		var details MediaActivityDetails
		if json.Unmarshal([]byte(row.Details), &details) == nil {
			result.Details = &details
		}
	}
	return result
}

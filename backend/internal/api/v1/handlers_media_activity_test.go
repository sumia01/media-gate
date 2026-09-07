package apiv1

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
)

type mediaActivityHandlerStore struct {
	store.Store
	itemErr error
	rows    []store.MediaActivityAttribution
	hasMore bool
	listFn  func(mediaItemID, viewerUserID uint, beforeID *uint, limit int) ([]store.MediaActivityAttribution, bool, error)

	getItemCalls int
	listCalls    int
	mediaItemID  uint
	viewerUserID uint
	beforeID     *uint
	limit        int
}

func (s *mediaActivityHandlerStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	s.getItemCalls++
	if s.itemErr != nil {
		return nil, s.itemErr
	}
	return &store.MediaItem{ID: id, Title: "Test title"}, nil
}

func (s *mediaActivityHandlerStore) ListMediaActivityPage(mediaItemID, viewerUserID uint, beforeID *uint, limit int) ([]store.MediaActivityAttribution, bool, error) {
	s.listCalls++
	s.mediaItemID = mediaItemID
	s.viewerUserID = viewerUserID
	s.limit = limit
	if beforeID != nil {
		before := *beforeID
		s.beforeID = &before
	} else {
		s.beforeID = nil
	}
	if s.listFn != nil {
		return s.listFn(mediaItemID, viewerUserID, beforeID, limit)
	}
	return s.rows, s.hasMore, nil
}

func mediaActivityContext(userID uint) context.Context {
	return auth.ContextWithUserID(context.Background(), userID)
}

func systemActivityRow(id uint) store.MediaActivityAttribution {
	return store.MediaActivityAttribution{MediaActivity: store.MediaActivity{
		ID: id, MediaItemID: 7, RecordedAt: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC),
		ActorKind: store.MediaActivityActorSystem, ActorComponent: "monitor",
		Action: store.MediaActivityActionMonitoringChanged, OperationID: "operation", MediaTitle: "Test title",
		DetailsVersion: store.MediaActivityDetailsVersion, Details: `{}`,
	}}
}

func TestGetMediaActivityLimitsAndEmptyItem(t *testing.T) {
	for _, tc := range []struct {
		name      string
		limit     *int
		wantLimit int
	}{
		{name: "default", wantLimit: mediaActivityDefaultLimit},
		{name: "explicit", limit: activityLimit(7), wantLimit: 7},
		{name: "maximum", limit: activityLimit(mediaActivityMaxLimit), wantLimit: mediaActivityMaxLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &mediaActivityHandlerStore{}
			response, err := (&Handlers{store: st}).GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{
				Id: 7, Params: GetMediaActivityParams{Limit: tc.limit},
			})
			if err != nil {
				t.Fatal(err)
			}
			page, ok := response.(GetMediaActivity200JSONResponse)
			if !ok {
				t.Fatalf("response = %#v", response)
			}
			if page.Items == nil || len(page.Items) != 0 || page.HasMore || page.NextCursor != nil {
				t.Fatalf("empty page = %#v", page)
			}
			if st.listCalls != 1 || st.mediaItemID != 7 || st.viewerUserID != 42 || st.beforeID != nil || st.limit != tc.wantLimit {
				t.Fatalf("store call = calls %d, media %d, viewer %d, before %v, limit %d", st.listCalls, st.mediaItemID, st.viewerUserID, st.beforeID, st.limit)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		limit int
	}{
		{name: "negative", limit: -1},
		{name: "zero", limit: 0},
		{name: "over maximum", limit: mediaActivityMaxLimit + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &mediaActivityHandlerStore{}
			response, err := (&Handlers{store: st}).GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{
				Id: 7, Params: GetMediaActivityParams{Limit: &tc.limit},
			})
			if err != nil {
				t.Fatal(err)
			}
			bad, ok := response.(GetMediaActivity400JSONResponse)
			if !ok || bad.Code != http.StatusBadRequest || bad.Message == "" {
				t.Fatalf("response = %#v", response)
			}
			if st.listCalls != 0 {
				t.Fatalf("invalid limit reached activity query %d times", st.listCalls)
			}
		})
	}
}

func TestGetMediaActivityDelegatesLimitPlusOnePaging(t *testing.T) {
	st := &mediaActivityHandlerStore{}
	st.listFn = func(_, _ uint, _ *uint, limit int) ([]store.MediaActivityAttribution, bool, error) {
		lookahead := []store.MediaActivityAttribution{systemActivityRow(5), systemActivityRow(4), systemActivityRow(3)}
		if len(lookahead) <= limit {
			return lookahead, false, nil
		}
		return lookahead[:limit], true, nil
	}
	limit := 2
	response, err := (&Handlers{store: st}).GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{
		Id: 7, Params: GetMediaActivityParams{Limit: &limit},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := response.(GetMediaActivity200JSONResponse)
	if st.limit != limit || len(page.Items) != limit || !page.HasMore || page.NextCursor == nil {
		t.Fatalf("store limit %d, page %#v", st.limit, page)
	}
	cursor, err := decodeMediaActivityCursor(*page.NextCursor)
	if err != nil || cursor.MediaItemID != 7 || cursor.BeforeID != 4 {
		t.Fatalf("next cursor = %+v, %v", cursor, err)
	}
}

func TestGetMediaActivityMissingAndInvalidItem(t *testing.T) {
	for _, id := range []int64{0, -1} {
		st := &mediaActivityHandlerStore{}
		response, err := (&Handlers{store: st}).GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{Id: id})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := response.(GetMediaActivity404JSONResponse); !ok {
			t.Fatalf("id %d response = %#v", id, response)
		}
		if st.getItemCalls != 0 || st.listCalls != 0 {
			t.Fatalf("id %d reached store: get %d, list %d", id, st.getItemCalls, st.listCalls)
		}
	}

	st := &mediaActivityHandlerStore{itemErr: store.ErrNotFound}
	response, err := (&Handlers{store: st}).GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{Id: 999})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(GetMediaActivity404JSONResponse); !ok {
		t.Fatalf("missing item response = %#v", response)
	}
	if st.getItemCalls != 1 || st.listCalls != 0 {
		t.Fatalf("missing item store calls: get %d, list %d", st.getItemCalls, st.listCalls)
	}
}

func TestGetMediaActivityRejectsInvalidCursors(t *testing.T) {
	unknownVersion, err := encodeMediaActivityCursor(mediaActivityCursor{Version: 2, MediaItemID: 7, BeforeID: 9})
	if err != nil {
		t.Fatal(err)
	}
	crossItem, err := encodeMediaActivityCursor(mediaActivityCursor{Version: mediaActivityCursorV1, MediaItemID: 8, BeforeID: 9})
	if err != nil {
		t.Fatal(err)
	}
	trailing := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"mediaId":7,"beforeId":9} {}`))
	unknownField := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"mediaId":7,"beforeId":9,"extra":true}`))
	aboveMaxMediaID := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"mediaId":9223372036854775808,"beforeId":9}`))
	aboveMaxBeforeID := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"mediaId":7,"beforeId":9223372036854775808}`))
	negativeBeforeID := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"mediaId":7,"beforeId":-1}`))
	missingBeforeID := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"mediaId":7}`))

	for _, tc := range []struct {
		name   string
		cursor string
	}{
		{name: "malformed", cursor: "%%%"},
		{name: "oversized", cursor: strings.Repeat("a", mediaActivityMaxCursor+1)},
		{name: "trailing data", cursor: trailing},
		{name: "unknown field", cursor: unknownField},
		{name: "media id above SQLite maximum", cursor: aboveMaxMediaID},
		{name: "before id above SQLite maximum", cursor: aboveMaxBeforeID},
		{name: "negative before id", cursor: negativeBeforeID},
		{name: "missing before id", cursor: missingBeforeID},
		{name: "unknown version", cursor: unknownVersion},
		{name: "cross item", cursor: crossItem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &mediaActivityHandlerStore{}
			response, err := (&Handlers{store: st}).GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{
				Id: 7, Params: GetMediaActivityParams{Before: &tc.cursor},
			})
			if err != nil {
				t.Fatal(err)
			}
			bad, ok := response.(GetMediaActivity400JSONResponse)
			if !ok || bad.Code != http.StatusBadRequest || bad.Message != "invalid activity cursor" {
				t.Fatalf("response = %#v", response)
			}
			if st.listCalls != 0 {
				t.Fatalf("invalid cursor reached activity query %d times", st.listCalls)
			}
		})
	}
}

func TestGetMediaActivityCursorIsStableAndExclusive(t *testing.T) {
	rows := []store.MediaActivityAttribution{
		systemActivityRow(12), systemActivityRow(11), systemActivityRow(10), systemActivityRow(9),
	}
	st := &mediaActivityHandlerStore{}
	st.listFn = func(_, _ uint, beforeID *uint, limit int) ([]store.MediaActivityAttribution, bool, error) {
		eligible := make([]store.MediaActivityAttribution, 0, len(rows))
		for _, row := range rows {
			if beforeID == nil || row.ID < *beforeID {
				eligible = append(eligible, row)
			}
		}
		hasMore := len(eligible) > limit
		if hasMore {
			eligible = eligible[:limit]
		}
		return eligible, hasMore, nil
	}
	h := &Handlers{store: st}
	limit := 2
	firstResponse, err := h.GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{
		Id: 7, Params: GetMediaActivityParams{Limit: &limit},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := firstResponse.(GetMediaActivity200JSONResponse)
	if len(first.Items) != 2 || first.Items[0].Id != 12 || first.Items[1].Id != 11 || first.NextCursor == nil {
		t.Fatalf("first page = %#v", first)
	}
	cursor, err := decodeMediaActivityCursor(*first.NextCursor)
	if err != nil || cursor.Version != mediaActivityCursorV1 || cursor.MediaItemID != 7 || cursor.BeforeID != 11 {
		t.Fatalf("cursor = %+v, %v", cursor, err)
	}

	rows = append([]store.MediaActivityAttribution{systemActivityRow(13)}, rows...)
	secondResponse, err := h.GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{
		Id: 7, Params: GetMediaActivityParams{Limit: &limit, Before: first.NextCursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	second := secondResponse.(GetMediaActivity200JSONResponse)
	if len(second.Items) != 2 || second.Items[0].Id != 10 || second.Items[1].Id != 9 || second.HasMore || second.NextCursor != nil {
		t.Fatalf("second page after insert = %#v", second)
	}
	if st.beforeID == nil || *st.beforeID != 11 {
		t.Fatalf("exclusive before id = %v", st.beforeID)
	}
}

func TestGetMediaActivityActorAndForwardCompatibleMapping(t *testing.T) {
	userID := uint(3)
	rows := []store.MediaActivityAttribution{
		{
			MediaActivity: store.MediaActivity{
				ID: 5, MediaItemID: 7, RecordedAt: time.Now().UTC(), ActorKind: store.MediaActivityActorUser, ActorUserID: &userID,
				Action: store.MediaActivityActionRequestMade, OperationID: "user", MediaTitle: "Original title",
				DetailsVersion: store.MediaActivityDetailsVersion, Details: `{"reason":"requested"}`,
			},
			FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.test",
		},
		{
			MediaActivity: store.MediaActivity{
				ID: 4, MediaItemID: 7, RecordedAt: time.Now().UTC(), ActorKind: store.MediaActivityActorUser,
				Action: store.MediaActivityActionRequestMade, OperationID: "deleted", MediaTitle: "Original title",
				DetailsVersion: store.MediaActivityDetailsVersion, Details: `{}`,
			},
		},
		systemActivityRow(3),
		{
			MediaActivity: store.MediaActivity{
				ID: 2, MediaItemID: 7, RecordedAt: time.Now().UTC(), ActorKind: store.MediaActivityActorSystem, ActorComponent: "future-worker",
				Action: "future.action", OperationID: "future", MediaTitle: "Historical title",
				DetailsVersion: store.MediaActivityDetailsVersion + 1, Details: `{"newShape":true}`,
			},
		},
	}
	st := &mediaActivityHandlerStore{rows: rows}
	response, err := (&Handlers{store: st}).GetMediaActivity(mediaActivityContext(42), GetMediaActivityRequestObject{Id: 7})
	if err != nil {
		t.Fatal(err)
	}
	items := response.(GetMediaActivity200JSONResponse).Items
	if len(items) != 4 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Actor.Kind != User || items[0].Actor.Name != "Ada Lovelace" || items[0].Actor.UserId == nil || *items[0].Actor.UserId != int64(userID) || items[0].Actor.Component != nil {
		t.Fatalf("named user actor = %#v", items[0].Actor)
	}
	if items[0].Details == nil || items[0].Details.Reason == nil || *items[0].Details.Reason != "requested" {
		t.Fatalf("version 1 details = %#v", items[0].Details)
	}
	if items[1].Actor.Kind != User || items[1].Actor.Name != "Deleted user" || items[1].Actor.UserId != nil {
		t.Fatalf("deleted user actor = %#v", items[1].Actor)
	}
	if items[2].Actor.Kind != System || items[2].Actor.Name != "monitor" || items[2].Actor.Component == nil || *items[2].Actor.Component != "monitor" || items[2].Actor.UserId != nil {
		t.Fatalf("system actor = %#v", items[2].Actor)
	}
	if items[3].Action != "future.action" || items[3].DetailsVersion != store.MediaActivityDetailsVersion+1 || items[3].Details != nil || items[3].MediaTitle != "Historical title" {
		t.Fatalf("forward-compatible row = %#v", items[3])
	}
}

func TestGetMediaActivityEndpointRequiresAuthButNotAdmin(t *testing.T) {
	st := &mediaActivityHandlerStore{}
	authSvc := auth.NewService(st, "test-signing-key")
	token, err := authSvc.GenerateAccessToken(&store.User{ID: 7, Email: "reader@example.test", IsAdmin: false})
	if err != nil {
		t.Fatal(err)
	}
	routes := HandlerWithOptions(
		NewStrictHandler(&Handlers{store: st}, []StrictMiddlewareFunc{AdminMiddleware(authSvc)}),
		StdHTTPServerOptions{BaseURL: "/api/v1", ErrorHandlerFunc: RequestErrorHandler},
	)
	handler := auth.AuthMiddleware(authSvc)(routes)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/media/7/activity", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d: %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/media/7/activity", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("non-admin status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if st.viewerUserID != 7 {
		t.Fatalf("activity viewer = %d", st.viewerUserID)
	}
}

func activityLimit(value int) *int {
	return &value
}

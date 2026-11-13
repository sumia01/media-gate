package apiv1

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	"github.com/sumia01/media-gate/internal/watched"
)

type watchedModeHandlerStore struct {
	store.Store
	modeValue  string
	modeErr    error
	listCalls  int
	checkCalls int
}

func (s *watchedModeHandlerStore) GetSetting(string) (*store.Setting, error) {
	if s.modeErr != nil {
		return nil, s.modeErr
	}
	return &store.Setting{Key: settings.KeyWatchedListMode, Value: s.modeValue}, nil
}

func (s *watchedModeHandlerStore) ListWatchedItems() ([]store.WatchedItem, error) {
	s.listCalls++
	return nil, nil
}

func (s *watchedModeHandlerStore) ListWatchedItemsByUser(uint) ([]store.WatchedItem, error) {
	s.listCalls++
	return nil, nil
}

func (s *watchedModeHandlerStore) ListWatchedItemsPage(*uint, *store.WatchedItemCursor, int) ([]store.WatchedItem, bool, error) {
	s.listCalls++
	return nil, false, nil
}

func (s *watchedModeHandlerStore) GetWatchedBySourceExternal(*uint, string, string, int) (*store.WatchedItem, error) {
	s.checkCalls++
	return nil, store.ErrNotFound
}

func TestWatchedHandlersUseExactMediaTypeIdentity(t *testing.T) {
	st, err := sqlite.New(filepath.Join(t.TempDir(), "handlers-watched.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	user := &store.User{Email: "watched-handler@example.com", PasswordHash: "hash"}
	if err := st.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	settingsSvc := settings.NewService(st, t.TempDir(), nil, "", nil)
	h := &Handlers{store: st, settings: settingsSvc, watchedSvc: watched.NewService(st, nil)}
	ctx := auth.ContextWithUserID(t.Context(), user.ID)

	create := func(mediaType WatchedItemCreateMediaType) CreateWatchedResponseObject {
		t.Helper()
		response, err := h.CreateWatched(ctx, CreateWatchedRequestObject{Body: &CreateWatchedJSONRequestBody{
			Source: "tmdb", MediaType: mediaType, ExternalId: 42, Title: string(mediaType),
		}})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	movie := create("movie")
	series := create("series")
	if _, ok := movie.(CreateWatched201JSONResponse); !ok {
		t.Fatalf("movie create response = %T", movie)
	}
	if _, ok := series.(CreateWatched201JSONResponse); !ok {
		t.Fatalf("series create response = %T", series)
	}
	if _, ok := create("movie").(CreateWatched409JSONResponse); !ok {
		t.Fatalf("exact duplicate did not return conflict")
	}

	for _, tc := range []struct {
		mediaType CheckWatchedParamsMediaType
		wantID    int64
	}{
		{mediaType: "movie", wantID: int64(movie.(CreateWatched201JSONResponse).Id)},
		{mediaType: "series", wantID: int64(series.(CreateWatched201JSONResponse).Id)},
	} {
		response, err := h.CheckWatched(ctx, CheckWatchedRequestObject{Params: CheckWatchedParams{
			Source: "tmdb", ExternalId: 42, MediaType: tc.mediaType,
		}})
		if err != nil {
			t.Fatal(err)
		}
		checked := response.(CheckWatched200JSONResponse)
		if !checked.Watched || checked.Id == nil || *checked.Id != tc.wantID {
			t.Fatalf("%s check = %+v, want id %d", tc.mediaType, checked, tc.wantID)
		}
	}
}

func TestWatchedListAndCheckHandlersFailClosedOnModeErrors(t *testing.T) {
	modeErr := errors.New("mode unavailable")
	for _, tc := range []struct {
		name      string
		modeValue string
		modeErr   error
		want      error
	}{
		{name: "database error", modeErr: modeErr, want: modeErr},
		{name: "unknown mode", modeValue: "public", want: watched.ErrInvalidMode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &watchedModeHandlerStore{modeValue: tc.modeValue, modeErr: tc.modeErr}
			h := &Handlers{store: st, watchedSvc: watched.NewService(st, nil)}
			ctx := auth.ContextWithUserID(t.Context(), 7)
			if _, err := h.ListWatched(ctx, ListWatchedRequestObject{}); !errors.Is(err, tc.want) {
				t.Fatalf("ListWatched error = %v, want %v", err, tc.want)
			}
			limit := 30
			if _, err := h.ListWatched(ctx, ListWatchedRequestObject{Params: ListWatchedParams{Limit: &limit}}); !errors.Is(err, tc.want) {
				t.Fatalf("paged ListWatched error = %v, want %v", err, tc.want)
			}
			if _, err := h.CheckWatched(ctx, CheckWatchedRequestObject{Params: CheckWatchedParams{
				Source: Tmdb, MediaType: CheckWatchedParamsMediaType("movie"), ExternalId: 42,
			}}); !errors.Is(err, tc.want) {
				t.Fatalf("CheckWatched error = %v, want %v", err, tc.want)
			}
			if st.listCalls != 0 || st.checkCalls != 0 {
				t.Fatalf("mode failure reached watched data: list=%d check=%d", st.listCalls, st.checkCalls)
			}
		})
	}
}

func TestWatchedPaginationThroughAuthenticatedRouter(t *testing.T) {
	st, err := sqlite.New(filepath.Join(t.TempDir(), "watched-pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	authSvc := auth.NewService(st, "watched-pages-test-key")
	users := []*store.User{{Email: "reader@watched.test", PasswordHash: "hash"}, {Email: "other@watched.test", PasswordHash: "hash"}}
	for _, user := range users {
		if err := st.CreateUser(user); err != nil {
			t.Fatal(err)
		}
	}
	token, err := authSvc.GenerateAccessToken(users[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(&store.Setting{Key: settings.KeyWatchedListMode, Value: "per_user"}); err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		for _, user := range users {
			if err := st.CreateWatchedItem(&store.WatchedItem{
				UserID: user.ID, Source: "tmdb", ExternalID: i + 1, MediaType: "movie", Title: fmt.Sprintf("User %d Movie %d", user.ID, i),
				WatchedAt: time.Date(2026, 11, 13, 10, 0, i/2, 0, time.UTC),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	h := &Handlers{store: st, watchedSvc: watched.NewService(st, nil)}
	router := auth.AuthMiddleware(authSvc)(HandlerWithOptions(NewStrictHandler(h, []StrictMiddlewareFunc{AdminMiddleware(authSvc)}), StdHTTPServerOptions{BaseURL: "/api/v1"}))
	request := func(query, accessToken string, status int) ListWatched200JSONResponse {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/watched"+query, nil)
		if accessToken != "" {
			r.Header.Set("Authorization", "Bearer "+accessToken)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("GET watched%s status=%d want=%d body=%s", query, w.Code, status, w.Body.String())
		}
		var page ListWatched200JSONResponse
		if status == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		return page
	}
	request("?limit=30", "", http.StatusUnauthorized)
	full := request("", token, http.StatusOK)
	if len(full.Items) != 40 || full.HasMore != nil || full.NextCursor != nil {
		t.Fatalf("unpaged badge response = %+v", full)
	}
	first := request("?limit=30", token, http.StatusOK)
	if len(first.Items) != 30 || first.HasMore == nil || !*first.HasMore || first.NextCursor == nil {
		t.Fatalf("first page = %+v", first)
	}
	for i := range first.Items {
		if first.Items[i].Id != full.Items[i].Id {
			t.Fatalf("first page order differs from full list at %d", i)
		}
	}
	// Cursor data must not depend on the anchor still existing in the DB.
	for _, item := range []WatchedItem{first.Items[0], first.Items[29]} {
		if err := st.DeleteWatchedItem(uint(item.Id)); err != nil {
			t.Fatal(err)
		}
	}
	second := request("?limit=30&before="+url.QueryEscape(*first.NextCursor), token, http.StatusOK)
	if len(second.Items) != 10 || second.HasMore == nil || *second.HasMore || second.NextCursor != nil {
		t.Fatalf("last page = %+v", second)
	}
	for i := range second.Items {
		if second.Items[i].Id != full.Items[30+i].Id {
			t.Fatalf("skipped/repeated/private item on final page at %d", i)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=-1", "?limit=101", "?limit=abc", "?before=abc", "?limit=30&before=", "?limit=30&before=abc", "?limit=30&before=" + strings.Repeat("x", 257)} {
		request(query, token, http.StatusBadRequest)
	}
	for _, payload := range []string{
		`{"v":2,"id":1,"at":"2026-11-13T10:00:00Z"}`,
		`{"v":1,"id":0,"at":"2026-11-13T10:00:00Z"}`,
		`{"v":1,"id":1}`,
		`{"v":1,"id":1,"at":"bad"}`,
		`{"v":1,"id":1,"at":"2026-11-13T10:00:00Z","extra":true}`,
		`{"v":1,"id":1,"at":"2026-11-13T10:00:00Z"} {}`,
	} {
		request("?limit=30&before="+base64.RawURLEncoding.EncodeToString([]byte(payload)), token, http.StatusBadRequest)
	}
	if err := st.SetSetting(&store.Setting{Key: settings.KeyWatchedListMode, Value: "global"}); err != nil {
		t.Fatal(err)
	}
	shared := request("?limit=100", token, http.StatusOK)
	if len(shared.Items) != 78 || shared.HasMore == nil || *shared.HasMore {
		t.Fatalf("shared page = %+v", shared)
	}
}

func TestWatchedHandlersRejectInvalidIdentity(t *testing.T) {
	st := &watchedModeHandlerStore{modeValue: "global"}
	h := &Handlers{store: st, watchedSvc: watched.NewService(st, nil)}
	ctx := auth.ContextWithUserID(t.Context(), 7)

	if _, err := h.CheckWatched(ctx, CheckWatchedRequestObject{Params: CheckWatchedParams{
		Source: CheckWatchedParamsSource("imdb"), MediaType: CheckWatchedParamsMediaType("movie"), ExternalId: 42,
	}}); !errors.Is(err, watched.ErrInvalidIdentity) {
		t.Fatalf("invalid check error = %v", err)
	}
	if _, err := h.CreateWatched(ctx, CreateWatchedRequestObject{Body: &CreateWatchedJSONRequestBody{
		Source: WatchedItemCreateSourceTmdb, MediaType: WatchedItemCreateMediaTypeSeries, ExternalId: 0, Title: "Invalid",
	}}); !errors.Is(err, watched.ErrInvalidIdentity) {
		t.Fatalf("invalid create error = %v", err)
	}
	if st.listCalls != 0 || st.checkCalls != 0 {
		t.Fatalf("invalid identity reached watched data: list=%d check=%d", st.listCalls, st.checkCalls)
	}
}

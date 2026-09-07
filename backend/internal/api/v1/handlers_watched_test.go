package apiv1

import (
	"errors"
	"path/filepath"
	"testing"

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
			if _, err := h.CheckWatched(ctx, CheckWatchedRequestObject{Params: CheckWatchedParams{
				Source: Tmdb, MediaType: CheckWatchedParamsMediaTypeMovie, ExternalId: 42,
			}}); !errors.Is(err, tc.want) {
				t.Fatalf("CheckWatched error = %v, want %v", err, tc.want)
			}
			if st.listCalls != 0 || st.checkCalls != 0 {
				t.Fatalf("mode failure reached watched data: list=%d check=%d", st.listCalls, st.checkCalls)
			}
		})
	}
}

func TestWatchedHandlersRejectInvalidIdentity(t *testing.T) {
	st := &watchedModeHandlerStore{modeValue: "global"}
	h := &Handlers{store: st, watchedSvc: watched.NewService(st, nil)}
	ctx := auth.ContextWithUserID(t.Context(), 7)

	if _, err := h.CheckWatched(ctx, CheckWatchedRequestObject{Params: CheckWatchedParams{
		Source: CheckWatchedParamsSource("imdb"), MediaType: CheckWatchedParamsMediaTypeMovie, ExternalId: 42,
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

package apiv1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

type discoverHandlerStore struct {
	store.Store
	metas []store.MediaMetadata
	err   error
}

type discoverRoundTripFunc func(*http.Request) (*http.Response, error)

func (f discoverRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func discoverResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func personCreditsHandlers(t *testing.T, transport discoverRoundTripFunc, env map[string]string) *Handlers {
	t.Helper()
	s := &discoverHandlerStore{}
	client := &http.Client{Transport: transport}
	set := settings.NewService(s, t.TempDir(), env, "", client)
	return &Handlers{matchSvc: matching.NewService(s, set, t.TempDir(), client)}
}

func (s *discoverHandlerStore) ListMediaMetadataExternalIDs() ([]store.MediaMetadata, error) {
	return s.metas, s.err
}

func (s *discoverHandlerStore) GetSetting(string) (*store.Setting, error) {
	return nil, store.ErrNotFound
}

func TestGetMediaExternalIdsMediaType(t *testing.T) {
	s := &discoverHandlerStore{metas: []store.MediaMetadata{
		{MediaItemID: 1, Source: "tmdb", ExternalID: 42, MediaType: "movie"},
		{MediaItemID: 2, Source: "tmdb", ExternalID: 42, MediaType: "series"},
		{MediaItemID: 3, Source: "tvdb", ExternalID: 42, MediaType: "series"},
	}}
	h := &Handlers{store: s}
	response, err := h.GetMediaExternalIds(context.Background(), GetMediaExternalIdsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got := response.(GetMediaExternalIds200JSONResponse)
	if len(got.Items) != len(s.metas) {
		t.Fatalf("got %d identities", len(got.Items))
	}
	for i, item := range got.Items {
		want := s.metas[i]
		if !item.MediaType.Valid() || string(item.MediaType) != want.MediaType || item.ExternalId != want.ExternalID || item.Source != want.Source || item.MediaItemId != int(want.MediaItemID) {
			t.Errorf("got %#v, want identity from %#v", item, want)
		}
	}
	s.err = errors.New("store unavailable")
	if _, err := h.GetMediaExternalIds(context.Background(), GetMediaExternalIdsRequestObject{}); !errors.Is(err, s.err) {
		t.Fatalf("membership error must propagate: %v", err)
	}
}

func TestFetchDiscoverDistinguishesProviderFailureFromEmpty(t *testing.T) {
	s := &discoverHandlerStore{}
	for _, key := range []string{"", "test-key"} {
		t.Run(key, func(t *testing.T) {
			client := &http.Client{}
			set := settings.NewService(s, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: key}, "", client)
			h := &Handlers{matchSvc: matching.NewService(s, set, t.TempDir(), client)}
			providerErr := errors.New("provider unavailable")
			called := false
			items, pages, err := h.fetchDiscover(func(*tmdb.Client) ([]DiscoverItem, int, error) {
				called = true
				return nil, 0, providerErr
			})
			if key == "" {
				if called || err != nil || len(items) != 0 || pages != 0 {
					t.Fatalf("missing key should be empty without network calls: %v, %v, %v", items, pages, err)
				}
			} else if !called || !errors.Is(err, providerErr) {
				t.Fatalf("provider error must reach the frontend for retry: %v", err)
			}
		})
	}
}

func TestPersonCreditsToDiscoverItems(t *testing.T) {
	movies, series := personCreditsToDiscoverItems([]tmdb.PersonCredit{
		{ID: 1, MediaType: "movie", Title: "Less Popular", Popularity: 10, ReleaseDate: "2024-01-01"},
		{ID: 2, MediaType: "movie", Title: "Most Popular", Popularity: 50, ReleaseDate: "1999-01-01"},
		{ID: 2, MediaType: "movie", Title: "Duplicate Role", Popularity: 40},
		{ID: 3, MediaType: "tv", Name: "Series", Popularity: 30, FirstAirDate: "2020-02-03"},
		{ID: 4, MediaType: "movie", Title: "Adult", Popularity: 100, Adult: true},
		{ID: 5, MediaType: "person", Name: "Unsupported"},
	})

	if len(movies) != 2 || movies[0].ExternalId != 2 || movies[0].Title != "Most Popular" || movies[1].ExternalId != 1 {
		t.Fatalf("unexpected movies: %+v", movies)
	}
	if len(series) != 1 || series[0].ExternalId != 3 || series[0].MediaType != DiscoverItemMediaTypeSeries || series[0].Year == nil || *series[0].Year != 2020 {
		t.Fatalf("unexpected series: %+v", series)
	}
}

func TestGetPersonCreditsUsesTVDBOwnedNameForFallback(t *testing.T) {
	var searchedName string
	h := personCreditsHandlers(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v4/login":
			return discoverResponse(http.StatusOK, `{"data":{"token":"token"}}`), nil
		case "/v4/people/77/extended":
			return discoverResponse(http.StatusOK, `{"data":{"id":77,"name":"Provider Name","remoteIds":[]}}`), nil
		case "/3/search/person":
			searchedName = req.URL.Query().Get("query")
			return discoverResponse(http.StatusOK, `{"total_pages":1,"results":[{"id":287,"name":"Provider Name"}]}`), nil
		case "/3/person/287":
			return discoverResponse(http.StatusOK, `{"id":287,"name":"Provider Name","combined_credits":{"cast":[]}}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}
	}, map[string]string{settings.KeyTMDBApiKey: "tmdb-key", settings.KeyTVDBApiKey: "tvdb-key"})

	queryName := "Client Supplied Name"
	response, err := h.GetPersonCredits(context.Background(), GetPersonCreditsRequestObject{
		Source: "tvdb", PersonId: 77, Params: GetPersonCreditsParams{Name: &queryName},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := response.(GetPersonCredits200JSONResponse)
	if searchedName != "Provider Name" || got.Name != "Provider Name" {
		t.Fatalf("searched %q and returned %q", searchedName, got.Name)
	}
}

func TestGetPersonCreditsRejectsInvalidIdentityAndMapsNotFound(t *testing.T) {
	h := &Handlers{}
	response, err := h.GetPersonCredits(context.Background(), GetPersonCreditsRequestObject{Source: "tmdb", PersonId: -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(GetPersonCredits400JSONResponse); !ok {
		t.Fatalf("negative ID response = %T", response)
	}

	h = personCreditsHandlers(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/3/person/999" {
			t.Fatalf("unexpected request: %s", req.URL.String())
		}
		return discoverResponse(http.StatusNotFound, `{"status_message":"not found"}`), nil
	}, map[string]string{settings.KeyTMDBApiKey: "tmdb-key"})
	response, err = h.GetPersonCredits(context.Background(), GetPersonCreditsRequestObject{Source: "tmdb", PersonId: 999})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(GetPersonCredits404JSONResponse); !ok {
		t.Fatalf("missing person response = %T", response)
	}
}

func TestGetPersonCreditsDoesNotNameFallbackOnTVDBFailure(t *testing.T) {
	var searched bool
	h := personCreditsHandlers(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v4/login":
			return discoverResponse(http.StatusOK, `{"data":{"token":"token"}}`), nil
		case "/v4/people/77/extended":
			return discoverResponse(http.StatusInternalServerError, `{"message":"unavailable"}`), nil
		case "/3/search/person":
			searched = true
			return discoverResponse(http.StatusOK, `{"results":[]}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}
	}, map[string]string{settings.KeyTMDBApiKey: "tmdb-key", settings.KeyTVDBApiKey: "tvdb-key"})

	name := "Fallback Name"
	response, err := h.GetPersonCredits(context.Background(), GetPersonCreditsRequestObject{
		Source: "tvdb", PersonId: 77, Params: GetPersonCreditsParams{Name: &name},
	})
	if err == nil || response != nil {
		t.Fatalf("response = %T, error = %v", response, err)
	}
	if searched {
		t.Fatal("TVDB failure must not trigger client-supplied name fallback")
	}
}

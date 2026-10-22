package matching

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/settings"
)

func TestProviderFetchesUseMappingsWithoutPersisting(t *testing.T) {
	for _, tc := range []struct {
		name, source, mediaType, detailPath, searchPath, episodePath, detail, search, episodes, title, poster string
	}{
		{name: "TMDB movie", source: "tmdb", mediaType: "movie", detailPath: "/3/movie/42", searchPath: "/3/search/movie",
			detail: `{"title":"Movie","release_date":"2020-01-01","poster_path":"/poster.jpg","imdb_id":"tt42"}`,
			search: `{"results":[{"id":42,"title":"Movie","release_date":"2020-01-01","poster_path":"/poster.jpg"}]}`,
			title:  "Movie", poster: tmdbPosterBase + "/poster.jpg"},
		{name: "TMDB series", source: "tmdb", mediaType: "series", detailPath: "/3/tv/42", searchPath: "/3/search/tv", episodePath: "/3/tv/42/season/1",
			detail:   `{"name":"Series","first_air_date":"2020-01-01","poster_path":"/poster.jpg","number_of_seasons":1,"external_ids":{"imdb_id":"tt42"}}`,
			search:   `{"results":[{"id":42,"name":"Series","first_air_date":"2020-01-01","poster_path":"/poster.jpg"}]}`,
			episodes: `{"episodes":[{"season_number":1,"episode_number":2,"name":"Episode","air_date":"2020-01-02","runtime":30}]}`,
			title:    "Series", poster: tmdbPosterBase + "/poster.jpg"},
		{name: "TVDB series", source: "tvdb", mediaType: "series", detailPath: "/v4/series/42/extended", searchPath: "/v4/search", episodePath: "/v4/series/42/episodes/default",
			detail:   `{"data":{"name":"Series","firstAired":"2020-01-01","image":"https://images.example.test/poster.jpg","seasons":[{"number":0},{"number":1},{"number":1}],"remoteIds":[{"sourceName":"IMDB","id":"tt42"}]}}`,
			search:   `{"data":[{"tvdb_id":"42","name":"Series","first_air_time":"2020-01-01","image_url":"https://images.example.test/poster.jpg"}]}`,
			episodes: `{"data":{"episodes":[{"seasonNumber":1,"number":2,"name":"Episode","aired":"2020-01-02","runtime":30}]}}`,
			title:    "Series", poster: "https://images.example.test/poster.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newMemStore()
			paths := map[string]int{}
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if st.inTx.Load() {
					t.Error("provider request ran inside a write transaction")
				}
				paths[req.URL.Path]++
				body, status := "{}", http.StatusOK
				switch req.URL.Path {
				case "/v4/login":
					body = `{"data":{"token":"fixture"}}`
				case tc.detailPath:
					body = tc.detail
				case tc.searchPath:
					body = tc.search
				case tc.episodePath:
					body = tc.episodes
				default:
					status = http.StatusNotFound
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			set := settings.NewService(st, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: "fixture", settings.KeyTVDBApiKey: "fixture"}, "", client)
			svc := NewService(st, set, t.TempDir(), client)
			preview, err := svc.GetExternalDetail(tc.source, tc.mediaType, 42)
			if err != nil {
				t.Fatal(err)
			}
			if preview.Title != tc.title || preview.Source != tc.source || preview.ExternalID != 42 || preview.MediaType != tc.mediaType || preview.ImdbID != "tt42" || preview.PosterURL != tc.poster || preview.Year == nil || *preview.Year != 2020 {
				t.Fatalf("preview = %+v", preview)
			}
			meta, episodes, err := svc.fetchMatchData(tc.source, "fixture", tc.mediaType, 42, 0.75)
			if err != nil {
				t.Fatal(err)
			}
			if meta.Title != preview.Title || meta.Source != tc.source || meta.ExternalID != 42 || meta.Confidence != 0.75 || meta.MatchedAt.IsZero() || meta.MediaItemID != 0 || meta.ID != 0 {
				t.Fatalf("match candidate = %+v", meta)
			}
			if tc.mediaType == "series" {
				want := []episodeData{{seasonNumber: 1, episodeNumber: 2, title: "Episode", airDate: "2020-01-02", runtime: 30}}
				if !reflect.DeepEqual(episodes, want) {
					t.Fatalf("episodes = %+v, want %+v", episodes, want)
				}
			} else if episodes != nil {
				t.Fatalf("movie episodes = %+v, want nil", episodes)
			}
			candidates, err := svc.SearchCandidates(tc.title, tc.mediaType, nil, tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) != 1 || candidates[0].Source != tc.source || candidates[0].ExternalID != 42 || candidates[0].PosterURL != tc.poster || candidates[0].Confidence != Score(tc.title, nil, tc.title, candidates[0].Year) {
				t.Fatalf("search candidates = %+v", candidates)
			}
			if paths[tc.detailPath] != 2 || paths[tc.searchPath] != 1 || (tc.episodePath != "" && paths[tc.episodePath] != 1) || (tc.source == "tvdb" && paths["/v4/login"] != 1) {
				t.Fatalf("provider call counts = %v", paths)
			}
			if len(st.items) != 0 || len(st.metas) != 0 || len(st.eps) != 0 || len(st.activity) != 0 {
				t.Fatal("read-only provider operations persisted state")
			}
		})
	}
}

func TestProviderFetchErrorsPreserveMetadataAndErrorChains(t *testing.T) {
	for _, tc := range []struct {
		name, source, mediaType, prefix string
	}{
		{name: "TMDB movie", source: "tmdb", mediaType: "movie", prefix: "fetching TMDB movie: "},
		{name: "TMDB series", source: "tmdb", mediaType: "series", prefix: "fetching TMDB TV: "},
		{name: "TVDB series", source: "tvdb", mediaType: "series", prefix: "fetching TVDB series: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status, body := http.StatusBadGateway, "{}"
				if req.URL.Path == "/v4/login" {
					status, body = http.StatusOK, `{"data":{"token":"fixture"}}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			svc := NewService(nil, nil, t.TempDir(), client)
			meta := mappingMetadataFixture()
			before := meta
			var err error
			if tc.source == "tmdb" {
				err = svc.fetchTMDBDetails("fixture", tc.mediaType, 42, &meta)
				var apiErr *tmdb.APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway {
					t.Fatalf("error chain = %v", err)
				}
			} else {
				err = svc.fetchTVDBDetails("fixture", 42, &meta)
				var apiErr *tvdb.APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway {
					t.Fatalf("error chain = %v", err)
				}
			}
			if !strings.HasPrefix(err.Error(), tc.prefix) || !reflect.DeepEqual(meta, before) {
				t.Fatalf("metadata/error changed on failed fetch: metadata=%+v error=%v", meta, err)
			}
		})
	}
}

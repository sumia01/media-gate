package tmdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// suggestionsTestClient returns a client pointed at a test server that serves
// per-path JSON bodies and records every request's path and query params.
func suggestionsTestClient(t *testing.T, bodies map[string]string, requests *[]string, queries *map[string]map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.URL.Path)
		q := make(map[string]string)
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		(*queries)[r.URL.Path] = q

		body, ok := bodies[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient("test-key", srv.Client())
	c.baseURL = srv.URL
	return c
}

func TestMovieSuggestions(t *testing.T) {
	t.Run("uses recommendations when they exist", func(t *testing.T) {
		var requests []string
		queries := map[string]map[string]string{}
		c := suggestionsTestClient(t, map[string]string{
			"/movie/603/recommendations": `{"results":[{"id":42,"title":"Blade Runner","release_date":"1982-06-25","vote_average":8.1}],"total_pages":7,"total_results":133}`,
		}, &requests, &queries)

		results, totalPages, err := c.MovieSuggestions(603, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(requests) != 1 || requests[0] != "/movie/603/recommendations" {
			t.Errorf("requests = %v, want only /movie/603/recommendations", requests)
		}
		if queries["/movie/603/recommendations"]["page"] != "2" {
			t.Errorf("page param = %q, want %q", queries["/movie/603/recommendations"]["page"], "2")
		}
		if totalPages != 7 {
			t.Errorf("totalPages = %d, want 7", totalPages)
		}
		if len(results) != 1 || results[0].ID != 42 || results[0].Title != "Blade Runner" {
			t.Errorf("unexpected results: %+v", results)
		}
	})

	t.Run("falls back to similar when recommendations are empty", func(t *testing.T) {
		var requests []string
		queries := map[string]map[string]string{}
		c := suggestionsTestClient(t, map[string]string{
			"/movie/603/recommendations": `{"results":[],"total_pages":1,"total_results":0}`,
			"/movie/603/similar":         `{"results":[{"id":77,"title":"Obscure Twin","release_date":"2001-01-01","vote_average":6.0}],"total_pages":4,"total_results":66}`,
		}, &requests, &queries)

		results, totalPages, err := c.MovieSuggestions(603, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"/movie/603/recommendations", "/movie/603/similar"}
		if len(requests) != 2 || requests[0] != want[0] || requests[1] != want[1] {
			t.Errorf("requests = %v, want %v", requests, want)
		}
		if queries["/movie/603/similar"]["page"] != "3" {
			t.Errorf("similar page param = %q, want %q", queries["/movie/603/similar"]["page"], "3")
		}
		if totalPages != 4 {
			t.Errorf("totalPages = %d, want 4", totalPages)
		}
		if len(results) != 1 || results[0].ID != 77 {
			t.Errorf("unexpected results: %+v", results)
		}
	})

	t.Run("page past a non-empty recommendation list does not mix in similar", func(t *testing.T) {
		var requests []string
		queries := map[string]map[string]string{}
		// TMDB answers out-of-range pages with empty results but keeps the
		// list's real total_pages/total_results; the fallback must not fire.
		c := suggestionsTestClient(t, map[string]string{
			"/movie/603/recommendations": `{"results":[],"total_pages":2,"total_results":40}`,
		}, &requests, &queries)

		results, totalPages, err := c.MovieSuggestions(603, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(requests) != 1 {
			t.Errorf("requests = %v, want only the recommendations call", requests)
		}
		if len(results) != 0 {
			t.Errorf("results = %+v, want empty", results)
		}
		if totalPages != 2 {
			t.Errorf("totalPages = %d, want 2", totalPages)
		}
	})
}

func TestTVSuggestions(t *testing.T) {
	t.Run("uses recommendations when they exist", func(t *testing.T) {
		var requests []string
		queries := map[string]map[string]string{}
		c := suggestionsTestClient(t, map[string]string{
			"/tv/1396/recommendations": `{"results":[{"id":99,"name":"The Expanse","first_air_date":"2015-12-14","vote_average":8.4}],"total_pages":3,"total_results":55}`,
		}, &requests, &queries)

		results, totalPages, err := c.TVSuggestions(1396, 4)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(requests) != 1 || requests[0] != "/tv/1396/recommendations" {
			t.Errorf("requests = %v, want only /tv/1396/recommendations", requests)
		}
		if queries["/tv/1396/recommendations"]["page"] != "4" {
			t.Errorf("page param = %q, want %q", queries["/tv/1396/recommendations"]["page"], "4")
		}
		if totalPages != 3 {
			t.Errorf("totalPages = %d, want 3", totalPages)
		}
		if len(results) != 1 || results[0].ID != 99 || results[0].Name != "The Expanse" {
			t.Errorf("unexpected results: %+v", results)
		}
	})

	t.Run("falls back to similar when recommendations are empty", func(t *testing.T) {
		var requests []string
		queries := map[string]map[string]string{}
		c := suggestionsTestClient(t, map[string]string{
			"/tv/1396/recommendations": `{"results":[],"total_pages":1,"total_results":0}`,
			"/tv/1396/similar":         `{"results":[{"id":88,"name":"Niche Show","first_air_date":"2010-05-01","vote_average":7.2}],"total_pages":2,"total_results":25}`,
		}, &requests, &queries)

		results, totalPages, err := c.TVSuggestions(1396, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"/tv/1396/recommendations", "/tv/1396/similar"}
		if len(requests) != 2 || requests[0] != want[0] || requests[1] != want[1] {
			t.Errorf("requests = %v, want %v", requests, want)
		}
		if totalPages != 2 {
			t.Errorf("totalPages = %d, want 2", totalPages)
		}
		if len(results) != 1 || results[0].ID != 88 {
			t.Errorf("unexpected results: %+v", results)
		}
	})
}

func TestPopularEndpoints(t *testing.T) {
	var requests []string
	queries := map[string]map[string]string{}
	c := suggestionsTestClient(t, map[string]string{
		"/movie/popular": `{"results":[{"id":1,"title":"A"}],"total_pages":9,"total_results":180}`,
		"/tv/popular":    `{"results":[{"id":2,"name":"B"}],"total_pages":5,"total_results":100}`,
	}, &requests, &queries)

	movies, movieTP, err := c.PopularMovies(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(movies) != 1 || movies[0].ID != 1 || movieTP != 9 {
		t.Errorf("PopularMovies = %+v, tp=%d; want one result id=1, tp=9", movies, movieTP)
	}
	if queries["/movie/popular"]["page"] != "2" {
		t.Errorf("movie page param = %q, want %q", queries["/movie/popular"]["page"], "2")
	}

	tv, tvTP, err := c.PopularTV(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tv) != 1 || tv[0].ID != 2 || tvTP != 5 {
		t.Errorf("PopularTV = %+v, tp=%d; want one result id=2, tp=5", tv, tvTP)
	}
	if queries["/tv/popular"]["page"] != "3" {
		t.Errorf("tv page param = %q, want %q", queries["/tv/popular"]["page"], "3")
	}
}

func TestFindTVByTVDBID(t *testing.T) {
	t.Run("resolves to TMDB id", func(t *testing.T) {
		var requests []string
		queries := map[string]map[string]string{}
		c := suggestionsTestClient(t, map[string]string{
			"/find/81189": `{"movie_results":[],"tv_results":[{"id":1396,"name":"Breaking Bad"}]}`,
		}, &requests, &queries)

		id, err := c.FindTVByTVDBID(81189)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if queries["/find/81189"]["external_source"] != "tvdb_id" {
			t.Errorf("external_source param = %q, want %q", queries["/find/81189"]["external_source"], "tvdb_id")
		}
		if id != 1396 {
			t.Errorf("id = %d, want 1396", id)
		}
	})

	t.Run("unknown TVDB id returns 0 without error", func(t *testing.T) {
		var requests []string
		queries := map[string]map[string]string{}
		c := suggestionsTestClient(t, map[string]string{
			"/find/999999": `{"movie_results":[],"tv_results":[]}`,
		}, &requests, &queries)

		id, err := c.FindTVByTVDBID(999999)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != 0 {
			t.Errorf("id = %d, want 0", id)
		}
	})
}

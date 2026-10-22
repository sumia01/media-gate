package matching

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/store"
)

func mappingMetadataFixture() store.MediaMetadata {
	year, runtime, seasons, rating := 1999, 90, 5, 8.5
	return store.MediaMetadata{
		ID: 11, MediaItemID: 22, MediaType: "series", Source: "original", ExternalID: 33,
		Title: "old title", Overview: "old overview", PosterPath: "old poster", ImdbID: "old imdb",
		Year: &year, Runtime: &runtime, Seasons: &seasons, Rating: &rating,
		Genres: "old genres", Credits: "old credits", TrailerURL: "old trailer",
		Status: "old status", ReleaseDate: "old date", ContentRatings: "old ratings",
		Confidence: 0.75, MatchedAt: time.Unix(10, 0), CreatedAt: time.Unix(20, 0), UpdatedAt: time.Unix(30, 0),
	}
}

func assertMetadataMapping(t *testing.T, input store.MediaMetadata, details any, want store.MediaMetadata, apply func() store.MediaMetadata) {
	t.Helper()
	before, err := json.Marshal([]any{input, details})
	if err != nil {
		t.Fatal(err)
	}
	if got := apply(); !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata = %+v, want %+v", got, want)
	}
	after, err := json.Marshal([]any{input, details})
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("mapping changed its metadata or provider input")
	}
}

func TestTMDBMovieMetadataMapping(t *testing.T) {
	base := mappingMetadataFixture()
	details := &tmdb.MovieDetails{
		MovieResult: tmdb.MovieResult{ID: 999, Title: "Movie", Overview: "Overview", PosterPath: "/movie.jpg", ReleaseDate: "2021-02-03", VoteAverage: 1},
		ImdbID:      "tt123", Runtime: 125, Status: "Released", Genres: []tmdb.Genre{{Name: "Drama"}},
		Credits:      &tmdb.Credits{Cast: []tmdb.CastMember{{ID: 7, Name: "Actor", Character: "Role", ProfilePath: "/actor.jpg", Order: 2}}},
		Videos:       &tmdb.VideosResult{Results: []tmdb.VideoResult{{Key: "trailer", Site: "YouTube", Type: "Trailer", Official: true, Iso639_1: "en"}}},
		ReleaseDates: &tmdb.ReleaseDatesResult{Results: []tmdb.ReleaseDateCountry{{Iso3166_1: "US", ReleaseDates: []tmdb.ReleaseDateEntry{{Certification: "PG-13", Type: 3}}}}},
	}
	want := base
	year, runtime := 2021, 125
	want.Title, want.Overview, want.PosterPath, want.Status = "Movie", "Overview", "/movie.jpg", "Released"
	want.ImdbID, want.Year, want.Runtime, want.ReleaseDate = "tt123", &year, &runtime, "2021-02-03"
	want.Genres = `["Drama"]`
	want.Credits = `[{"name":"Actor","role":"Role","type":"cast","image":"/actor.jpg","order":2,"source":"tmdb","personId":7}]`
	want.TrailerURL = "https://www.youtube.com/watch?v=trailer"
	want.ContentRatings = `[{"country":"US","rating":"PG-13"}]`
	assertMetadataMapping(t, base, details, want, func() store.MediaMetadata { return tmdbMovieMetadata(base, details) })

	for _, tc := range []struct {
		name    string
		runtime int
	}{{name: "zero runtime"}, {name: "negative runtime", runtime: -1}} {
		t.Run(tc.name, func(t *testing.T) {
			empty := &tmdb.MovieDetails{Runtime: tc.runtime, MovieResult: tmdb.MovieResult{ReleaseDate: "invalid"}}
			want := base
			want.Title, want.Overview, want.PosterPath, want.Status, want.ImdbID = "", "", "", "", ""
			want.ReleaseDate, want.Genres, want.Credits, want.TrailerURL, want.ContentRatings = "invalid", "[]", "", "", ""
			assertMetadataMapping(t, base, empty, want, func() store.MediaMetadata { return tmdbMovieMetadata(base, empty) })
		})
	}
}

func TestTMDBTVMetadataMapping(t *testing.T) {
	base := mappingMetadataFixture()
	details := &tmdb.TVDetails{
		TVResult: tmdb.TVResult{ID: 999, Name: "Series", Overview: "Overview", PosterPath: "/series.jpg", FirstAirDate: "2022-03-04"},
		Status:   "Returning Series", NumberOfSeasons: 2, ExternalIds: &tmdb.ExternalIds{ImdbID: "tt456", TvdbID: 987},
		Genres: []tmdb.Genre{{Name: "Comedy"}}, Credits: &tmdb.Credits{Crew: []tmdb.CrewMember{{ID: 8, Name: "Director", Job: "Director"}}},
		ContentRatings: &tmdb.ContentRatingsResult{Results: []tmdb.ContentRatingEntry{{Iso3166_1: "HU", Rating: "16"}}},
		Videos:         &tmdb.VideosResult{Results: []tmdb.VideoResult{{Key: "series", Site: "YouTube", Type: "Trailer"}}},
	}
	want := base
	year, seasons := 2022, 2
	want.Title, want.Overview, want.PosterPath, want.Status = "Series", "Overview", "/series.jpg", "Returning Series"
	want.ImdbID, want.Year, want.Seasons, want.ReleaseDate = "tt456", &year, &seasons, "2022-03-04"
	want.Genres, want.TrailerURL, want.ContentRatings = `["Comedy"]`, "https://www.youtube.com/watch?v=series", `[{"country":"HU","rating":"16"}]`
	want.Credits = `[{"name":"Director","role":"Director","type":"crew","order":0,"source":"tmdb","personId":8}]`
	assertMetadataMapping(t, base, details, want, func() store.MediaMetadata { return tmdbTVMetadata(base, details) })

	for _, tc := range []struct {
		name string
		ids  *tmdb.ExternalIds
		imdb string
	}{
		{name: "absent IDs preserve existing", imdb: base.ImdbID},
		{name: "present empty IDs clear existing", ids: &tmdb.ExternalIds{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			empty := &tmdb.TVDetails{TVResult: tmdb.TVResult{FirstAirDate: "1899-01-01"}, NumberOfSeasons: -1, ExternalIds: tc.ids}
			want := base
			want.Title, want.Overview, want.PosterPath, want.Status, want.ImdbID = "", "", "", "", tc.imdb
			want.ReleaseDate, want.Genres, want.Credits, want.TrailerURL, want.ContentRatings = "1899-01-01", "[]", "", "", ""
			assertMetadataMapping(t, base, empty, want, func() store.MediaMetadata { return tmdbTVMetadata(base, empty) })
		})
	}
}

func TestTVDBSeriesMetadataMapping(t *testing.T) {
	base := mappingMetadataFixture()
	details := &tvdb.SeriesDetails{
		ID: 999, Name: "TVDB series", Overview: "Overview", Image: "https://images.example.test/poster.jpg", FirstAired: "2023-04-05",
		Status: tvdb.Status{Name: "Ended"}, Seasons: []tvdb.SeasonEntry{{Number: 0}, {Number: 3}, {Number: 1}, {Number: 3}},
		RemoteIds:      []tvdb.RemoteID{{SourceName: "TMDB", ID: "ignored"}, {SourceName: "IMDB", ID: "tt789"}},
		Characters:     []tvdb.Character{{PeopleID: 9, PersonName: "Actor", PeopleType: "Actor", Name: "Role", Sort: 1}},
		ContentRatings: []tvdb.ContentRating{{Name: "TV-Y", Country: "usa", ContentType: "episode", Order: 9}, {Name: "TV-MA", Country: "usa", ContentType: "series"}},
	}
	want := base
	year, seasons := 2023, 3
	want.Title, want.Overview, want.PosterPath, want.Status = "TVDB series", "Overview", "https://images.example.test/poster.jpg", "Ended"
	want.ImdbID, want.Year, want.Seasons, want.ReleaseDate = "tt789", &year, &seasons, "2023-04-05"
	want.Credits = `[{"name":"Actor","role":"Role","type":"cast","order":1,"source":"tvdb","personId":9}]`
	want.ContentRatings = `[{"country":"US","rating":"TV-MA"}]`
	assertMetadataMapping(t, base, details, want, func() store.MediaMetadata { return tvdbSeriesMetadata(base, details) })

	for _, tc := range []struct {
		name    string
		seasons []tvdb.SeasonEntry
	}{
		{name: "absent seasons"},
		{name: "specials only", seasons: []tvdb.SeasonEntry{{Number: 0}, {Number: -1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			empty := &tvdb.SeriesDetails{Seasons: tc.seasons}
			want := base
			want.Title, want.Overview, want.PosterPath, want.Status, want.ImdbID = "", "", "", "", ""
			want.ReleaseDate, want.Credits, want.ContentRatings = "", "", ""
			assertMetadataMapping(t, base, empty, want, func() store.MediaMetadata { return tvdbSeriesMetadata(base, empty) })
		})
	}
}

func TestProviderEpisodeMappingsPreserveNaturalKeysAndOrder(t *testing.T) {
	tmdbEntries := []tmdb.TVEpisode{{ID: 999, SeasonNumber: 0, EpisodeNumber: 4, Name: "Special", Overview: "Overview", AirDate: "invalid", Runtime: -1}, {SeasonNumber: 2, EpisodeNumber: 1, Name: "First", Runtime: 30}}
	tvdbEntries := []tvdb.EpisodeEntry{{ID: 888, SeasonNumber: 0, Number: 4, Name: "Special", Overview: "Overview", Aired: "invalid", Runtime: -1}, {SeasonNumber: 2, Number: 1, Name: "First", Runtime: 30}}
	want := []episodeData{{seasonNumber: 0, episodeNumber: 4, title: "Special", overview: "Overview", airDate: "invalid", runtime: -1}, {seasonNumber: 2, episodeNumber: 1, title: "First", runtime: 30}}
	for name, got := range map[string][]episodeData{"tmdb": tmdbEpisodeData(tmdbEntries), "tvdb": tvdbEpisodeData(tvdbEntries)} {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s episodes = %+v, want %+v", name, got, want)
		}
		got[0].title = "changed output"
	}
	if tmdbEntries[0].Name != "Special" || tvdbEntries[0].Name != "Special" {
		t.Fatal("episode mapping changed provider input")
	}
	for name, got := range map[string][]episodeData{"tmdb": tmdbEpisodeData(nil), "tvdb": tvdbEpisodeData(nil)} {
		if got == nil || len(got) != 0 {
			t.Fatalf("%s empty episodes = %#v, want non-nil empty slice", name, got)
		}
	}
}

func TestProviderCandidateMappings(t *testing.T) {
	year := 2020
	for _, tc := range []struct {
		name string
		got  Candidate
		want Candidate
	}{
		{name: "TMDB movie", got: tmdbMovieCandidate(tmdb.MovieResult{ID: 1, Title: "Movie", Overview: "Overview", PosterPath: "/movie.jpg", ReleaseDate: "2020-01-01"}), want: Candidate{Source: "tmdb", ExternalID: 1, Title: "Movie", Overview: "Overview", PosterURL: tmdbPosterBase + "/movie.jpg", Year: &year}},
		{name: "TMDB TV", got: tmdbTVCandidate(tmdb.TVResult{ID: 2, Name: "Series", Overview: "Overview", PosterPath: "/tv.jpg", FirstAirDate: "2020-01-01"}), want: Candidate{Source: "tmdb", ExternalID: 2, Title: "Series", Overview: "Overview", PosterURL: tmdbPosterBase + "/tv.jpg", Year: &year}},
		{name: "TVDB series", got: tvdbSeriesCandidate(tvdb.SeriesResult{TVDBID: "3", Name: "Series", Overview: "Overview", ImageURL: "https://images.example.test/poster.jpg", FirstAirDate: "2020-01-01"}), want: Candidate{Source: "tvdb", ExternalID: 3, Title: "Series", Overview: "Overview", PosterURL: "https://images.example.test/poster.jpg", Year: &year}},
		{name: "TMDB missing movie fields", got: tmdbMovieCandidate(tmdb.MovieResult{ReleaseDate: "2100-01-01"}), want: Candidate{Source: "tmdb"}},
		{name: "TMDB missing TV fields", got: tmdbTVCandidate(tmdb.TVResult{FirstAirDate: "invalid"}), want: Candidate{Source: "tmdb"}},
		{name: "TVDB invalid ID", got: tvdbSeriesCandidate(tvdb.SeriesResult{TVDBID: "invalid"}), want: Candidate{Source: "tvdb"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Fatalf("candidate = %+v, want %+v", tc.got, tc.want)
			}
		})
	}
}

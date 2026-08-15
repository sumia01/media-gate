package tmdb

import (
	"encoding/json"
	"testing"
)

func TestMovieCertificationsPrefersTheatrical(t *testing.T) {
	// One country, several release events: only the digital and theatrical ones
	// carry a certification and they disagree. Theatrical must win.
	rd := &ReleaseDatesResult{Results: []ReleaseDateCountry{{
		Iso3166_1: "US",
		ReleaseDates: []ReleaseDateEntry{
			{Type: 1, Certification: ""},
			{Type: 4, Certification: "R"},
			{Type: 3, Certification: "PG-13"},
		},
	}}}

	got := MovieCertifications(rd)
	if got["US"] != "PG-13" {
		t.Errorf("US = %q, want %q (theatrical outranks digital)", got["US"], "PG-13")
	}
}

func TestMovieCertificationsFallsBackToAnyNonEmpty(t *testing.T) {
	// No recognised release type, but a certification is still present.
	rd := &ReleaseDatesResult{Results: []ReleaseDateCountry{{
		Iso3166_1: "HU",
		ReleaseDates: []ReleaseDateEntry{
			{Type: 99, Certification: "16"},
		},
	}}}

	if got := MovieCertifications(rd)["HU"]; got != "16" {
		t.Errorf("HU = %q, want %q", got, "16")
	}
}

func TestMovieCertificationsSkipsCountriesWithoutCertification(t *testing.T) {
	// TMDB routinely lists a country with release dates but no certification.
	// Storing it as an empty rating would render a blank badge in the UI.
	rd := &ReleaseDatesResult{Results: []ReleaseDateCountry{
		{Iso3166_1: "US", ReleaseDates: []ReleaseDateEntry{{Type: 3, Certification: "R"}}},
		{Iso3166_1: "HU", ReleaseDates: []ReleaseDateEntry{{Type: 3, Certification: "  "}}},
		{Iso3166_1: "", ReleaseDates: []ReleaseDateEntry{{Type: 3, Certification: "18"}}},
	}}

	got := MovieCertifications(rd)
	if len(got) != 1 {
		t.Fatalf("got %d certifications, want 1: %v", len(got), got)
	}
	if _, ok := got["HU"]; ok {
		t.Error("HU present despite a blank certification")
	}
}

func TestMovieCertificationsNilSafe(t *testing.T) {
	// A movie fetched before release_dates was appended (or one TMDB has no
	// release data for) unmarshals to a nil pointer.
	if got := MovieCertifications(nil); got != nil {
		t.Errorf("nil release dates = %v, want nil", got)
	}
}

func TestTVCertifications(t *testing.T) {
	cr := &ContentRatingsResult{Results: []ContentRatingEntry{
		{Iso3166_1: "us", Rating: "TV-MA"},
		{Iso3166_1: "HU", Rating: "16"},
		{Iso3166_1: "DE", Rating: ""},
	}}

	got := TVCertifications(cr)
	if got["US"] != "TV-MA" {
		t.Errorf("US = %q, want %q (country code must be upper-cased)", got["US"], "TV-MA")
	}
	if got["HU"] != "16" {
		t.Errorf("HU = %q, want %q", got["HU"], "16")
	}
	if _, ok := got["DE"]; ok {
		t.Error("DE present despite an empty rating")
	}
}

func TestTVCertificationsNilSafe(t *testing.T) {
	if got := TVCertifications(nil); got != nil {
		t.Errorf("nil content ratings = %v, want nil", got)
	}
}

// TestDetailsDecodeAppendedCertifications guards the wiring between the
// append_to_response request and the structs: a JSON field renamed or a tag
// typo would silently yield no ratings at all rather than an error.
func TestDetailsDecodeAppendedCertifications(t *testing.T) {
	var movie MovieDetails
	if err := json.Unmarshal([]byte(`{
		"id": 1, "title": "T",
		"release_dates": {"results": [
			{"iso_3166_1": "US", "release_dates": [{"certification": "PG-13", "type": 3}]}
		]}
	}`), &movie); err != nil {
		t.Fatalf("unmarshal movie: %v", err)
	}
	if got := MovieCertifications(movie.ReleaseDates)["US"]; got != "PG-13" {
		t.Errorf("movie US = %q, want %q", got, "PG-13")
	}

	var tv TVDetails
	if err := json.Unmarshal([]byte(`{
		"id": 2, "name": "S",
		"content_ratings": {"results": [{"iso_3166_1": "HU", "rating": "16"}]}
	}`), &tv); err != nil {
		t.Fatalf("unmarshal tv: %v", err)
	}
	if got := TVCertifications(tv.ContentRatings)["HU"]; got != "16" {
		t.Errorf("tv HU = %q, want %q", got, "16")
	}
}

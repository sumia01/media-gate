package matching

import (
	"encoding/json"
	"testing"

	"github.com/sumia01/media-gate/internal/integration/tvdb"
)

func decodeRatings(t *testing.T, raw string) []ContentRating {
	t.Helper()
	if raw == "" {
		return nil
	}
	var out []ContentRating
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return out
}

func TestContentRatingsToJSONIsSortedByCountry(t *testing.T) {
	// Map iteration order is random; an unstable serialization would rewrite the
	// media_metadata row on every refresh even when nothing actually changed.
	in := map[string]string{"US": "R", "HU": "18", "DE": "FSK 16"}

	first := contentRatingsToJSON(in)
	for range 20 {
		if got := contentRatingsToJSON(in); got != first {
			t.Fatalf("unstable output: %q != %q", got, first)
		}
	}

	got := decodeRatings(t, first)
	want := []ContentRating{{"DE", "FSK 16"}, {"HU", "18"}, {"US", "R"}}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestContentRatingsToJSONEmpty(t *testing.T) {
	// Empty must serialize to "" (the column's zero value), not "null" or "[]",
	// so withContentRatings can treat "no data" as a single condition.
	if got := contentRatingsToJSON(nil); got != "" {
		t.Errorf("nil = %q, want empty string", got)
	}
	if got := contentRatingsToJSON(map[string]string{}); got != "" {
		t.Errorf("empty map = %q, want empty string", got)
	}
}

func TestTVDBContentRatingsNormalizeAlpha3(t *testing.T) {
	// TVDB reports lower-case alpha-3; TMDB reports alpha-2. Without
	// normalization a country filter of ["HU"] would never match a TVDB series.
	raw := tvdbContentRatingsToJSON([]tvdb.ContentRating{
		{Name: "TV-MA", Country: "usa"},
		{Name: "16", Country: "hun"},
	})

	got := decodeRatings(t, raw)
	want := []ContentRating{{"HU", "16"}, {"US", "TV-MA"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTVDBContentRatingsPrefersSeriesLevel(t *testing.T) {
	// A series-level rating describes the show; an episode-level one describes a
	// single episode. Picking the episode entry can advertise a mature series as
	// child-friendly, so contentType outranks Order.
	raw := tvdbContentRatingsToJSON([]tvdb.ContentRating{
		{Name: "TV-Y", Country: "usa", ContentType: "episode", Order: 1},
		{Name: "TV-MA", Country: "usa", ContentType: "series", Order: 6},
	})

	got := decodeRatings(t, raw)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	if got[0].Rating != "TV-MA" {
		t.Errorf("US = %q, want %q (series-level must win)", got[0].Rating, "TV-MA")
	}
}

func TestTVDBContentRatingsSeriesLevelWinsEvenWithLowerOrder(t *testing.T) {
	// contentType is the documented discriminator, Order is not — so a
	// series-level entry wins regardless of how the Order values compare.
	raw := tvdbContentRatingsToJSON([]tvdb.ContentRating{
		{Name: "TV-MA", Country: "usa", ContentType: "episode", Order: 6},
		{Name: "TV-14", Country: "usa", ContentType: "series", Order: 5},
	})

	got := decodeRatings(t, raw)
	if got[0].Rating != "TV-14" {
		t.Errorf("US = %q, want %q (series-level must win)", got[0].Rating, "TV-14")
	}
}

func TestTVDBContentRatingsPrefersStrictestAmongEquals(t *testing.T) {
	// With no series/episode distinction to go on, the highest Order wins.
	// Order's meaning is undocumented, so this is chosen for the safe direction:
	// where it ranks severity, this yields the strictest rating rather than the
	// mildest.
	raw := tvdbContentRatingsToJSON([]tvdb.ContentRating{
		{Name: "TV-Y", Country: "usa", Order: 1},
		{Name: "TV-MA", Country: "usa", Order: 6},
	})

	got := decodeRatings(t, raw)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	if got[0].Rating != "TV-MA" {
		t.Errorf("US = %q, want %q (strictest must win)", got[0].Rating, "TV-MA")
	}
}

func TestTVDBContentRatingsSkipsUnusable(t *testing.T) {
	// Unmapped country codes and blank names are dropped rather than stored
	// under a key the settings picker could never select.
	raw := tvdbContentRatingsToJSON([]tvdb.ContentRating{
		{Name: "16", Country: "hun"},
		{Name: "18", Country: "xyz"},
		{Name: "   ", Country: "usa"},
	})

	got := decodeRatings(t, raw)
	if len(got) != 1 || got[0].Country != "HU" {
		t.Errorf("got %+v, want only HU", got)
	}
}

// pickerCountries mirrors RATING_COUNTRIES in
// frontend/src/components/settings/SettingsMediaDb.vue. Keep the two in sync:
// a country offered in the picker but absent from tvdbAlpha3ToAlpha2 is
// selectable yet silently unmatchable for every TVDB series, with no error
// anywhere — TMDB titles would show a rating while TVDB ones never would.
var pickerCountries = []string{
	"HU", "US", "GB", "DE", "AT", "CH", "FR", "IT", "ES", "PT",
	"NL", "BE", "IE", "DK", "SE", "NO", "FI", "PL", "CZ", "SK",
	"RO", "BG", "HR", "RS", "SI", "GR", "TR", "UA", "RU", "CA",
	"MX", "BR", "AR", "AU", "NZ", "JP", "KR", "IN", "ZA",
}

func TestEverySelectableCountryIsMappableFromTVDB(t *testing.T) {
	mapped := make(map[string]bool, len(tvdbAlpha3ToAlpha2))
	for _, alpha2 := range tvdbAlpha3ToAlpha2 {
		mapped[alpha2] = true
	}
	for _, c := range pickerCountries {
		if !mapped[c] {
			t.Errorf("country %q is selectable in the settings picker but has no alpha-3 entry in tvdbAlpha3ToAlpha2; TVDB series would never match it", c)
		}
	}
}

func TestTVDBContentRatingsEmpty(t *testing.T) {
	if got := tvdbContentRatingsToJSON(nil); got != "" {
		t.Errorf("nil = %q, want empty string", got)
	}
}

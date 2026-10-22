package matching

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/sumia01/media-gate/internal/integration/tvdb"
)

// ContentRating is one country's content/age certification, normalized across
// providers. Country is ISO 3166-1 alpha-2 upper case; Rating is the raw
// provider string ("TV-MA", "PG-13", "16"), which is regionally meaningful and
// deliberately not translated to a common scale.
type ContentRating struct {
	Country string `json:"country"`
	Rating  string `json:"rating"`
}

// tvdbAlpha3ToAlpha2 maps the lower-case ISO 3166-1 alpha-3 codes TVDB reports
// on contentRatings onto the alpha-2 codes TMDB uses, so one stored country
// preference matches items regardless of which provider matched them.
//
// Codes outside this map are dropped rather than stored under an alpha-3 key:
// the settings picker offers alpha-2 countries only, so an unmapped code could
// never be selected for display anyway, and storing it would just be dead data.
var tvdbAlpha3ToAlpha2 = map[string]string{
	"arg": "AR", "aus": "AU", "aut": "AT", "bel": "BE", "bgr": "BG",
	"bra": "BR", "can": "CA", "che": "CH", "chn": "CN", "cze": "CZ",
	"deu": "DE", "dnk": "DK", "esp": "ES", "est": "EE", "fin": "FI",
	"fra": "FR", "gbr": "GB", "grc": "GR", "hkg": "HK", "hrv": "HR",
	"hun": "HU", "idn": "ID", "ind": "IN", "irl": "IE", "isl": "IS",
	"isr": "IL", "ita": "IT", "jpn": "JP", "kor": "KR", "ltu": "LT",
	"lux": "LU", "lva": "LV", "mex": "MX", "mys": "MY", "nld": "NL",
	"nor": "NO", "nzl": "NZ", "phl": "PH", "pol": "PL", "prt": "PT",
	"rou": "RO", "rus": "RU", "sgp": "SG", "svk": "SK", "svn": "SI",
	"srb": "RS", "swe": "SE", "tha": "TH", "tur": "TR", "twn": "TW",
	"ukr": "UA", "usa": "US", "vnm": "VN", "zaf": "ZA",
}

// tvdbRatingCandidate is one country's competing rating during selection.
type tvdbRatingCandidate struct {
	rating   string
	isSeries bool
	order    int
}

// beats reports whether c should replace cur as the country's rating.
func (c tvdbRatingCandidate) beats(cur tvdbRatingCandidate) bool {
	if c.isSeries != cur.isSeries {
		return c.isSeries
	}
	return c.order > cur.order
}

// tvdbContentRatingsToJSON normalizes TVDB's extended-record contentRatings
// into the shared storage shape.
//
// TVDB can report several ratings for one country — typically a series-level
// entry alongside an episode-level one. Selection prefers the series-level
// rating, then the highest Order among equals.
//
// Order's meaning is NOT documented by TVDB (its published schema declares a
// bare integer), so this deliberately does not rely on it being presentation
// order. Where it is observably a severity rank, highest-wins yields the
// strictest rating — the safe direction, since displaying TV-Y for a TV-MA
// series is a far worse error than the reverse.
func tvdbContentRatingsToJSON(ratings []tvdb.ContentRating) string {
	if len(ratings) == 0 {
		return ""
	}
	best := make(map[string]tvdbRatingCandidate)
	for _, r := range ratings {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		country, ok := tvdbAlpha3ToAlpha2[strings.ToLower(strings.TrimSpace(r.Country))]
		if !ok {
			continue
		}
		c := tvdbRatingCandidate{
			rating:   name,
			isSeries: strings.EqualFold(strings.TrimSpace(r.ContentType), "series"),
			order:    r.Order,
		}
		if cur, seen := best[country]; !seen || c.beats(cur) {
			best[country] = c
		}
	}
	out := make(map[string]string, len(best))
	for country, c := range best {
		out[country] = c.rating
	}
	return contentRatingsToJSON(out)
}

// contentRatingsToJSON serializes a country -> rating map into the stored JSON
// array, sorted by country so the column is stable across refetches (an
// unstable order would rewrite the row on every metadata refresh).
func contentRatingsToJSON(ratings map[string]string) string {
	if len(ratings) == 0 {
		return ""
	}
	countries := make([]string, 0, len(ratings))
	for c := range ratings {
		countries = append(countries, c)
	}
	sort.Strings(countries)

	list := make([]ContentRating, 0, len(countries))
	for _, c := range countries {
		list = append(list, ContentRating{Country: c, Rating: ratings[c]})
	}
	b, _ := json.Marshal(list)
	return string(b)
}

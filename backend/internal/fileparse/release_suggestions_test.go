package fileparse

import "testing"

func TestReleaseSuggestionsFuzzyMatches(t *testing.T) {
	for _, query := range []string{"ETHEL", "eth", "thel", "etl", "ehtel", "ethle", "etjel", "ethek", "ethelx"} {
		if score := ReleaseSuggestionScore("Ethel", query); score < 0 {
			t.Errorf("%q did not suggest ethel", query)
		}
	}
	for _, query := range []string{"flx", "lux", "fluz"} {
		if score := ReleaseSuggestionScore("flux", query); score < 0 {
			t.Errorf("%q did not suggest flux", query)
		}
	}
	for _, query := range []string{"", " ", "zz", "xyz", "exyzl"} {
		if score := ReleaseSuggestionScore("ethel", query); score >= 0 {
			t.Errorf("unrelated %q suggested ethel with score %d", query, score)
		}
	}
	previous := -1
	for _, name := range []string{"etl", "etlhq", "team-etl", "ethel", "elt"} {
		score := ReleaseSuggestionScore(name, "etl")
		if score <= previous {
			t.Fatalf("ranking %q = %d, want after %d", name, score, previous)
		}
		previous = score
	}
}

package mediaserver

import "testing"

func TestAutoMatchUsesTypeAndPaths(t *testing.T) {
	sections := []Section{
		{ID: "movie", Type: "movie", Locations: []string{"/media/TV"}},
		{ID: "other", Type: "show", Locations: []string{"/other"}},
		{ID: "basename", Type: "show", Locations: []string{"/mnt/TV"}},
		{ID: "exact", Type: "show", Locations: []string{"/media/TV"}},
	}
	for _, mediaType := range []string{"series", "tv"} {
		if match := AutoMatch("/media/TV", mediaType, sections); match == nil || match.SectionID != "exact" {
			t.Fatalf("wrong match for %s: %+v", mediaType, match)
		}
		if match := AutoMatch("/media/TV", mediaType, sections[:3]); match == nil || match.SectionID != "basename" {
			t.Fatalf("wrong basename match: %+v", match)
		}
	}
	if match := AutoMatch("/media/TV", "music", sections); match != nil {
		t.Fatalf("matched incompatible type: %+v", match)
	}
}

package apiv1

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

const storedRatings = `[{"country":"DE","rating":"FSK 16"},{"country":"HU","rating":"16"},{"country":"US","rating":"TV-MA"}]`

func itemWithMetadata() MediaItem {
	return MediaItem{Metadata: &MediaMetadata{Title: "Silo"}}
}

func TestWithContentRatingsFiltersAndOrders(t *testing.T) {
	// Selection order is display order: HU before US even though the stored
	// list is sorted alphabetically (DE, HU, US).
	meta := &store.MediaMetadata{ContentRatings: storedRatings}

	got := withContentRatings(itemWithMetadata(), meta, []string{"HU", "US"})

	if got.Metadata.ContentRatings == nil {
		t.Fatal("contentRatings not attached")
	}
	ratings := *got.Metadata.ContentRatings
	want := []ContentRating{{Country: "HU", Rating: "16"}, {Country: "US", Rating: "TV-MA"}}
	if len(ratings) != len(want) {
		t.Fatalf("got %+v, want %+v", ratings, want)
	}
	for i := range want {
		if ratings[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, ratings[i], want[i])
		}
	}
}

func TestWithContentRatingsSkipsMissingCountries(t *testing.T) {
	// The agreed behaviour: a selected country with no rating for this title is
	// silently skipped, not rendered as a blank badge.
	meta := &store.MediaMetadata{ContentRatings: storedRatings}

	got := withContentRatings(itemWithMetadata(), meta, []string{"FR", "HU", "JP"})

	ratings := *got.Metadata.ContentRatings
	if len(ratings) != 1 || ratings[0].Country != "HU" {
		t.Errorf("got %+v, want only HU", ratings)
	}
}

func TestWithContentRatingsEmptyWhenNoneMatch(t *testing.T) {
	// An empty-but-present array is what drives the "No rating data found"
	// message, so it must not collapse to nil.
	meta := &store.MediaMetadata{ContentRatings: storedRatings}

	got := withContentRatings(itemWithMetadata(), meta, []string{"JP", "KR"})

	if got.Metadata.ContentRatings == nil {
		t.Fatal("contentRatings is nil, want an empty array")
	}
	if len(*got.Metadata.ContentRatings) != 0 {
		t.Errorf("got %+v, want empty", *got.Metadata.ContentRatings)
	}
}

func TestWithContentRatingsEmptyWhenNothingUsableStored(t *testing.T) {
	// Items matched before this feature shipped have an empty column, and a
	// corrupted column must not break the page. Both report "no rating data"
	// rather than hiding the tile, which is reserved for "feature switched off".
	cases := map[string]*store.MediaMetadata{
		"no stored ratings": {},
		"nil metadata":      nil,
		"malformed json":    {ContentRatings: "{not json"},
	}

	for name, meta := range cases {
		t.Run(name, func(t *testing.T) {
			got := withContentRatings(itemWithMetadata(), meta, []string{"HU", "US"})
			if got.Metadata.ContentRatings == nil {
				t.Fatal("contentRatings is nil, want an empty array")
			}
			if len(*got.Metadata.ContentRatings) != 0 {
				t.Errorf("got %+v, want empty", *got.Metadata.ContentRatings)
			}
		})
	}
}

func TestWithContentRatingsUntouchedWithoutMetadata(t *testing.T) {
	// An unmatched item has no metadata object to hang ratings off.
	got := withContentRatings(MediaItem{}, &store.MediaMetadata{ContentRatings: storedRatings}, []string{"HU"})
	if got.Metadata != nil {
		t.Errorf("metadata = %+v, want nil", got.Metadata)
	}
}

// TestFilterContentRatingsSharedByPreview covers the external-preview path,
// which stores the same JSON shape but has no store.MediaMetadata behind it.
func TestFilterContentRatingsSharedByPreview(t *testing.T) {
	t.Run("filters and orders", func(t *testing.T) {
		got := filterContentRatings(storedRatings, []string{"US", "HU"})
		if got == nil {
			t.Fatal("got nil, want ratings")
		}
		want := []ContentRating{{Country: "US", Rating: "TV-MA"}, {Country: "HU", Rating: "16"}}
		if len(*got) != len(want) {
			t.Fatalf("got %+v, want %+v", *got, want)
		}
		for i := range want {
			if (*got)[i] != want[i] {
				t.Errorf("entry %d = %+v, want %+v", i, (*got)[i], want[i])
			}
		}
	})

	t.Run("empty when provider returned none", func(t *testing.T) {
		got := filterContentRatings("", []string{"HU"})
		if got == nil {
			t.Fatal("got nil, want an empty array")
		}
		if len(*got) != 0 {
			t.Errorf("got %+v, want empty", *got)
		}
	})

	t.Run("omitted when no countries selected", func(t *testing.T) {
		if got := filterContentRatings(storedRatings, nil); got != nil {
			t.Errorf("got %+v, want nil", *got)
		}
	})
}

func TestWithContentRatingsOmittedWhenNoCountriesSelected(t *testing.T) {
	// Deselecting every country switches the feature off: the field is omitted
	// so the UI hides the tile instead of claiming data is missing.
	meta := &store.MediaMetadata{ContentRatings: storedRatings}

	for name, countries := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			got := withContentRatings(itemWithMetadata(), meta, countries)
			if got.Metadata.ContentRatings != nil {
				t.Errorf("contentRatings = %+v, want nil", *got.Metadata.ContentRatings)
			}
		})
	}
}

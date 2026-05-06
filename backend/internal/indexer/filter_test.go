package indexer

import (
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

func TestFilterByProfile_Languages(t *testing.T) {
	results := []TorrentResult{
		{Title: "Movie.2024.1080p.HUN.ENG.BluRay.x264", Seeders: 100},
		{Title: "Movie.2024.1080p.ENG.BluRay.x264", Seeders: 200},
		{Title: "Movie.2024.1080p.GER.BluRay.x264", Seeders: 150},
		{Title: "Movie.2024.1080p.MULTI.BluRay.x264", Seeders: 50},
	}

	t.Run("AND mode filters to dual audio only", func(t *testing.T) {
		filtered := FilterByProfile(results, nil, nil, []string{"hun", "eng"}, nil, "and")
		// Should match: HUN.ENG and MULTI
		if len(filtered) != 2 {
			t.Fatalf("expected 2 results, got %d: %v", len(filtered), titles(filtered))
		}
		if filtered[0].Title != results[0].Title {
			t.Errorf("expected first result to be HUN.ENG, got %q", filtered[0].Title)
		}
		if filtered[1].Title != results[3].Title {
			t.Errorf("expected second result to be MULTI, got %q", filtered[1].Title)
		}
	})

	t.Run("OR mode accepts any language match", func(t *testing.T) {
		filtered := FilterByProfile(results, nil, nil, []string{"hun", "eng"}, nil, "or")
		// Should match: HUN.ENG, ENG, MULTI (not GER)
		if len(filtered) != 3 {
			t.Fatalf("expected 3 results, got %d: %v", len(filtered), titles(filtered))
		}
	})

	t.Run("no languages = no filter", func(t *testing.T) {
		filtered := FilterByProfile(results, nil, nil, nil, nil, "or")
		if len(filtered) != 4 {
			t.Fatalf("expected 4 results, got %d", len(filtered))
		}
	})
}

func TestRankResults(t *testing.T) {
	results := []TorrentResult{
		{Title: "Movie.2024.720p.ENG.WEBRip.x264", Seeders: 300},
		{Title: "Movie.2024.1080p.HUN.BluRay.x264", Seeders: 100},
		{Title: "Movie.2024.1080p.ENG.WEB-DL.x264", Seeders: 200},
		{Title: "Movie.2024.720p.HUN.ENG.BluRay.x264", Seeders: 50},
	}

	t.Run("resolution priority", func(t *testing.T) {
		ranked := RankResults(
			copyResults(results),
			[]string{"1080p", "720p"}, // prefer 1080p
			nil,
			nil,
			"or",
		)
		// 1080p should come first
		if ranked[0].Title != results[1].Title && ranked[0].Title != results[2].Title {
			t.Errorf("expected 1080p first, got %q", ranked[0].Title)
		}
	})

	t.Run("resolution then language priority", func(t *testing.T) {
		ranked := RankResults(
			copyResults(results),
			[]string{"1080p", "720p"},
			nil,
			[]string{"hun", "eng"},
			"or",
		)
		// 1080p+HUN should be first (res=1, lang=1)
		if ranked[0].Title != results[1].Title {
			t.Errorf("expected 1080p.HUN first, got %q", ranked[0].Title)
		}
		// 1080p+ENG should be second (res=1, lang=2)
		if ranked[1].Title != results[2].Title {
			t.Errorf("expected 1080p.ENG second, got %q", ranked[1].Title)
		}
	})

	t.Run("source priority", func(t *testing.T) {
		ranked := RankResults(
			copyResults(results),
			nil,
			[]string{"bluray", "webdl", "webrip"},
			nil,
			"or",
		)
		// BluRay should come before WEB-DL/WEBRip
		if ranked[0].Title != results[1].Title && ranked[0].Title != results[3].Title {
			t.Errorf("expected bluray first, got %q", ranked[0].Title)
		}
	})

	t.Run("AND mode skips language ranking", func(t *testing.T) {
		ranked := RankResults(
			copyResults(results),
			nil,
			nil,
			[]string{"hun", "eng"},
			"and",
		)
		// Order should be unchanged (no ranking applied)
		for i := range results {
			if ranked[i].Title != results[i].Title {
				t.Errorf("pos %d: expected %q, got %q", i, results[i].Title, ranked[i].Title)
			}
		}
	})
}

func TestFilterByMediaProfile(t *testing.T) {
	results := []TorrentResult{
		{Title: "Movie.2024.1080p.HUN.ENG.BluRay.x264", Seeders: 100},
		{Title: "Movie.2024.720p.ENG.WEB-DL.x264", Seeders: 200},
		{Title: "Movie.2024.1080p.GER.BluRay.x264", Seeders: 150},
	}

	profile := &store.MediaProfile{
		Resolutions:  `["1080p","720p"]`,
		Languages:    `["hun","eng"]`,
		LanguageMode: "and",
		Sources:      `["bluray"]`,
	}

	filtered := FilterByMediaProfile(results, profile)

	// AND mode: only HUN+ENG passes language filter
	// Source: only BluRay passes
	// So only the first result should match
	if len(filtered) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(filtered), titles(filtered))
	}
	if filtered[0].Title != results[0].Title {
		t.Errorf("expected %q, got %q", results[0].Title, filtered[0].Title)
	}
}

func titles(results []TorrentResult) []string {
	t := make([]string, len(results))
	for i, r := range results {
		t[i] = r.Title
	}
	return t
}

func copyResults(results []TorrentResult) []TorrentResult {
	c := make([]TorrentResult, len(results))
	copy(c, results)
	return c
}

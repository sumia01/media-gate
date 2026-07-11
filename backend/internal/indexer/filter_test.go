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

	t.Run("OR mode: untagged release passes when eng in profile (English fallback)", func(t *testing.T) {
		// Indexers often omit the language tag for English-only releases.
		// In OR mode with "eng" in the profile, such releases should pass.
		withUntagged := []TorrentResult{
			{Title: "Movie.2024.1080p.BluRay.x264-RARBG", Seeders: 500}, // no language tag
			{Title: "Movie.2024.1080p.GER.BluRay.x264", Seeders: 150},
		}
		filtered := FilterByProfile(withUntagged, nil, nil, []string{"hun", "eng"}, nil, "or")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 result, got %d: %v", len(filtered), titles(filtered))
		}
		if filtered[0].Title != withUntagged[0].Title {
			t.Errorf("expected untagged release to pass, got %q", filtered[0].Title)
		}
	})

	t.Run("OR mode: untagged release dropped when eng NOT in profile", func(t *testing.T) {
		withUntagged := []TorrentResult{
			{Title: "Movie.2024.1080p.BluRay.x264-RARBG", Seeders: 500}, // no language tag
			{Title: "Movie.2024.1080p.HUN.BluRay.x264", Seeders: 100},
		}
		filtered := FilterByProfile(withUntagged, nil, nil, []string{"hun", "ger"}, nil, "or")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 result, got %d: %v", len(filtered), titles(filtered))
		}
		if filtered[0].Title != withUntagged[1].Title {
			t.Errorf("expected only HUN release, got %q", filtered[0].Title)
		}
	})

	t.Run("AND mode: untagged release passes for eng-only profile", func(t *testing.T) {
		withUntagged := []TorrentResult{
			{Title: "Movie.2024.1080p.BluRay.x264-RARBG", Seeders: 500},
		}
		filtered := FilterByProfile(withUntagged, nil, nil, []string{"eng"}, nil, "and")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 result, got %d", len(filtered))
		}
	})

	t.Run("AND mode: untagged release still dropped for multi-language profile", func(t *testing.T) {
		// English fallback only adds "eng" virtually; hun is still missing.
		withUntagged := []TorrentResult{
			{Title: "Movie.2024.1080p.BluRay.x264-RARBG", Seeders: 500},
		}
		filtered := FilterByProfile(withUntagged, nil, nil, []string{"hun", "eng"}, nil, "and")
		if len(filtered) != 0 {
			t.Fatalf("expected 0 results, got %d: %v", len(filtered), titles(filtered))
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

func TestPreferReleases(t *testing.T) {
	results := []TorrentResult{
		{Title: "Silo.S01E01.1080p.WEB.H264-FLUX", Seeders: 500},
		{Title: "Silo.S01E01.1080p.WEB.H264-ETHEL", Seeders: 100},
		{Title: "Silo.S01E01.2160p.WEB.H264-NTb", Seeders: 300},
		{Title: "Silo.S01E01.1080p.WEB.H264-ethel.PROPER", Seeders: 50},
	}

	t.Run("floats a single matching keyword to the front, stable", func(t *testing.T) {
		got := PreferReleases(copyResults(results), "ETHEL")
		// Both ETHEL releases float up, preserving their relative order;
		// non-matching releases keep their relative order below.
		want := []string{results[1].Title, results[3].Title, results[0].Title, results[2].Title}
		assertTitles(t, got, want)
	})

	t.Run("case-insensitive substring match", func(t *testing.T) {
		got := PreferReleases(copyResults(results), "ethel")
		if got[0].Title != results[1].Title {
			t.Errorf("expected an ETHEL release first, got %q", got[0].Title)
		}
	})

	t.Run("multiple comma-separated keywords with whitespace", func(t *testing.T) {
		got := PreferReleases(copyResults(results), "  NTb , FLUX ")
		// FLUX (idx 0) and NTb (idx 2) float up preserving original order.
		want := []string{results[0].Title, results[2].Title, results[1].Title, results[3].Title}
		assertTitles(t, got, want)
	})

	t.Run("empty preference is a no-op", func(t *testing.T) {
		got := PreferReleases(copyResults(results), "")
		assertTitles(t, got, titles(results))
		got = PreferReleases(copyResults(results), "  ,  ")
		assertTitles(t, got, titles(results))
	})

	t.Run("no match leaves order unchanged", func(t *testing.T) {
		got := PreferReleases(copyResults(results), "NONEXISTENT")
		assertTitles(t, got, titles(results))
	})

	t.Run("handles empty and single-element input", func(t *testing.T) {
		if got := PreferReleases(nil, "ETHEL"); len(got) != 0 {
			t.Errorf("expected empty result, got %v", titles(got))
		}
		single := []TorrentResult{{Title: "Silo.S01E01-ETHEL"}}
		if got := PreferReleases(single, "ETHEL"); len(got) != 1 {
			t.Errorf("expected single result preserved, got %v", titles(got))
		}
	})
}

func TestMatchesPreferredRelease(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		preferred string
		want      bool
	}{
		{"single match", "Silo.S01E01.1080p.WEB.H264-ETHEL", "ETHEL", true},
		{"case-insensitive", "Silo.S01E01-ethel", "ETHEL", true},
		{"one of multiple keywords", "Silo.S01E01-FLUX", "ETHEL, FLUX", true},
		{"no match", "Silo.S01E01-NTb", "ETHEL, FLUX", false},
		{"empty preference", "Silo.S01E01-ETHEL", "", false},
		{"whitespace-only preference", "Silo.S01E01-ETHEL", "  ,  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesPreferredRelease(tt.title, tt.preferred); got != tt.want {
				t.Errorf("MatchesPreferredRelease(%q, %q) = %v, want %v", tt.title, tt.preferred, got, tt.want)
			}
		})
	}
}

func assertTitles(t *testing.T, got []TorrentResult, want []string) {
	t.Helper()
	gotTitles := titles(got)
	if len(gotTitles) != len(want) {
		t.Fatalf("length mismatch: got %v, want %v", gotTitles, want)
	}
	for i := range want {
		if gotTitles[i] != want[i] {
			t.Fatalf("pos %d: got %q, want %q (full: %v)", i, gotTitles[i], want[i], gotTitles)
		}
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

package fileparse

import "testing"

func TestMatchesProfile(t *testing.T) {
	tests := []struct {
		name        string
		resolution  string
		source      string
		resolutions []string
		sources     []string
		want        bool
	}{
		{"exact resolution match", "1080p", "webdl", []string{"1080p"}, nil, true},
		{"resolution in list", "1080p", "webdl", []string{"2160p", "1080p"}, nil, true},
		{"resolution not in list", "720p", "webdl", []string{"2160p", "1080p"}, nil, false},
		{"empty resolution rejected", "", "webdl", []string{"1080p"}, nil, false},
		{"source match", "1080p", "bluray", []string{"1080p"}, []string{"bluray", "webdl"}, true},
		{"source not in list", "1080p", "hdtv", []string{"1080p"}, []string{"bluray", "webdl"}, false},
		{"empty source rejected when sources required", "1080p", "", []string{"1080p"}, []string{"bluray"}, false},
		{"no profile constraints", "720p", "hdtv", nil, nil, true},
		{"no resolution constraint", "720p", "hdtv", nil, []string{"hdtv"}, true},
		{"no source constraint", "1080p", "hdtv", []string{"1080p"}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesProfile(tt.resolution, tt.source, tt.resolutions, tt.sources)
			if got != tt.want {
				t.Errorf("MatchesProfile(%q, %q, %v, %v) = %v, want %v",
					tt.resolution, tt.source, tt.resolutions, tt.sources, got, tt.want)
			}
		})
	}
}

func TestMatchesLanguages(t *testing.T) {
	tests := []struct {
		name     string
		detected []string
		profile  []string
		mode     string
		want     bool
	}{
		// AND mode
		{"and: all present", []string{"hun", "eng"}, []string{"hun", "eng"}, "and", true},
		{"and: missing one", []string{"hun"}, []string{"hun", "eng"}, "and", false},
		{"and: none present", []string{"ger"}, []string{"hun", "eng"}, "and", false},
		{"and: superset detected", []string{"hun", "eng", "ger"}, []string{"hun", "eng"}, "and", true},
		{"and: empty detected with hun+eng profile still false", nil, []string{"hun", "eng"}, "and", false},
		{"and: multi satisfies all", []string{"multi"}, []string{"hun", "eng"}, "and", true},

		// OR mode
		{"or: one present", []string{"hun"}, []string{"hun", "eng"}, "or", true},
		{"or: other present", []string{"eng"}, []string{"hun", "eng"}, "or", true},
		{"or: none present", []string{"ger"}, []string{"hun", "eng"}, "or", false},
		{"or: multi satisfies", []string{"multi"}, []string{"hun", "eng"}, "or", true},

		// English fallback: untagged release treated as English if "eng" in profile.
		{"or: empty detected falls back to eng when in profile", nil, []string{"hun", "eng"}, "or", true},
		{"or: empty detected no eng in profile stays false", nil, []string{"hun", "ger"}, "or", false},
		{"or: empty detected eng-only profile", nil, []string{"eng"}, "or", true},
		{"and: empty detected eng-only profile", nil, []string{"eng"}, "and", true},
		{"and: empty detected hun+eng profile cannot satisfy hun", nil, []string{"hun", "eng"}, "and", false},
		{"or: detected ger ignores eng fallback", []string{"ger"}, []string{"hun", "eng"}, "or", false},

		// No profile languages = no filter
		{"no profile languages", []string{"hun"}, nil, "and", true},
		{"no profile languages or mode", []string{"hun"}, nil, "or", true},
		{"no profile no detected", nil, nil, "and", true},

		// Empty mode defaults to OR
		{"empty mode defaults to or", []string{"hun"}, []string{"hun", "eng"}, "", true},
		{"empty mode defaults to or with eng fallback", nil, []string{"hun", "eng"}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesLanguages(tt.detected, tt.profile, tt.mode)
			if got != tt.want {
				t.Errorf("MatchesLanguages(%v, %v, %q) = %v, want %v",
					tt.detected, tt.profile, tt.mode, got, tt.want)
			}
		})
	}
}

func TestLanguageScore(t *testing.T) {
	tests := []struct {
		name     string
		detected []string
		profile  []string
		want     int
	}{
		{"no profile languages", []string{"hun"}, nil, 0},
		{"first priority match", []string{"hun", "eng"}, []string{"hun", "eng"}, 1},
		{"second priority match", []string{"eng"}, []string{"hun", "eng"}, 2},
		{"no match worst score", []string{"ger"}, []string{"hun", "eng"}, 3},
		{"multi gets best score", []string{"multi"}, []string{"hun", "eng"}, 1},
		// English fallback: untagged release scored as English when "eng" in profile.
		{"empty detected falls back to eng (2nd in profile)", nil, []string{"hun", "eng"}, 2},
		{"empty detected falls back to eng (1st in profile)", nil, []string{"eng", "hun"}, 1},
		{"empty detected eng-only profile", nil, []string{"eng"}, 1},
		{"empty detected no eng in profile worst score", nil, []string{"hun", "ger"}, 3},
		{"detected ger ignores eng fallback worst score", []string{"ger"}, []string{"hun", "eng"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LanguageScore(tt.detected, tt.profile)
			if got != tt.want {
				t.Errorf("LanguageScore(%v, %v) = %d, want %d",
					tt.detected, tt.profile, got, tt.want)
			}
		})
	}
}

func TestPriorityScore(t *testing.T) {
	tests := []struct {
		name       string
		val        string
		priorities []string
		want       int
	}{
		{"empty priorities", "1080p", nil, 0},
		{"first priority", "1080p", []string{"1080p", "720p"}, 1},
		{"second priority", "720p", []string{"1080p", "720p"}, 2},
		{"not in list", "480p", []string{"1080p", "720p"}, 3},
		{"single item match", "bluray", []string{"bluray"}, 1},
		{"single item no match", "webdl", []string{"bluray"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PriorityScore(tt.val, tt.priorities)
			if got != tt.want {
				t.Errorf("PriorityScore(%q, %v) = %d, want %d",
					tt.val, tt.priorities, got, tt.want)
			}
		})
	}
}

func TestContainsExcludedTag(t *testing.T) {
	tests := []struct {
		name  string
		title string
		tags  []string
		want  bool
	}{
		{"tag found", "Movie.2024.3D.1080p.BluRay", []string{"3d"}, true},
		{"tag not found", "Movie.2024.1080p.BluRay", []string{"3d"}, false},
		{"case insensitive", "Movie.CAM.2024", []string{"cam"}, true},
		{"multiple tags one match", "Movie.TS.2024", []string{"cam", "ts"}, true},
		{"empty tags", "Movie.2024", nil, false},
		{"empty tag string ignored", "Movie.2024", []string{""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsExcludedTag(tt.title, tt.tags)
			if got != tt.want {
				t.Errorf("ContainsExcludedTag(%q, %v) = %v, want %v",
					tt.title, tt.tags, got, tt.want)
			}
		})
	}
}

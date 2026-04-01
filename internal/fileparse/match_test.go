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

func TestContainsExcludedTag(t *testing.T) {
	tests := []struct {
		name string
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

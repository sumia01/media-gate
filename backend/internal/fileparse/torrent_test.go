package fileparse

import "testing"

func TestParseTorrentSeasonEpisode(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		wantSeason *int
		wantEp     *int
		wantEnd    *int
	}{
		{"standard S01E05", "Show.S01E05.1080p.WEB-DL", intPtr(1), intPtr(5), nil},
		{"range S02E01-E10", "Show.S02E01-E10.720p.BluRay", intPtr(2), intPtr(1), intPtr(10)},
		{"season only S03", "Show.S03.1080p.WEB-DL", intPtr(3), nil, nil},
		{"season episode spelled out", "Show Season 2 Episode 5 720p", intPtr(2), intPtr(5), nil},
		{"season only spelled out", "Show Season 1 Complete 1080p", intPtr(1), nil, nil},
		{"cross style 2x05", "Show.2x05.HDTV", intPtr(2), intPtr(5), nil},
		{"no match", "Just.A.Movie.2024.1080p.BluRay", nil, nil, nil},
		{"S01E05 case insensitive", "show.s01e05.hdtv", intPtr(1), intPtr(5), nil},
		{"season pack with dot", "Show.S02.COMPLETE.1080p", intPtr(2), nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTorrentSeasonEpisode(tt.title)
			if !intPtrEq(got.Season, tt.wantSeason) {
				t.Errorf("Season = %v, want %v", deref(got.Season), deref(tt.wantSeason))
			}
			if !intPtrEq(got.Episode, tt.wantEp) {
				t.Errorf("Episode = %v, want %v", deref(got.Episode), deref(tt.wantEp))
			}
			if !intPtrEq(got.EpisodeEnd, tt.wantEnd) {
				t.Errorf("EpisodeEnd = %v, want %v", deref(got.EpisodeEnd), deref(tt.wantEnd))
			}
		})
	}
}

func TestParseResolution(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Movie.2024.1080p.BluRay", "1080p"},
		{"Show.S01E01.2160p.WEB-DL", "2160p"},
		{"Movie.720p.HDTV", "720p"},
		{"Movie.480p.DVDRip", "480p"},
		{"No.Resolution.Here", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseResolution(tt.input); got != tt.want {
				t.Errorf("ParseResolution(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Movie.2024.1080p.BluRay", "bluray"},
		{"Movie.2024.WEB-DL", "webdl"},
		{"Movie.2024.WEBRip", "webrip"},
		{"Movie.2024.HDTV", "hdtv"},
		{"Movie.2024.DVDRip", "dvdrip"},
		{"Movie.2024.1080p", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseSource(tt.input); got != tt.want {
				t.Errorf("ParseSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func intPtrEq(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func deref(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return string(rune('0' + *p))
}

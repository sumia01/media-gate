package fileparse

import (
	"testing"
)

func intPtr(v int) *int { return &v }

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     FileInfo
	}{
		{
			name:     "standard S##E## with resolution and source",
			filename: "The.Show.S02E05.1080p.BluRay.x264.mkv",
			want:     FileInfo{Resolution: "1080p", SourceType: "bluray", SeasonNumber: intPtr(2), EpisodeNumber: intPtr(5)},
		},
		{
			name:     "S##E## uppercase",
			filename: "Show.Name.S01E12.720p.HDTV.mkv",
			want:     FileInfo{Resolution: "720p", SourceType: "hdtv", SeasonNumber: intPtr(1), EpisodeNumber: intPtr(12)},
		},
		{
			name:     "##x## format",
			filename: "Show.Name.2x03.480p.DVDRip.avi",
			want:     FileInfo{Resolution: "480p", SourceType: "dvdrip", SeasonNumber: intPtr(2), EpisodeNumber: intPtr(3)},
		},
		{
			name:     "4K WEB-DL",
			filename: "Movie.Name.2024.2160p.WEB-DL.DDP5.1.mkv",
			want:     FileInfo{Resolution: "2160p", SourceType: "webdl"},
		},
		{
			name:     "webrip source",
			filename: "Show.S03E01.WEBRip.x264.mp4",
			want:     FileInfo{Resolution: "", SourceType: "webrip", SeasonNumber: intPtr(3), EpisodeNumber: intPtr(1)},
		},
		{
			name:     "blu-ray with hyphen",
			filename: "Movie.2022.1080p.Blu-Ray.REMUX.mkv",
			want:     FileInfo{Resolution: "1080p", SourceType: "bluray"},
		},
		{
			name:     "no resolution or source",
			filename: "random_video.mkv",
			want:     FileInfo{},
		},
		{
			name:     "3-digit episode number",
			filename: "Show.S01E100.1080p.mkv",
			want:     FileInfo{Resolution: "1080p", SeasonNumber: intPtr(1), EpisodeNumber: intPtr(100)},
		},
		{
			name:     "lowercase s##e##",
			filename: "show.s05e10.720p.hdtv.mkv",
			want:     FileInfo{Resolution: "720p", SourceType: "hdtv", SeasonNumber: intPtr(5), EpisodeNumber: intPtr(10)},
		},
		{
			name:     "movie with year only",
			filename: "Great.Movie.2023.720p.BRRip.mkv",
			want:     FileInfo{Resolution: "720p", SourceType: "bluray"},
		},
		{
			name:     "web-rip variant",
			filename: "Show.S01E01.Web-Rip.mp4",
			want:     FileInfo{SourceType: "webrip", SeasonNumber: intPtr(1), EpisodeNumber: intPtr(1)},
		},
		{
			name:     "Hungarian évad rész",
			filename: "Gordon Ramsay - 24 óra-Pokoli éttermek - 1. évad 02. rész.mkv",
			want:     FileInfo{SeasonNumber: intPtr(1), EpisodeNumber: intPtr(2)},
		},
		{
			name:     "Hungarian évad rész season 3",
			filename: "Gordon Ramsay - 24 óra-Pokoli éttermek - 3. évad 10. rész.mkv",
			want:     FileInfo{SeasonNumber: intPtr(3), EpisodeNumber: intPtr(10)},
		},
		{
			name:     "standalone E### Dragon Ball Z",
			filename: "Dragon.Ball.Z.E085.-.1080p.BluRay.2.0.x264.HUN.ENG.JAP-ClunkyChip.mkv",
			want:     FileInfo{Resolution: "1080p", SourceType: "bluray", EpisodeNumber: intPtr(85)},
		},
		{
			name:     "standalone E### Dragon Ball Super",
			filename: "Dragon.Ball.Super.E043.mkv",
			want:     FileInfo{EpisodeNumber: intPtr(43)},
		},
		{
			name:     "dot episode Kacsamesek",
			filename: "Kacsamesek.63.Minden.Kacsa.A.Fedelzetre.DVDRip.XviD.Hun-Coopter.avi",
			want:     FileInfo{SourceType: "dvdrip", EpisodeNumber: intPtr(63)},
		},
		{
			name:     "dot episode Kacsamesek single digit",
			filename: "Kacsamesek.01.Hasonmasok.DVDRip.XviD.Hun-Coopter.avi",
			want:     FileInfo{SourceType: "dvdrip", EpisodeNumber: intPtr(1)},
		},
		// Bug #23 regression: WxH resolution tokens must NOT be parsed as SxE.
		{
			name:     "resolution 1920x1080 not parsed as SxE",
			filename: "Movie.2024.1920x1080.x264.mkv",
			want:     FileInfo{},
		},
		{
			name:     "resolution 1280x720 not parsed as SxE",
			filename: "Show.Name.101.1280x720.mkv",
			want:     FileInfo{},
		},
		{
			name:     "resolution 720x404 not parsed as SxE",
			filename: "Show.S01.720x404.HDTV.mkv",
			want:     FileInfo{SourceType: "hdtv"},
		},
		// Legitimate ##x## episode notation must still parse.
		{
			name:     "legitimate 1x05 still parses",
			filename: "Show.1x05.mkv",
			want:     FileInfo{SeasonNumber: intPtr(1), EpisodeNumber: intPtr(5)},
		},
		{
			name:     "legitimate 12x34 still parses",
			filename: "Series 12x34",
			want:     FileInfo{SeasonNumber: intPtr(12), EpisodeNumber: intPtr(34)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.filename)
			if got.Resolution != tt.want.Resolution {
				t.Errorf("Resolution = %q, want %q", got.Resolution, tt.want.Resolution)
			}
			if got.SourceType != tt.want.SourceType {
				t.Errorf("SourceType = %q, want %q", got.SourceType, tt.want.SourceType)
			}
			if !intPtrEqual(got.SeasonNumber, tt.want.SeasonNumber) {
				t.Errorf("SeasonNumber = %v, want %v", ptrStr(got.SeasonNumber), ptrStr(tt.want.SeasonNumber))
			}
			if !intPtrEqual(got.EpisodeNumber, tt.want.EpisodeNumber) {
				t.Errorf("EpisodeNumber = %v, want %v", ptrStr(got.EpisodeNumber), ptrStr(tt.want.EpisodeNumber))
			}
		})
	}
}

func TestIsVideoFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"movie.mkv", true},
		{"movie.MKV", true},
		{"movie.mp4", true},
		{"movie.avi", true},
		{"movie.ts", true},
		{"movie.wmv", true},
		{"movie.flv", true},
		{"movie.srt", false},
		{"movie.nfo", false},
		{"movie.txt", false},
		{"movie.jpg", false},
		{"noext", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsVideoFile(tt.name); got != tt.want {
				t.Errorf("IsVideoFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func ptrStr(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return string(rune('0' + *p))
}

func TestIsJunkFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"setup.exe", true},
		{"install.msi", true},
		{"run.bat", true},
		{"script.cmd", true},
		{"shortcut.lnk", true},
		{"RARBG.txt", true},
		{"RARBG_DO_NOT_MIRROR.exe", true},
		{"WWW.YTS.MX.txt", true},
		{"WWW.TORRENTS.ORG.txt", true},
		{"movie.mkv", false},
		{"movie.srt", false},
		{"movie.nfo", false},
		{"movie.jpg", false},
		{"Subs/english.srt", false},
		{"movie.ass", false},
		{"movie.sub", false},
		{"movie.idx", false},
		{"readme.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsJunkFile(tt.name); got != tt.want {
				t.Errorf("IsJunkFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsSampleFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// Positive: sample in directory name
		{"Sample/movie.mkv", true},
		{"sample/movie.mkv", true},
		// Positive: sample as token in filename
		{"movie.sample.mkv", true},
		{"Movie-Sample.avi", true},
		{"Movie_Sample_2019.mkv", true},
		{"Movie Sample.mkv", true},
		// Positive: standalone sample filename
		{"sample.mkv", true},
		{"SAMPLE.MKV", true},
		// Positive: bare directory name
		{"Sample", true},
		// Negative: not a word boundary
		{"Sampler.mkv", false},
		{"example.mkv", false},
		{"resampled.mkv", false},
		// Negative: normal files
		{"movie.mkv", false},
		{"Movie.2019.1080p.mkv", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsSampleFile(tt.path); got != tt.want {
				t.Errorf("IsSampleFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestParseSeasonFromDir(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want *int
	}{
		{"exact Season 01", "Season 01", intPtr(1)},
		{"exact S1", "S1", intPtr(1)},
		{"exact season 2", "season 2", intPtr(2)},
		{"exact Season 12", "Season 12", intPtr(12)},
		{"suffix ShowName Season 1", "ShowName Season 1", intPtr(1)},
		{"suffix Breaking Bad - S02", "Breaking Bad - S02", intPtr(2)},
		{"suffix with dots", "Show.Name.Season.3", intPtr(3)},
		{"no season", "Breaking Bad", nil},
		{"no season just year", "ShowName (2020)", nil},
		{"suffix S## with trailing resolution", "Az Oroszlán őrség S01 1080p", intPtr(1)},
		{"suffix S## with trailing resolution 720p", "A Büszke Birtok Oroszlán őrsége S02 720p", intPtr(2)},
		{"dot-separated S## with metadata", "The.Lion.Guard.S03.720p.DSNP.WEBRip.DDP5.1.x264", intPtr(3)},
		{"Hungarian évad", "Gordon Ramsay 24 óra. Pokoli éttermek 1. évad", intPtr(1)},
		{"Hungarian évad season 3", "Gordon Ramsay 24 óra. Pokoli éttermek 3. évad", intPtr(3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSeasonFromDir(tt.dir)
			if !intPtrEqual(got, tt.want) {
				t.Errorf("ParseSeasonFromDir(%q) = %v, want %v", tt.dir, ptrStr(got), ptrStr(tt.want))
			}
		})
	}
}

func TestStripSeasonSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with Season N", "Breaking Bad Season 1", "Breaking Bad"},
		{"with S##", "Breaking Bad - S02", "Breaking Bad"},
		{"with dots", "Breaking.Bad.Season.3", "Breaking Bad"},
		{"no season", "Breaking Bad", "Breaking Bad"},
		{"exact season dir", "Season 01", "Season 01"}, // exact match not stripped (no base title)
		{"standalone S1", "S1", "S1"},                   // no base title
		{"S## with trailing resolution", "Az Oroszlán őrség S01 1080p", "Az Oroszlán őrség"},
		{"dot-separated S## with metadata", "The.Lion.Guard.S03.720p.DSNP.WEBRip.DDP5.1.x264", "The Lion Guard"},
		{"Hungarian évad", "Gordon Ramsay 24 óra. Pokoli éttermek 1. évad", "Gordon Ramsay 24 óra. Pokoli éttermek"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSeasonSuffix(tt.input)
			if got != tt.want {
				t.Errorf("StripSeasonSuffix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

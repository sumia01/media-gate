package fileparse

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type FileInfo struct {
	Resolution    string // "2160p", "1080p", "720p", "480p", ""
	SourceType    string // "bluray", "webdl", "webrip", "hdtv", "dvdrip", ""
	SeasonNumber  *int
	EpisodeNumber *int
}

var (
	seRe = regexp.MustCompile(`(?i)S(\d{1,2})E(\d{1,3})`)
	// Word-boundary guarded so WxH resolution tokens (e.g. "1920x1080", "1280x720")
	// aren't misread as season/episode. Since the literal "x" is itself a word
	// character, a longer digit run like "1920x1080" has no internal \b, so the
	// boundary can only anchor at the true start/end of the token — mirrors
	// torrent.go's torrentNxNRe.
	sxeRe = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
	resRe = regexp.MustCompile(`(?i)(2160|1080|720|480)p`)

	// Hungarian: "1. évad 02. rész" → season 1, episode 2
	hunEvadReszRe = regexp.MustCompile(`(\d{1,2})\.\s*évad\s+(\d{1,3})\.\s*rész`)
	// Standalone episode: ".E085." or ".E03." (no S## prefix)
	standaloneEpRe = regexp.MustCompile(`(?i)(?:^|[._\s-])E(\d{2,3})(?:[._\s-]|$)`)
	// Name.##.Title pattern: "Kacsamesek.63.Minden" — number between dots preceded by a word
	dotEpRe = regexp.MustCompile(`[A-Za-z]\.(\d{1,3})\.[A-Za-z]`)

	// Matches standalone season dir names: "Season 01", "S1", "season 2"
	seasonDirExactRe = regexp.MustCompile(`(?i)^(?:season|s)\s*(\d{1,2})$`)
	// Matches season suffix in folder names: "ShowName Season 1", "ShowName - S02"
	// Also matches when followed by trailing metadata: "ShowName S02 720p", "Show.S03.720p.WEBRip"
	seasonDirSuffixRe = regexp.MustCompile(`(?i)[\s._-]+(?:season|s)\s*(\d{1,2})(?:\s*$|[\s._-])`)
	// Hungarian: "... 1. évad" at end or with trailing metadata
	hunEvadDirRe = regexp.MustCompile(`(\d{1,2})\.\s*évad\s*$`)

	videoExts = map[string]bool{
		".mkv": true,
		".mp4": true,
		".avi": true,
		".ts":  true,
		".wmv": true,
		".flv": true,
	}

	junkExts = map[string]bool{
		".exe": true,
		".bat": true,
		".cmd": true,
		".msi": true,
		".lnk": true,
	}

	junkNameRe = regexp.MustCompile(`(?i)^(?:RARBG|WWW\..+\.txt$)`)

	// sampleRe matches "sample" as a discrete token in filenames and paths.
	sampleRe = regexp.MustCompile(`(?i)(?:^|[._\-/\\ ])sample(?:$|[._\-/\\ ])`)

	sourceAliases = map[string]string{
		"bluray":  "bluray",
		"blu-ray": "bluray",
		"bdrip":   "bluray",
		"brrip":   "bluray",
		"web-dl":  "webdl",
		"webdl":   "webdl",
		"web":     "webdl",
		"webrip":  "webrip",
		"web-rip": "webrip",
		"hdtv":    "hdtv",
		"pdtv":    "hdtv",
		"dvdrip":  "dvdrip",
		"dvd-rip": "dvdrip",
	}
)

func Parse(filename string) FileInfo {
	var info FileInfo

	// Season/episode: try S##E## first, then ##x##, then Hungarian, then standalone E##, then Name.##.
	if m := seRe.FindStringSubmatch(filename); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		info.SeasonNumber = &s
		info.EpisodeNumber = &e
	} else if m := sxeRe.FindStringSubmatch(filename); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		info.SeasonNumber = &s
		info.EpisodeNumber = &e
	} else if m := hunEvadReszRe.FindStringSubmatch(filename); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		info.SeasonNumber = &s
		info.EpisodeNumber = &e
	} else if m := standaloneEpRe.FindStringSubmatch(filename); m != nil {
		e, _ := strconv.Atoi(m[1])
		info.EpisodeNumber = &e
	} else if m := dotEpRe.FindStringSubmatch(filename); m != nil {
		e, _ := strconv.Atoi(m[1])
		info.EpisodeNumber = &e
	}

	info.Resolution = ParseResolution(filename)
	info.SourceType = ParseSource(filename)

	return info
}

// ParseResolution extracts a resolution string from text (e.g. "1080p", "2160p").
func ParseResolution(text string) string {
	if m := resRe.FindStringSubmatch(text); m != nil {
		return m[1] + "p"
	}
	return ""
}

// ParseSource extracts a canonical source type from text (e.g. "bluray", "webdl").
func ParseSource(text string) string {
	lower := strings.ToLower(text)
	bestAlias := ""
	bestCanonical := ""
	for alias, canonical := range sourceAliases {
		if strings.Contains(lower, alias) && len(alias) > len(bestAlias) {
			bestAlias = alias
			bestCanonical = canonical
		}
	}
	return bestCanonical
}

func IsVideoFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExts[ext]
}

// IsJunkFile returns true for files that should never be imported
// (executables, torrent spam). Subtitles, NFO, images etc. are NOT junk.
func IsJunkFile(name string) bool {
	base := filepath.Base(name)
	ext := strings.ToLower(filepath.Ext(base))
	if junkExts[ext] {
		return true
	}
	return junkNameRe.MatchString(base)
}

// IsSampleFile returns true if the path contains "sample" as a discrete token.
// Matches: "Sample/movie.mkv", "movie.sample.mkv", "Movie-Sample.avi", "Sample"
func IsSampleFile(path string) bool {
	return sampleRe.MatchString(path)
}

// ParseSeasonFromDir extracts a season number from a directory name.
// It handles these patterns:
//   - Exact season dirs: "Season 01", "S1", "season 2"
//   - Season suffix in show name: "ShowName Season 1", "ShowName - S02"
//   - Hungarian: "... 1. évad"
//
// Returns the season number or nil if no season pattern is found.
func ParseSeasonFromDir(name string) *int {
	clean := strings.ReplaceAll(name, ".", " ")

	if m := seasonDirExactRe.FindStringSubmatch(clean); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &n
	}
	if m := seasonDirSuffixRe.FindStringSubmatch(clean); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &n
	}
	if m := hunEvadDirRe.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &n
	}
	return nil
}

// StripSeasonSuffix removes a trailing season indicator (and any metadata after it)
// from a folder name, returning the base series title.
// E.g. "Breaking Bad Season 1" → "Breaking Bad",
//
//	"ShowName S02 720p" → "ShowName".
//
// If no season suffix is found, returns the input unchanged.
func StripSeasonSuffix(name string) string {
	clean := strings.ReplaceAll(name, ".", " ")

	// Try English pattern first
	loc := seasonDirSuffixRe.FindStringIndex(clean)
	if loc == nil {
		// Try Hungarian pattern (use original name, not dot-cleaned)
		loc = hunEvadDirRe.FindStringIndex(name)
		if loc != nil {
			clean = name
		}
	}
	if loc == nil {
		return name
	}
	stripped := strings.TrimRight(clean[:loc[0]], "- ")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return name
	}
	return stripped
}

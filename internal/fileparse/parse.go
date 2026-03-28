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
	seRe  = regexp.MustCompile(`(?i)S(\d{1,2})E(\d{1,3})`)
	sxeRe = regexp.MustCompile(`(?i)(\d{1,2})x(\d{1,3})`)
	resRe = regexp.MustCompile(`(?i)(2160|1080|720|480)p`)

	// Matches standalone season dir names: "Season 01", "S1", "season 2"
	seasonDirExactRe = regexp.MustCompile(`(?i)^(?:season|s)\s*(\d{1,2})$`)
	// Matches season suffix in folder names: "ShowName Season 1", "ShowName - S02"
	seasonDirSuffixRe = regexp.MustCompile(`(?i)[\s._-]+(?:season|s)\s*(\d{1,2})\s*$`)

	videoExts = map[string]bool{
		".mkv": true,
		".mp4": true,
		".avi": true,
		".ts":  true,
		".wmv": true,
		".flv": true,
	}

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

	// Season/episode: try S##E## first, then ##x##
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
	}

	// Resolution
	if m := resRe.FindStringSubmatch(filename); m != nil {
		info.Resolution = m[1] + "p"
	}

	// Source type: case-insensitive search through aliases
	lower := strings.ToLower(filename)
	for alias, canonical := range sourceAliases {
		if strings.Contains(lower, alias) {
			info.SourceType = canonical
			break
		}
	}

	return info
}

func IsVideoFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExts[ext]
}

// ParseSeasonFromDir extracts a season number from a directory name.
// It handles two patterns:
//   - Exact season dirs: "Season 01", "S1", "season 2"
//   - Season suffix in show name: "ShowName Season 1", "ShowName - S02"
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
	return nil
}

// StripSeasonSuffix removes a trailing season indicator from a folder name,
// returning the base series title. E.g. "Breaking Bad Season 1" → "Breaking Bad".
// If no season suffix is found, returns the input unchanged.
func StripSeasonSuffix(name string) string {
	clean := strings.ReplaceAll(name, ".", " ")
	stripped := seasonDirSuffixRe.ReplaceAllString(clean, "")
	stripped = strings.TrimRight(stripped, "- ")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return name
	}
	return stripped
}

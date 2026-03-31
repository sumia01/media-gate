package importer

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sumia01/media-gate/internal/store"
)

// illegalChars matches characters not allowed in file/folder names across OS.
var illegalChars = regexp.MustCompile(`[/\\:*?"<>|]`)

// BuildTargetDir returns the library directory where imported files should be placed.
// Movie:  {lib.Path}/{Title} ({Year})/
// Series: {lib.Path}/{Title} ({Year})/Season {XX}/
func BuildTargetDir(lib *store.Library, item *store.MediaItem, meta *store.MediaMetadata, seasonNumber *int) string {
	title, year := item.Title, item.Year
	if meta != nil {
		if meta.Title != "" {
			title = meta.Title
		}
		if meta.Year != nil {
			year = meta.Year
		}
	}

	folderName := sanitizePath(title)
	if year != nil {
		folderName = fmt.Sprintf("%s (%d)", folderName, *year)
	}

	dir := filepath.Join(lib.Path, folderName)

	if item.MediaType == "series" && seasonNumber != nil {
		dir = filepath.Join(dir, fmt.Sprintf("Season %02d", *seasonNumber))
	}

	return dir
}

// sanitizePath removes characters that are illegal in filenames and trims whitespace/dots.
func sanitizePath(name string) string {
	s := illegalChars.ReplaceAllString(name, "")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".")
	if s == "" {
		return "Unknown"
	}
	return s
}

// BuildReleaseFolderName returns a filesystem-safe folder name derived from a
// torrent/release title. The name is sanitized and truncated to 200 characters.
func BuildReleaseFolderName(title string) string {
	s := illegalChars.ReplaceAllString(title, "")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".")
	if len(s) > 200 {
		s = strings.TrimRight(s[:200], ". ")
	}
	if s == "" {
		return "release"
	}
	return s
}

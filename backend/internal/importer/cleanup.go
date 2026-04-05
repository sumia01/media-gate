package importer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sumia01/media-gate/internal/fileparse"
)

// RemoveEmptyParents removes empty directories from dir up to (but not including) stopAt.
func RemoveEmptyParents(dir, stopAt string) {
	for dir != stopAt && strings.HasPrefix(dir, stopAt) {
		if err := os.Remove(dir); err != nil {
			break // not empty or permission error
		}
		dir = filepath.Dir(dir)
	}
}

// OnlyCompanionsLeft returns true if a directory contains no video files —
// only companion files (subtitles, NFO, images) or is empty/already removed.
// It recurses into subdirectories.
func OnlyCompanionsLeft(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if !OnlyCompanionsLeft(filepath.Join(dir, e.Name())) {
				return false
			}
			continue
		}
		if fileparse.IsVideoFile(e.Name()) {
			return false
		}
	}
	return true
}

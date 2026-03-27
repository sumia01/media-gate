package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sumia01/media-gate/internal/store"
)

var yearRe = regexp.MustCompile(`[\(\[]?(\d{4})[\)\]]?\s*$`)

type Service struct {
	store store.Store
}

func NewService(s store.Store) *Service {
	return &Service{store: s}
}

func (s *Service) SyncLibrary(lib *store.Library) (added, removed int, err error) {
	entries, err := os.ReadDir(lib.Path)
	if err != nil {
		return 0, 0, fmt.Errorf("reading directory %s: %w", lib.Path, err)
	}

	// Collect directories from disk
	diskPaths := make(map[string]struct{})
	type folderInfo struct {
		name  string
		title string
		year  *int
		path  string
	}
	var folders []folderInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		fullPath := filepath.Join(lib.Path, name)
		title, year := parseFolderName(name)
		diskPaths[fullPath] = struct{}{}
		folders = append(folders, folderInfo{name: name, title: title, year: year, path: fullPath})
	}

	// Get existing items from DB
	existing, err := s.store.ListMediaItemsByLibrary(lib.ID)
	if err != nil {
		return 0, 0, fmt.Errorf("listing existing items: %w", err)
	}

	existingPaths := make(map[string]struct{}, len(existing))
	var removePaths []string
	for _, item := range existing {
		existingPaths[item.Path] = struct{}{}
		if _, ok := diskPaths[item.Path]; !ok {
			removePaths = append(removePaths, item.Path)
		}
	}

	// Remove items no longer on disk
	if len(removePaths) > 0 {
		if err := s.store.DeleteMediaItemsByPaths(lib.ID, removePaths); err != nil {
			return 0, 0, fmt.Errorf("removing stale items: %w", err)
		}
		removed = len(removePaths)
	}

	// Add new items from disk
	for _, f := range folders {
		if _, ok := existingPaths[f.path]; ok {
			continue
		}
		item := &store.MediaItem{
			LibraryID:  lib.ID,
			Title:      f.title,
			FolderName: f.name,
			Path:       f.path,
			MediaType:  lib.MediaType,
			Status:     "new",
			Year:       f.year,
		}
		if err := s.store.CreateMediaItem(item); err != nil {
			return added, removed, fmt.Errorf("creating media item %q: %w", f.name, err)
		}
		added++
	}

	return added, removed, nil
}

func parseFolderName(name string) (title string, year *int) {
	clean := strings.ReplaceAll(name, ".", " ")

	if m := yearRe.FindStringSubmatch(clean); m != nil {
		var y int
		fmt.Sscanf(m[1], "%d", &y)
		if y >= 1900 && y <= 2099 {
			year = &y
		}
		clean = strings.TrimSpace(yearRe.ReplaceAllString(clean, ""))
	}

	// Remove trailing dash/hyphen leftover
	clean = strings.TrimRight(clean, "- ")
	title = strings.TrimSpace(clean)
	if title == "" {
		title = name
	}
	return title, year
}

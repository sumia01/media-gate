package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

	// Get existing media files for this library to detect existing/removed items
	existingFiles, err := s.store.ListMediaFilesByLibrary(lib.ID)
	if err != nil {
		return 0, 0, fmt.Errorf("listing existing media files: %w", err)
	}

	existingPaths := make(map[string]uint, len(existingFiles)) // path → MediaItemID
	var removePaths []string
	for _, f := range existingFiles {
		existingPaths[f.Path] = f.MediaItemID
		if _, ok := diskPaths[f.Path]; !ok {
			removePaths = append(removePaths, f.Path)
		}
	}

	// Remove media files no longer on disk
	if len(removePaths) > 0 {
		if err := s.store.DeleteMediaFilesByPaths(removePaths); err != nil {
			return 0, 0, fmt.Errorf("removing stale media files: %w", err)
		}
		removed = len(removePaths)

		// Clean up orphaned disk-sourced MediaItems (items with no remaining files)
		orphanIDs := s.findOrphanedMediaItems(removePaths, existingFiles, existingPaths)
		for _, id := range orphanIDs {
			_ = s.store.DeleteMediaMetadataByMediaItem(id)
			_ = s.store.DeleteMediaItem(id)
		}
	}

	// Add new items from disk
	for _, f := range folders {
		if _, ok := existingPaths[f.path]; ok {
			continue
		}
		item := &store.MediaItem{
			LibraryID: lib.ID,
			Title:     f.title,
			MediaType: lib.MediaType,
			Status:    "new",
			Source:    "disk",
			Year:      f.year,
		}
		if err := s.store.CreateMediaItem(item); err != nil {
			return added, removed, fmt.Errorf("creating media item %q: %w", f.name, err)
		}
		// Create a MediaFile for this folder
		mf := &store.MediaFile{
			MediaItemID: item.ID,
			Path:        f.path,
			FileName:    f.name,
			AddedAt:     time.Now(),
		}
		if err := s.store.CreateMediaFile(mf); err != nil {
			return added, removed, fmt.Errorf("creating media file for %q: %w", f.name, err)
		}
		added++
	}

	return added, removed, nil
}

// findOrphanedMediaItems returns MediaItem IDs that have no remaining MediaFiles
// after the given paths were removed.
func (s *Service) findOrphanedMediaItems(removedPaths []string, allFiles []store.MediaFile, pathToItem map[string]uint) []uint {
	removedSet := make(map[string]struct{}, len(removedPaths))
	for _, p := range removedPaths {
		removedSet[p] = struct{}{}
	}

	// Count remaining files per media item
	fileCounts := make(map[uint]int)
	for _, f := range allFiles {
		if _, removed := removedSet[f.Path]; !removed {
			fileCounts[f.MediaItemID]++
		}
	}

	// Collect IDs of items with zero remaining files
	seen := make(map[uint]bool)
	var orphans []uint
	for _, p := range removedPaths {
		itemID := pathToItem[p]
		if seen[itemID] {
			continue
		}
		seen[itemID] = true
		if fileCounts[itemID] == 0 {
			orphans = append(orphans, itemID)
		}
	}
	return orphans
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

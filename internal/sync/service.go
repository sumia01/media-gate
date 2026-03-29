package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
)

var yearRe = regexp.MustCompile(`[\(\[]?(\d{4})[\)\]]?\s*$`)

type scannedFile struct {
	path          string
	fileName      string
	size          int64
	resolution    string
	sourceType    string
	seasonNumber  *int
	episodeNumber *int
}

type folderInfo struct {
	name  string
	title string
	year  *int
	path  string
	files []scannedFile
}

// mediaGroup represents a logical media item that may span multiple top-level folders.
// For movies: always one folder = one group.
// For series: multiple "ShowName Season N" folders get grouped under one title.
type mediaGroup struct {
	title   string
	year    *int
	folders []string       // top-level folder paths belonging to this group
	files   []scannedFile  // all video files across all folders
}

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

	// Phase 1: Scan all top-level directories and their video files
	var folders []folderInfo

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		fullPath := filepath.Join(lib.Path, name)
		title, year := parseFolderName(name)
		scanned := scanMediaFolder(fullPath)
		folders = append(folders, folderInfo{name: name, title: title, year: year, path: fullPath, files: scanned})
	}

	// Phase 2: Group folders into logical media items.
	// For series libraries: folders with season suffixes (e.g. "ShowName Season 1",
	// "ShowName Season 2") are grouped under the same base title.
	groups := groupFolders(folders, lib.MediaType)

	// Collect all disk file paths for removal detection
	diskFilePaths := make(map[string]struct{})
	for _, g := range groups {
		for _, sf := range g.files {
			diskFilePaths[sf.path] = struct{}{}
		}
	}

	// Phase 3: Get existing media files to detect existing/removed items
	existingFiles, err := s.store.ListMediaFilesByLibrary(lib.ID)
	if err != nil {
		return 0, 0, fmt.Errorf("listing existing media files: %w", err)
	}

	existingPaths := make(map[string]store.MediaFile, len(existingFiles))
	var removePaths []string
	for _, f := range existingFiles {
		existingPaths[f.Path] = f
		if _, ok := diskFilePaths[f.Path]; !ok {
			removePaths = append(removePaths, f.Path)
		}
	}

	// Remove media files no longer on disk
	if len(removePaths) > 0 {
		if err := s.store.DeleteMediaFilesByPaths(removePaths); err != nil {
			return 0, 0, fmt.Errorf("removing stale media files: %w", err)
		}
		removed = len(removePaths)

		pathToItem := make(map[string]uint, len(existingFiles))
		for _, f := range existingFiles {
			pathToItem[f.Path] = f.MediaItemID
		}
		orphanIDs := s.findOrphanedMediaItems(removePaths, existingFiles, pathToItem)
		for _, id := range orphanIDs {
			_ = s.store.DeleteMediaMetadataByMediaItem(id)
			_ = s.store.DeleteEpisodesByMediaItem(id)
			_ = s.store.DeleteMediaItem(id)
		}
	}

	// Phase 4: Build folder→MediaItemID map from existing files.
	// A top-level folder maps to a MediaItem if any existing file lives under it.
	folderToItem := make(map[string]uint)
	for _, f := range existingFiles {
		rel, _ := filepath.Rel(lib.Path, f.Path)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 {
			topDir := filepath.Join(lib.Path, parts[0])
			folderToItem[topDir] = f.MediaItemID
		}
	}

	// Phase 5: Process groups — create or update MediaItems
	for _, g := range groups {
		if len(g.files) == 0 {
			continue
		}

		// Find existing MediaItem for any folder in this group
		var mediaItemID uint
		exists := false
		for _, folderPath := range g.folders {
			if id, ok := folderToItem[folderPath]; ok {
				mediaItemID = id
				exists = true
				break
			}
		}

		if !exists {
			item := &store.MediaItem{
				LibraryID: lib.ID,
				Title:     g.title,
				MediaType: lib.MediaType,
				Status:    "new",
				Source:    "disk",
				Year:      g.year,
			}
			if err := s.store.CreateMediaItem(item); err != nil {
				return added, removed, fmt.Errorf("creating media item %q: %w", g.title, err)
			}
			mediaItemID = item.ID
			// Register all folders in this group so subsequent groups can find them
			for _, folderPath := range g.folders {
				folderToItem[folderPath] = mediaItemID
			}
		}

		// Create MediaFiles for new video files
		for _, sf := range g.files {
			if _, found := existingPaths[sf.path]; found {
				continue
			}
			mf := &store.MediaFile{
				MediaItemID:   mediaItemID,
				Path:          sf.path,
				FileName:      sf.fileName,
				Size:          sf.size,
				Resolution:    sf.resolution,
				SourceType:    sf.sourceType,
				SeasonNumber:  sf.seasonNumber,
				EpisodeNumber: sf.episodeNumber,
				AddedAt:       time.Now(),
			}
			if err := s.store.CreateMediaFile(mf); err != nil {
				return added, removed, fmt.Errorf("creating media file %q: %w", sf.fileName, err)
			}
			added++
		}
	}

	return added, removed, nil
}

// ResyncMediaItem re-scans all files belonging to a single media item,
// updating resolution/source/season/episode on existing files and
// adding/removing files as needed.
func (s *Service) ResyncMediaItem(itemID uint) (updated, added, removed int, err error) {
	existingFiles, err := s.store.ListMediaFilesByMediaItem(itemID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("listing files: %w", err)
	}

	// Derive the set of top-level folders this item's files live in.
	// A file at /lib/ShowName/Season 01/ep.mkv → top folder = parent or grandparent
	// We collect all unique parent dirs (could be the media folder itself,
	// a season dir, or an episode wrapper dir).
	folderSet := make(map[string]struct{})
	for _, f := range existingFiles {
		dir := filepath.Dir(f.Path)
		folderSet[dir] = struct{}{}
	}

	// Walk up to find the top-level media folder(s) and re-scan from there.
	// We need the root folder(s) to pick up new season dirs or new files at any level.
	rootFolders := make(map[string]struct{})
	for dir := range folderSet {
		rootFolders[dir] = struct{}{}
		// Also include parent — covers season dirs and episode wrapper dirs
		parent := filepath.Dir(dir)
		rootFolders[parent] = struct{}{}
		grandparent := filepath.Dir(parent)
		rootFolders[grandparent] = struct{}{}
	}

	// Re-scan all candidate root folders and collect video files
	var freshFiles []scannedFile
	scannedRoots := make(map[string]bool)
	for dir := range rootFolders {
		if scannedRoots[dir] {
			continue
		}
		// Only scan directories that actually exist and are directories
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			continue
		}
		scanned := scanMediaFolder(dir)
		if len(scanned) > 0 {
			scannedRoots[dir] = true
			freshFiles = append(freshFiles, scanned...)
		}
	}

	// Deduplicate scanned files by path (multiple root scans may overlap)
	freshByPath := make(map[string]scannedFile, len(freshFiles))
	for _, sf := range freshFiles {
		freshByPath[sf.path] = sf
	}

	// Also include files scanned directly in any folder (flat layout — files
	// sitting next to season dirs won't be picked up by scanMediaFolder's
	// subdirectory descent, but they're already in existingFiles).
	// Re-parse them from the existing paths to get fresh metadata.
	for _, ef := range existingFiles {
		if _, already := freshByPath[ef.Path]; already {
			continue
		}
		// File might still exist on disk — re-parse it
		fi, err := os.Stat(ef.Path)
		if err != nil {
			continue // will be caught as removed below
		}
		info := fileparse.Parse(ef.FileName)
		freshByPath[ef.Path] = scannedFile{
			path:          ef.Path,
			fileName:      ef.FileName,
			size:          fi.Size(),
			resolution:    info.Resolution,
			sourceType:    info.SourceType,
			seasonNumber:  info.SeasonNumber,
			episodeNumber: info.EpisodeNumber,
		}
	}

	// Build lookup of existing files
	existingByPath := make(map[string]store.MediaFile, len(existingFiles))
	existingPathSet := make(map[string]struct{}, len(existingFiles))
	for _, f := range existingFiles {
		existingByPath[f.Path] = f
		existingPathSet[f.Path] = struct{}{}
	}

	// Update existing files with fresh metadata
	for path, sf := range freshByPath {
		ef, exists := existingByPath[path]
		if !exists {
			continue
		}
		if fileNeedsUpdate(ef, sf) {
			ef.Size = sf.size
			ef.Resolution = sf.resolution
			ef.SourceType = sf.sourceType
			ef.SeasonNumber = sf.seasonNumber
			ef.EpisodeNumber = sf.episodeNumber
			if err := s.store.UpdateMediaFile(&ef); err != nil {
				return updated, added, removed, fmt.Errorf("updating file %q: %w", sf.fileName, err)
			}
			updated++
		}
	}

	// Add new files (in freshByPath but not in existing)
	for path, sf := range freshByPath {
		if _, exists := existingPathSet[path]; exists {
			continue
		}
		mf := &store.MediaFile{
			MediaItemID:   itemID,
			Path:          sf.path,
			FileName:      sf.fileName,
			Size:          sf.size,
			Resolution:    sf.resolution,
			SourceType:    sf.sourceType,
			SeasonNumber:  sf.seasonNumber,
			EpisodeNumber: sf.episodeNumber,
			AddedAt:       time.Now(),
		}
		if err := s.store.CreateMediaFile(mf); err != nil {
			return updated, added, removed, fmt.Errorf("creating file %q: %w", sf.fileName, err)
		}
		added++
	}

	// Remove files no longer on disk
	for path := range existingPathSet {
		if _, exists := freshByPath[path]; !exists {
			// Verify the file is actually gone
			if _, err := os.Stat(path); os.IsNotExist(err) {
				removePaths := []string{path}
				if err := s.store.DeleteMediaFilesByPaths(removePaths); err != nil {
					return updated, added, removed, fmt.Errorf("removing file: %w", err)
				}
				removed++
			}
		}
	}

	return updated, added, removed, nil
}

func fileNeedsUpdate(existing store.MediaFile, fresh scannedFile) bool {
	if existing.Resolution != fresh.resolution {
		return true
	}
	if existing.SourceType != fresh.sourceType {
		return true
	}
	if existing.Size != fresh.size {
		return true
	}
	if !intPtrEqual(existing.SeasonNumber, fresh.seasonNumber) {
		return true
	}
	if !intPtrEqual(existing.EpisodeNumber, fresh.episodeNumber) {
		return true
	}
	return false
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

// groupFolders groups top-level folders into logical media items.
// For series: folders whose name contains a season indicator (e.g. "ShowName Season 1")
// are grouped under the base title stripped of the season suffix.
// For movies: each folder is its own group (1:1).
func groupFolders(folders []folderInfo, mediaType string) []mediaGroup {
	if mediaType != "series" {
		// Movies: 1 folder = 1 group
		groups := make([]mediaGroup, 0, len(folders))
		for _, f := range folders {
			groups = append(groups, mediaGroup{
				title:   f.title,
				year:    f.year,
				folders: []string{f.path},
				files:   f.files,
			})
		}
		return groups
	}

	// Series: detect season-folder patterns and group them
	groupMap := make(map[string]*mediaGroup) // normalized title → group
	var groupOrder []string                  // preserve insertion order

	for _, f := range folders {
		seasonNum := fileparse.ParseSeasonFromDir(f.name)

		var key string
		var title string
		if seasonNum != nil {
			// This folder has a season indicator — strip it to get the base series title
			title = fileparse.StripSeasonSuffix(f.name)
			// If StripSeasonSuffix returned the original (exact season dir like "Season 01"),
			// the folder itself IS the season dir, not a "ShowName Season N" pattern.
			// In that case, this is a standalone season dir which should not happen at library
			// root level (it would be inside a show folder). Treat as-is.
			if title == f.name {
				title = f.title
				key = strings.ToLower(title)
			} else {
				key = strings.ToLower(title)
			}
		} else {
			title = f.title
			key = strings.ToLower(title)
		}

		if g, ok := groupMap[key]; ok {
			g.folders = append(g.folders, f.path)
			g.files = append(g.files, f.files...)
			// Keep the year from the first folder that has one
			if g.year == nil && f.year != nil {
				g.year = f.year
			}
		} else {
			g := &mediaGroup{
				title:   title,
				year:    f.year,
				folders: []string{f.path},
				files:   f.files,
			}
			groupMap[key] = g
			groupOrder = append(groupOrder, key)
		}
	}

	groups := make([]mediaGroup, 0, len(groupOrder))
	for _, key := range groupOrder {
		groups = append(groups, *groupMap[key])
	}
	return groups
}

// scanMediaFolder walks a media folder and returns scanned video files.
// Supports:
// 1. Season subfolders (Season 01, S1, etc.) — descends into them
// 2. Flat layout — video files directly in the folder
// 3. Episode wrapper dirs inside season dirs — descends one more level
func scanMediaFolder(folderPath string) []scannedFile {
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return nil
	}

	var files []scannedFile

	for _, e := range entries {
		if e.IsDir() {
			subPath := filepath.Join(folderPath, e.Name())
			// Check if it's a season subfolder
			if sn := fileparse.ParseSeasonFromDir(e.Name()); sn != nil {
				seasonFiles := scanSeasonDir(subPath, sn)
				files = append(files, seasonFiles...)
			} else {
				// Non-season subdirectory (e.g. release folder, show-name subfolder).
				// Descend one level to pick up video files inside it.
				subFiles := scanVideoDir(subPath, nil, true)
				files = append(files, subFiles...)
			}
			continue
		}

		name := e.Name()
		if !fileparse.IsVideoFile(name) {
			continue
		}

		fullPath := filepath.Join(folderPath, name)
		info := fileparse.Parse(name)
		size := fileSize(fullPath)

		files = append(files, scannedFile{
			path:          fullPath,
			fileName:      name,
			size:          size,
			resolution:    info.Resolution,
			sourceType:    info.SourceType,
			seasonNumber:  info.SeasonNumber,
			episodeNumber: info.EpisodeNumber,
		})
	}

	return files
}

// scanSeasonDir scans video files inside a season subfolder.
// If a file doesn't have S##E## in its name, the subfolder's season number is used.
// Also descends into episode wrapper directories (e.g. "Episode 01 - Title/video.mkv").
func scanSeasonDir(dirPath string, seasonNumber *int) []scannedFile {
	return scanVideoDir(dirPath, seasonNumber, true)
}

// scanVideoDir scans video files in a directory.
// If recurse is true, it descends into subdirectories (one level).
func scanVideoDir(dirPath string, fallbackSeason *int, recurse bool) []scannedFile {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	var files []scannedFile
	for _, e := range entries {
		if e.IsDir() {
			if recurse {
				subFiles := scanVideoDir(filepath.Join(dirPath, e.Name()), fallbackSeason, false)
				files = append(files, subFiles...)
			}
			continue
		}
		name := e.Name()
		if !fileparse.IsVideoFile(name) {
			continue
		}

		sf := buildScannedFile(filepath.Join(dirPath, name), name, fallbackSeason)
		files = append(files, sf)
	}

	return files
}

// buildScannedFile creates a scannedFile from a video file path,
// using the provided seasonNumber as fallback if the filename lacks S##E##.
func buildScannedFile(fullPath, fileName string, fallbackSeason *int) scannedFile {
	info := fileparse.Parse(fileName)
	size := fileSize(fullPath)

	sn := info.SeasonNumber
	en := info.EpisodeNumber
	if sn == nil && fallbackSeason != nil {
		sn = fallbackSeason
	}

	return scannedFile{
		path:          fullPath,
		fileName:      fileName,
		size:          size,
		resolution:    info.Resolution,
		sourceType:    info.SourceType,
		seasonNumber:  sn,
		episodeNumber: en,
	}
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// findOrphanedMediaItems returns MediaItem IDs that have no remaining MediaFiles
// after the given paths were removed.
func (s *Service) findOrphanedMediaItems(removedPaths []string, allFiles []store.MediaFile, pathToItem map[string]uint) []uint {
	removedSet := make(map[string]struct{}, len(removedPaths))
	for _, p := range removedPaths {
		removedSet[p] = struct{}{}
	}

	fileCounts := make(map[uint]int)
	for _, f := range allFiles {
		if _, removed := removedSet[f.Path]; !removed {
			fileCounts[f.MediaItemID]++
		}
	}

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

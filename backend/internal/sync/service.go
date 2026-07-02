package sync

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
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
	bus   *eventbus.Bus
}

func NewService(s store.Store) *Service {
	return &Service{store: s}
}

// SetBus injects the event bus for publishing resync events.
func (s *Service) SetBus(b *eventbus.Bus) {
	s.bus = b
}

func (s *Service) SyncLibrary(lib *store.Library) (added, removed int, err error) {
	entries, err := os.ReadDir(lib.Path)
	if err != nil {
		return 0, 0, fmt.Errorf("reading directory %s: %w", lib.Path, err)
	}

	// Phase 1: Scan all top-level directories and their video files
	folders := make([]folderInfo, 0, len(entries))
	// scanFailed tracks top-level folders that could NOT be scanned (transient I/O
	// error — unmounted NAS, EACCES, EIO). Files under these folders must be excluded
	// from removal: a momentarily-unreadable folder is NOT the same as an empty one,
	// and treating it as empty would hard-delete the media item and its metadata.
	scanFailed := make(map[string]struct{})

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		fullPath := filepath.Join(lib.Path, name)
		title, year := parseFolderName(name)
		scanned, err := scanMediaFolder(fullPath)
		if err != nil {
			slog.Warn("sync: could not scan media folder; excluding its files from removal",
				"library_id", lib.ID, "path", fullPath, "error", err)
			scanFailed[fullPath] = struct{}{}
			continue
		}
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

	existingPaths := make(map[string]struct{}, len(existingFiles))
	var removePaths []string
	for i := range existingFiles {
		existingPaths[existingFiles[i].Path] = struct{}{}
		if _, ok := diskFilePaths[existingFiles[i].Path]; ok {
			continue
		}
		// The file was not seen on disk during this scan. If its top-level folder
		// could not be scanned (transient I/O error), do NOT treat the file as
		// removed — it is likely still present once the mount/permission issue
		// clears. Only genuinely-empty folders yield removals.
		if topDir := topLevelFolder(lib.Path, existingFiles[i].Path); topDir != "" {
			if _, failed := scanFailed[topDir]; failed {
				continue
			}
		}
		removePaths = append(removePaths, existingFiles[i].Path)
	}

	// Remove media files no longer on disk
	if len(removePaths) > 0 {
		if err := s.store.DeleteMediaFilesByPaths(removePaths); err != nil {
			return 0, 0, fmt.Errorf("removing stale media files: %w", err)
		}
		removed = len(removePaths)

		pathToItem := make(map[string]uint, len(existingFiles))
		for i := range existingFiles {
			pathToItem[existingFiles[i].Path] = existingFiles[i].MediaItemID
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
	for i := range existingFiles {
		if topDir := topLevelFolder(lib.Path, existingFiles[i].Path); topDir != "" {
			folderToItem[topDir] = existingFiles[i].MediaItemID
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

	slog.Info("sync: library sync complete", "library_id", lib.ID, "library", lib.Name, "files_added", added, "files_removed", removed)
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

	// Get the library path to use as a boundary — never scan above it.
	item, err := s.store.GetMediaItem(itemID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getting media item: %w", err)
	}
	lib, err := s.store.GetLibrary(item.LibraryID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getting library: %w", err)
	}
	libraryRoot := lib.Path

	// Find the media item's top-level folder(s) — the first directory level
	// below the library root. For a file at /lib/ShowName/Season 01/ep.mkv
	// the top-level folder is /lib/ShowName/. We only scan within these.
	topFolders := make(map[string]struct{})
	for _, f := range existingFiles {
		rel, err := filepath.Rel(libraryRoot, f.Path)
		if err != nil {
			continue
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 {
			topFolders[filepath.Join(libraryRoot, parts[0])] = struct{}{}
		}
	}

	// Re-scan only the top-level media folder(s) and collect video files
	var freshFiles []scannedFile
	for dir := range topFolders {
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			continue
		}
		scanned, err := scanMediaFolder(dir)
		if err != nil {
			// Could not read the folder (transient I/O error). Skip it: the files it
			// contains are left untouched below (the removal step only deletes files
			// that os.Stat confirms are gone via os.IsNotExist), so an unreadable
			// folder never causes a file to be dropped from the item.
			slog.Warn("resync: could not scan media folder; leaving its files untouched",
				"media_item_id", itemID, "path", dir, "error", err)
			continue
		}
		freshFiles = append(freshFiles, scanned...)
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

	// Recalculate media item status after file changes.
	if err := s.RecalcMediaItemStatus(itemID); err != nil {
		slog.Warn("resync: status recalc failed", "media_item_id", itemID, "error", err)
	}

	if s.bus != nil {
		s.bus.Publish(eventbus.ResyncCompleted, eventbus.ResyncPayload{
			MediaItemID: itemID,
			Updated:     updated,
			Added:       added,
			Removed:     removed,
		})
	}

	slog.Info("sync: resync complete", "media_item_id", itemID, "updated", updated, "added", added, "removed", removed)
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

// RecalcMediaItemStatus recalculates and persists the media item's status
// based on the current MediaFile and Episode records in the database.
func (s *Service) RecalcMediaItemStatus(itemID uint) error {
	item, err := s.store.GetMediaItem(itemID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // item was deleted, nothing to recalculate
		}
		return fmt.Errorf("recalc status: get item: %w", err)
	}

	files, err := s.store.ListMediaFilesByMediaItem(itemID)
	if err != nil {
		return fmt.Errorf("recalc status: list files: %w", err)
	}
	hasFiles := len(files) > 0

	var newStatus string

	if item.MediaType == "movie" {
		if hasFiles {
			newStatus = "available"
		} else if item.Source == "request" {
			newStatus = "requested"
		} else {
			newStatus = "missing"
		}
	} else {
		// series
		if !hasFiles {
			if item.Source == "request" {
				newStatus = "requested"
			} else {
				newStatus = "missing"
			}
		} else {
			episodes, err := s.store.ListEpisodesByMediaItem(itemID)
			if err != nil {
				return fmt.Errorf("recalc status: list episodes: %w", err)
			}

			aired := filterAiredEpisodes(episodes)
			if len(aired) == 0 {
				// No aired episodes tracked yet but files exist — treat as available.
				newStatus = "available"
			} else if allAiredEpisodesCovered(aired, files) {
				newStatus = "available"
			} else {
				newStatus = "partial"
			}
		}
	}

	if item.Status == newStatus {
		return nil // no change
	}
	item.Status = newStatus
	return s.store.UpdateMediaItem(item)
}

// filterAiredEpisodes returns only episodes whose AirDate is non-empty and not in the future.
func filterAiredEpisodes(episodes []store.Episode) []store.Episode {
	today := time.Now().Format("2006-01-02")
	aired := make([]store.Episode, 0, len(episodes))
	for _, ep := range episodes {
		if ep.AirDate != "" && ep.AirDate <= today {
			aired = append(aired, ep)
		}
	}
	return aired
}

// allAiredEpisodesCovered checks whether every aired episode has at least one
// matching MediaFile (by SeasonNumber + EpisodeNumber).
func allAiredEpisodesCovered(aired []store.Episode, files []store.MediaFile) bool {
	type seKey struct{ s, e int }
	fileSet := make(map[seKey]struct{}, len(files))
	for _, f := range files {
		if f.SeasonNumber != nil && f.EpisodeNumber != nil {
			fileSet[seKey{*f.SeasonNumber, *f.EpisodeNumber}] = struct{}{}
		}
	}
	for _, ep := range aired {
		if _, ok := fileSet[seKey{ep.SeasonNumber, ep.EpisodeNumber}]; !ok {
			return false
		}
	}
	return true
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
//
// A non-nil error means the folder (or one of its subfolders) could not be read —
// e.g. an unmounted NAS, EACCES or EIO. Callers MUST distinguish this from a
// successful scan that returns zero files (a genuinely empty folder): treating a
// read error as "no files" would make every file under the folder look removed and
// could trigger hard-deletion of the media item.
func scanMediaFolder(folderPath string) ([]scannedFile, error) {
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return nil, err
	}

	files := make([]scannedFile, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			if fileparse.IsSampleFile(e.Name()) {
				continue
			}
			subPath := filepath.Join(folderPath, e.Name())
			// Check if it's a season subfolder
			if sn := fileparse.ParseSeasonFromDir(e.Name()); sn != nil {
				seasonFiles, err := scanSeasonDir(subPath, sn)
				if err != nil {
					return nil, err
				}
				files = append(files, seasonFiles...)
			} else {
				// Non-season subdirectory (e.g. release folder, show-name subfolder).
				// Descend one level to pick up video files inside it.
				subFiles, err := scanVideoDir(subPath, nil, true)
				if err != nil {
					return nil, err
				}
				files = append(files, subFiles...)
			}
			continue
		}

		name := e.Name()
		if !fileparse.IsVideoFile(name) || fileparse.IsSampleFile(name) {
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

	return files, nil
}

// scanSeasonDir scans video files inside a season subfolder.
// If a file doesn't have S##E## in its name, the subfolder's season number is used.
// Also descends into episode wrapper directories (e.g. "Episode 01 - Title/video.mkv").
// A non-nil error means the directory could not be read (see scanMediaFolder).
func scanSeasonDir(dirPath string, seasonNumber *int) ([]scannedFile, error) {
	return scanVideoDir(dirPath, seasonNumber, true)
}

// scanVideoDir scans video files in a directory.
// If recurse is true, it descends into subdirectories (one level).
// A non-nil error means the directory (or a recursed subdirectory) could not be
// read; the caller must not interpret the returned files as the complete contents.
func scanVideoDir(dirPath string, fallbackSeason *int, recurse bool) ([]scannedFile, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	files := make([]scannedFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			if recurse && !fileparse.IsSampleFile(e.Name()) {
				subFiles, err := scanVideoDir(filepath.Join(dirPath, e.Name()), fallbackSeason, false)
				if err != nil {
					return nil, err
				}
				files = append(files, subFiles...)
			}
			continue
		}
		name := e.Name()
		if !fileparse.IsVideoFile(name) || fileparse.IsSampleFile(name) {
			continue
		}

		sf := buildScannedFile(filepath.Join(dirPath, name), name, fallbackSeason)
		files = append(files, sf)
	}

	return files, nil
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
// after the given paths were removed and are therefore safe to hard-delete.
//
// Two classes of item are deliberately NOT reported as orphans:
//  1. Items whose media item could not be loaded (skipped defensively — we never
//     delete something we cannot inspect).
//  2. Request/pending items (see isRequestOrPending). These legitimately have zero
//     disk files because they have not been downloaded yet; deleting them would
//     destroy the user's request together with its metadata and episode records.
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
		if fileCounts[itemID] != 0 {
			continue
		}

		item, err := s.store.GetMediaItem(itemID)
		if err != nil {
			slog.Warn("sync: could not load media item during orphan check; skipping delete",
				"media_item_id", itemID, "error", err)
			continue
		}
		if isRequestOrPending(item) {
			slog.Info("sync: skipping orphan delete for request/pending item",
				"media_item_id", itemID, "source", item.Source, "status", item.Status)
			continue
		}
		orphans = append(orphans, itemID)
	}
	return orphans
}

// isRequestOrPending reports whether a media item legitimately has no disk files
// yet — a user request that has not been downloaded. Such items must never be
// treated as orphans during a library sync, regardless of what is on disk.
func isRequestOrPending(item *store.MediaItem) bool {
	if item == nil {
		return false
	}
	// Requested items keep Source == "request" for their whole lifecycle (even after
	// download), so this guards the request no matter its current status.
	if item.Source == "request" {
		return true
	}
	// "not yet downloaded" statuses: an item in one of these states is expected to
	// have no files on disk.
	switch item.Status {
	case "requested", "pending":
		return true
	}
	return false
}

// topLevelFolder returns the absolute path of the first directory level below
// libraryRoot that contains filePath (e.g. for /lib/Show/Season 01/ep.mkv it
// returns /lib/Show). It returns "" when filePath is not under libraryRoot.
func topLevelFolder(libraryRoot, filePath string) string {
	rel, err := filepath.Rel(libraryRoot, filePath)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) == 0 || parts[0] == "" || parts[0] == ".." {
		return ""
	}
	return filepath.Join(libraryRoot, parts[0])
}

func parseFolderName(name string) (title string, year *int) {
	clean := strings.ReplaceAll(name, ".", " ")

	if m := yearRe.FindStringSubmatch(clean); m != nil {
		if y, err := strconv.Atoi(m[1]); err != nil {
			slog.Warn("unexpected year parse failure", "input", m[1], "error", err)
		} else if y >= 1900 && y <= 2099 {
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

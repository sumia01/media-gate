package sync

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
)

var yearRe = regexp.MustCompile(`[\(\[]?(\d{4})[\)\]]?\s*$`)

// ErrResyncStale means the catalog changed while the filesystem was being
// scanned. The candidate is discarded rather than applied to a newer preimage.
var ErrResyncStale = errors.New("media resync candidate is stale")

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
	folders []string      // top-level folder paths belonging to this group
	files   []scannedFile // all video files across all folders
}

type librarySyncCandidate struct {
	itemID        uint
	newItem       *store.MediaItem
	addFiles      []scannedFile
	removePaths   []string
	deleteIfEmpty bool
}

type resyncPreimage struct {
	item    store.MediaItem
	library store.Library
	files   []store.MediaFile
}

type resyncCandidate struct {
	freshByPath   map[string]scannedFile
	removePaths   map[string]struct{}
	partial       bool
	failedWindows int
}

type resyncResult struct {
	updated int
	added   int
	removed int
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
	pathToItem := make(map[string]uint, len(existingFiles))
	var removePaths []string
	for i := range existingFiles {
		existingPaths[existingFiles[i].Path] = struct{}{}
		pathToItem[existingFiles[i].Path] = existingFiles[i].MediaItemID
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

	candidatesByItem := make(map[uint]*librarySyncCandidate)
	for _, path := range removePaths {
		itemID := pathToItem[path]
		candidate := candidatesByItem[itemID]
		if candidate == nil {
			candidate = &librarySyncCandidate{itemID: itemID}
			candidatesByItem[itemID] = candidate
		}
		candidate.removePaths = append(candidate.removePaths, path)
	}

	// Phase 4: Build folder→MediaItemID map from existing files.
	// A top-level folder maps to a MediaItem if any existing file lives under it.
	folderToItem := make(map[string]uint)
	for i := range existingFiles {
		if topDir := topLevelFolder(lib.Path, existingFiles[i].Path); topDir != "" {
			folderToItem[topDir] = existingFiles[i].MediaItemID
		}
	}

	// Phase 5: Build per-item database candidates from the completed scan.
	var newItemCandidates []*librarySyncCandidate
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
			newItemCandidates = append(newItemCandidates, &librarySyncCandidate{
				newItem: &store.MediaItem{
					LibraryID: lib.ID,
					Title:     g.title,
					MediaType: lib.MediaType,
					Status:    "new",
					Source:    "disk",
					Year:      g.year,
				},
				addFiles: g.files,
			})
			continue
		}

		candidate := candidatesByItem[mediaItemID]
		if candidate == nil {
			candidate = &librarySyncCandidate{itemID: mediaItemID}
			candidatesByItem[mediaItemID] = candidate
		}
		for _, sf := range g.files {
			if _, found := existingPaths[sf.path]; found {
				continue
			}
			candidate.addFiles = append(candidate.addFiles, sf)
		}
	}

	for _, itemID := range s.findOrphanedMediaItems(removePaths, existingFiles, pathToItem) {
		candidate := candidatesByItem[itemID]
		if candidate != nil && len(candidate.addFiles) == 0 {
			candidate.deleteIfEmpty = true
		}
	}

	itemIDs := make([]uint, 0, len(candidatesByItem))
	for itemID, candidate := range candidatesByItem {
		if len(candidate.addFiles) > 0 || len(candidate.removePaths) > 0 {
			itemIDs = append(itemIDs, itemID)
		}
	}
	sort.Slice(itemIDs, func(i, j int) bool { return itemIDs[i] < itemIDs[j] })

	candidates := make([]*librarySyncCandidate, 0, len(itemIDs)+len(newItemCandidates))
	for _, itemID := range itemIDs {
		candidates = append(candidates, candidatesByItem[itemID])
	}
	candidates = append(candidates, newItemCandidates...)
	for _, candidate := range candidates {
		committedItemID, result, deleted, applyErr := s.applyLibrarySyncCandidate(candidate)
		if applyErr != nil {
			return added, removed, applyErr
		}
		added += result.added
		removed += result.removed
		s.finishLibrarySyncCandidate(committedItemID, result, deleted)
	}

	slog.Info("sync: library sync complete", "library_id", lib.ID, "library", lib.Name, "files_added", added, "files_removed", removed)
	return added, removed, nil
}

func (s *Service) applyLibrarySyncCandidate(candidate *librarySyncCandidate) (uint, resyncResult, bool, error) {
	operationID, err := store.NewMediaActivityOperationID()
	if err != nil {
		return 0, resyncResult{}, false, err
	}

	var (
		itemID  uint
		result  resyncResult
		deleted bool
	)
	err = s.store.WithTx(func(tx store.Store) error {
		var item *store.MediaItem
		if candidate.newItem != nil {
			itemCopy := *candidate.newItem
			if err := tx.CreateMediaItem(&itemCopy); err != nil {
				return fmt.Errorf("creating media item %q: %w", itemCopy.Title, err)
			}
			item = &itemCopy
		} else {
			var err error
			item, err = tx.GetMediaItem(candidate.itemID)
			if err != nil {
				return fmt.Errorf("getting media item %d: %w", candidate.itemID, err)
			}
			if item.DeletionPending {
				return store.ErrMediaDeletionPending
			}
		}
		itemID = item.ID

		currentFiles, err := tx.ListMediaFilesByMediaItem(item.ID)
		if err != nil {
			return fmt.Errorf("listing files for media item %d: %w", item.ID, err)
		}
		currentByPath := make(map[string]struct{}, len(currentFiles)+len(candidate.addFiles))
		for _, file := range currentFiles {
			currentByPath[file.Path] = struct{}{}
		}

		for _, scanned := range candidate.addFiles {
			if _, exists := currentByPath[scanned.path]; exists {
				continue
			}
			file := &store.MediaFile{
				MediaItemID: item.ID, Path: scanned.path, FileName: scanned.fileName, Size: scanned.size,
				Resolution: scanned.resolution, SourceType: scanned.sourceType,
				SeasonNumber: scanned.seasonNumber, EpisodeNumber: scanned.episodeNumber,
				AddedAt: time.Now().UTC(),
			}
			if err := tx.CreateMediaFile(file); err != nil {
				return fmt.Errorf("creating media file %q: %w", scanned.fileName, err)
			}
			currentByPath[scanned.path] = struct{}{}
			result.added++
		}

		actualRemovePaths := make([]string, 0, len(candidate.removePaths))
		for _, path := range candidate.removePaths {
			if _, exists := currentByPath[path]; !exists {
				continue
			}
			actualRemovePaths = append(actualRemovePaths, path)
			delete(currentByPath, path)
		}
		if len(actualRemovePaths) > 0 {
			if err := tx.DeleteMediaFilesByPaths(actualRemovePaths); err != nil {
				return fmt.Errorf("removing stale media files for media item %d: %w", item.ID, err)
			}
			result.removed = len(actualRemovePaths)
		}

		if result.added == 0 && result.removed == 0 {
			return nil
		}
		entry, err := store.NewSystemMediaActivity(
			item, "sync", store.MediaActivityActionResyncCompleted, operationID,
			store.MediaActivityDetails{
				Reason: "library_sync", Added: result.added, Updated: result.updated, Removed: result.removed,
			},
		)
		if err != nil {
			return err
		}
		if err := tx.AppendMediaActivity(entry); err != nil {
			return err
		}

		if !candidate.deleteIfEmpty || len(currentByPath) != 0 || isRequestOrPending(item) || item.Monitored {
			return nil
		}
		if err := tx.DeleteMediaMetadataByMediaItem(item.ID); err != nil {
			return fmt.Errorf("deleting orphaned media metadata for item %d: %w", item.ID, err)
		}
		if err := tx.DeleteEpisodesByMediaItem(item.ID); err != nil {
			return fmt.Errorf("deleting orphaned episodes for item %d: %w", item.ID, err)
		}
		if err := tx.DeleteMediaItem(item.ID); err != nil {
			return fmt.Errorf("deleting orphaned media item %d: %w", item.ID, err)
		}
		deleted = true
		return nil
	})
	if err != nil {
		return 0, resyncResult{}, false, err
	}
	return itemID, result, deleted, nil
}

func (s *Service) finishLibrarySyncCandidate(itemID uint, result resyncResult, deleted bool) {
	if result.added == 0 && result.removed == 0 || deleted {
		return
	}
	s.PublishActivityInvalidation(itemID)
	if err := s.RecalcMediaItemStatus(itemID); err != nil {
		slog.Warn("sync: status recalc failed", "media_item_id", itemID, "error", err)
	}
}

// ResyncMediaItem performs the automatic resync used by the importer. It uses
// the same stale-safe scan/apply boundary as an explicit resync, but does not
// create user activity.
func (s *Service) ResyncMediaItem(itemID uint) (updated, added, removed int, err error) {
	preimage, err := captureResyncPreimage(s.store, itemID)
	if err != nil {
		return 0, 0, 0, err
	}
	candidate := scanResyncCandidate(preimage)
	result, err := s.applyResyncCandidate(preimage, candidate, "")
	if err != nil {
		return 0, 0, 0, err
	}
	s.finishResync(itemID, result)
	return result.updated, result.added, result.removed, nil
}

// ResyncMediaItemForUser records an explicit command and its committed result.
// Filesystem inspection occurs after the request transaction commits and before
// the result transaction begins.
func (s *Service) ResyncMediaItemForUser(userID, itemID uint) (updated, added, removed int, err error) {
	var (
		preimage    *resyncPreimage
		operationID string
	)
	if err := s.store.WithTx(func(tx store.Store) error {
		if err := validateResyncActor(tx, userID); err != nil {
			return err
		}
		var err error
		preimage, err = captureResyncPreimage(tx, itemID)
		if err != nil {
			return err
		}
		operationID, err = store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		entry, err := store.NewUserMediaActivity(
			&preimage.item, userID, store.MediaActivityActionResyncRequested, operationID,
			store.MediaActivityVisibilityShared, store.MediaActivityDetails{},
		)
		if err != nil {
			return err
		}
		return tx.AppendMediaActivity(entry)
	}); err != nil {
		return 0, 0, 0, err
	}
	s.PublishActivityInvalidation(itemID)

	candidate := scanResyncCandidate(preimage)
	result, err := s.applyResyncCandidate(preimage, candidate, operationID)
	if err != nil {
		return 0, 0, 0, err
	}
	s.PublishActivityInvalidation(itemID)
	s.finishResync(itemID, result)
	return result.updated, result.added, result.removed, nil
}

func captureResyncPreimage(st store.Store, itemID uint) (*resyncPreimage, error) {
	item, err := st.GetMediaItem(itemID)
	if err != nil {
		return nil, fmt.Errorf("getting media item: %w", err)
	}
	if item.DeletionPending {
		return nil, store.ErrMediaDeletionPending
	}
	library, err := st.GetLibrary(item.LibraryID)
	if err != nil {
		return nil, fmt.Errorf("getting library: %w", err)
	}
	files, err := st.ListMediaFilesByMediaItem(itemID)
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}
	return &resyncPreimage{item: *item, library: *library, files: files}, nil
}

func scanResyncCandidate(preimage *resyncPreimage) *resyncCandidate {
	libraryRoot := preimage.library.Path
	topFolders := make(map[string]struct{})
	for _, file := range preimage.files {
		if folder := topLevelFolder(libraryRoot, file.Path); folder != "" {
			topFolders[folder] = struct{}{}
		}
	}

	candidate := &resyncCandidate{
		freshByPath: make(map[string]scannedFile),
		removePaths: make(map[string]struct{}),
	}
	failedTopFolders := make(map[string]struct{})
	for dir := range topFolders {
		info, err := os.Stat(dir)
		if err != nil {
			candidate.failedWindows++
			failedTopFolders[dir] = struct{}{}
			slog.Warn("resync: could not inspect media folder; leaving its files untouched",
				"media_item_id", preimage.item.ID, "path", dir, "error", err)
			continue
		}
		if !info.IsDir() {
			candidate.failedWindows++
			failedTopFolders[dir] = struct{}{}
			slog.Warn("resync: tracked media folder is not a directory; leaving its files untouched",
				"media_item_id", preimage.item.ID, "path", dir)
			continue
		}
		files, err := scanMediaFolder(dir)
		if err != nil {
			candidate.failedWindows++
			failedTopFolders[dir] = struct{}{}
			slog.Warn("resync: could not scan media folder; leaving its files untouched",
				"media_item_id", preimage.item.ID, "path", dir, "error", err)
			continue
		}
		for _, file := range files {
			if topLevelFolder(libraryRoot, file.path) != "" {
				candidate.freshByPath[file.path] = file
			}
		}
	}

	for _, existing := range preimage.files {
		topFolder := topLevelFolder(libraryRoot, existing.Path)
		if _, found := candidate.freshByPath[existing.Path]; found || topFolder == "" {
			continue
		}
		if _, failed := failedTopFolders[topFolder]; failed {
			continue
		}
		info, err := os.Stat(existing.Path)
		if err == nil {
			parsed := fileparse.Parse(existing.FileName)
			candidate.freshByPath[existing.Path] = scannedFile{
				path: existing.Path, fileName: existing.FileName, size: info.Size(),
				resolution: parsed.Resolution, sourceType: parsed.SourceType,
				seasonNumber: parsed.SeasonNumber, episodeNumber: parsed.EpisodeNumber,
			}
		} else if os.IsNotExist(err) {
			candidate.removePaths[existing.Path] = struct{}{}
		} else {
			candidate.failedWindows++
			slog.Warn("resync: could not inspect tracked media file; leaving it untouched",
				"media_item_id", preimage.item.ID, "path", existing.Path, "error", err)
		}
	}
	candidate.partial = candidate.failedWindows > 0
	return candidate
}

func (s *Service) applyResyncCandidate(preimage *resyncPreimage, candidate *resyncCandidate, operationID string) (resyncResult, error) {
	var result resyncResult
	err := s.store.WithTx(func(tx store.Store) error {
		item, err := tx.GetMediaItem(preimage.item.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrResyncStale
			}
			return err
		}
		if item.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		library, err := tx.GetLibrary(item.LibraryID)
		if err != nil {
			return err
		}
		currentFiles, err := tx.ListMediaFilesByMediaItem(item.ID)
		if err != nil {
			return err
		}
		if !sameResyncIdentity(preimage, item, library) || !sameMediaFilePreimage(preimage.files, currentFiles) {
			return ErrResyncStale
		}

		currentByPath := make(map[string]store.MediaFile, len(currentFiles))
		for _, file := range currentFiles {
			currentByPath[file.Path] = file
		}
		for _, path := range sortedScannedPaths(candidate.freshByPath) {
			fresh := candidate.freshByPath[path]
			current, exists := currentByPath[path]
			if exists {
				if !fileNeedsUpdate(current, fresh) {
					continue
				}
				current.Size = fresh.size
				current.Resolution = fresh.resolution
				current.SourceType = fresh.sourceType
				current.SeasonNumber = fresh.seasonNumber
				current.EpisodeNumber = fresh.episodeNumber
				if err := tx.UpdateMediaFile(&current); err != nil {
					return fmt.Errorf("updating file %q: %w", fresh.fileName, err)
				}
				result.updated++
				continue
			}
			file := &store.MediaFile{
				MediaItemID: item.ID, Path: fresh.path, FileName: fresh.fileName, Size: fresh.size,
				Resolution: fresh.resolution, SourceType: fresh.sourceType,
				SeasonNumber: fresh.seasonNumber, EpisodeNumber: fresh.episodeNumber,
				AddedAt: time.Now().UTC(),
			}
			if err := tx.CreateMediaFile(file); err != nil {
				return fmt.Errorf("creating file %q: %w", fresh.fileName, err)
			}
			result.added++
		}

		removePaths := sortedPathSet(candidate.removePaths)
		if len(removePaths) > 0 {
			if err := tx.DeleteMediaFilesByPaths(removePaths); err != nil {
				return fmt.Errorf("removing files: %w", err)
			}
			result.removed = len(removePaths)
		}

		if operationID == "" {
			return nil
		}
		details := store.MediaActivityDetails{Added: result.added, Updated: result.updated, Removed: result.removed}
		if candidate.partial {
			details.Partial = true
			details.Reason = "partial_scan"
			details.CleanupOutcome = "partial_scan"
			details.Total = candidate.failedWindows
		}
		entry, err := store.NewSystemMediaActivity(
			item, "sync", store.MediaActivityActionResyncCompleted, operationID,
			details,
		)
		if err != nil {
			return err
		}
		return tx.AppendMediaActivity(entry)
	})
	if err != nil {
		return resyncResult{}, err
	}
	return result, nil
}

func (s *Service) finishResync(itemID uint, result resyncResult) {
	if err := s.RecalcMediaItemStatus(itemID); err != nil {
		slog.Warn("resync: status recalc failed", "media_item_id", itemID, "error", err)
	}
	if s.bus != nil {
		s.bus.Publish(eventbus.ResyncCompleted, eventbus.ResyncPayload{
			MediaItemID: itemID, Updated: result.updated, Added: result.added, Removed: result.removed,
		})
	}
	slog.Info("sync: resync complete", "media_item_id", itemID, "updated", result.updated, "added", result.added, "removed", result.removed)
}

func validateResyncActor(st store.Store, userID uint) error {
	if _, err := st.GetUser(userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrActivityActorNotFound
		}
		return err
	}
	return nil
}

func sameResyncIdentity(preimage *resyncPreimage, item *store.MediaItem, library *store.Library) bool {
	return item.ID == preimage.item.ID && item.LibraryID == preimage.item.LibraryID &&
		item.MediaType == preimage.item.MediaType && library.ID == preimage.library.ID &&
		library.Path == preimage.library.Path
}

func sameMediaFilePreimage(before, after []store.MediaFile) bool {
	if len(before) != len(after) {
		return false
	}
	afterByID := make(map[uint]store.MediaFile, len(after))
	for _, file := range after {
		afterByID[file.ID] = file
	}
	for _, file := range before {
		current, ok := afterByID[file.ID]
		if !ok || !sameMediaFileSnapshot(file, current) {
			return false
		}
	}
	return true
}

func sameMediaFileSnapshot(a, b store.MediaFile) bool {
	return a.ID == b.ID && a.MediaItemID == b.MediaItemID && a.Path == b.Path &&
		a.FileName == b.FileName && a.Size == b.Size && a.Resolution == b.Resolution &&
		a.SourceType == b.SourceType && intPtrEqual(a.SeasonNumber, b.SeasonNumber) &&
		intPtrEqual(a.EpisodeNumber, b.EpisodeNumber) && a.AddedAt.Equal(b.AddedAt) &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt)
}

func sortedScannedPaths(files map[string]scannedFile) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedPathSet(files map[string]struct{}) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
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

// RecalcMediaItemStatus recalculates and persists the media item's status from
// the current DB state. It is the single authority for the status state machine:
//
//	unmatched (no MediaMetadata)      → status untouched: "new" marks the
//	                                    auto-match queue (ListNewMediaItemsByLibrary)
//	                                    and belongs to the matching service
//	movie:   has files                → "available"
//	         no files, source=request → "requested"
//	         no files                 → "missing"
//	series:  no files, source=request → "requested"
//	         otherwise, coverage of the WANTED aired episodes decides. Wanted =
//	         monitored aired episodes (EpisodeMonitor > SeasonMonitor > not
//	         monitored — the same hierarchy the monitor worker uses) when the
//	         item is monitored; ALL aired episodes when it is not (informational
//	         disk completeness for unmonitored items).
//	         wanted empty → "available" when files exist or the item is monitored
//	                        (nothing wanted is absent — e.g. only future seasons
//	                        are monitored), "missing" otherwise
//	         all covered  → "available"
//	         some covered → "partial"
//	         none covered → "partial" when any files exist (files whose names
//	                        failed S/E parsing still count as presence), else
//	                        "missing"
func (s *Service) RecalcMediaItemStatus(itemID uint) error {
	item, err := s.store.GetMediaItem(itemID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // item was deleted, nothing to recalculate
		}
		return fmt.Errorf("recalc status: get item: %w", err)
	}
	if item.DeletionPending {
		return store.ErrMediaDeletionPending
	}

	// Unmatched items keep their status untouched: "new" is the auto-match
	// queue marker and overwriting it would silently exclude the item from
	// MatchLibrary forever.
	if _, err := s.store.GetMediaMetadataByMediaItem(itemID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("recalc status: get metadata: %w", err)
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
	} else if !hasFiles && item.Source == "request" {
		// A requested series with nothing imported yet stays "requested"
		// regardless of monitoring configuration.
		newStatus = "requested"
	} else {
		episodes, err := s.store.ListEpisodesByMediaItem(itemID)
		if err != nil {
			return fmt.Errorf("recalc status: list episodes: %w", err)
		}

		wanted := filterAiredEpisodes(episodes)
		if item.Monitored {
			wanted, err = s.filterMonitoredEpisodes(itemID, wanted)
			if err != nil {
				return fmt.Errorf("recalc status: resolve monitors: %w", err)
			}
		}
		covered := countCoveredEpisodes(wanted, files)

		switch {
		case len(wanted) == 0:
			if hasFiles || item.Monitored {
				newStatus = "available"
			} else {
				newStatus = "missing"
			}
		case covered == len(wanted):
			newStatus = "available"
		case covered > 0:
			newStatus = "partial"
		case hasFiles:
			newStatus = "partial"
		default:
			newStatus = "missing"
		}
	}

	if item.Status == newStatus {
		return nil // no change
	}
	item.Status = newStatus
	return s.store.UpdateMediaItem(item)
}

// filterMonitoredEpisodes returns the episodes that resolve to monitored via
// the EpisodeMonitor > SeasonMonitor > not-monitored hierarchy (no season row =
// not monitored), mirroring the monitor worker's wanted-episode resolution.
func (s *Service) filterMonitoredEpisodes(itemID uint, episodes []store.Episode) ([]store.Episode, error) {
	monitors, err := s.store.ListSeasonMonitorsByMediaItem(itemID)
	if err != nil {
		return nil, err
	}
	seasonMon := make(map[int]bool, len(monitors))
	for _, m := range monitors {
		seasonMon[m.SeasonNumber] = m.Monitored
	}

	epMonitors, err := s.store.ListEpisodeMonitorsByMediaItem(itemID)
	if err != nil {
		return nil, err
	}
	type seKey struct{ s, e int }
	epMon := make(map[seKey]bool, len(epMonitors))
	for _, em := range epMonitors {
		epMon[seKey{em.SeasonNumber, em.EpisodeNumber}] = em.Monitored
	}

	monitored := make([]store.Episode, 0, len(episodes))
	for _, ep := range episodes {
		if mon, ok := epMon[seKey{ep.SeasonNumber, ep.EpisodeNumber}]; ok {
			if mon {
				monitored = append(monitored, ep)
			}
			continue
		}
		if seasonMon[ep.SeasonNumber] {
			monitored = append(monitored, ep)
		}
	}
	return monitored, nil
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

// countCoveredEpisodes counts how many of the given episodes have at least one
// matching MediaFile (by SeasonNumber + EpisodeNumber).
func countCoveredEpisodes(episodes []store.Episode, files []store.MediaFile) int {
	type seKey struct{ s, e int }
	fileSet := make(map[seKey]struct{}, len(files))
	for _, f := range files {
		if f.SeasonNumber != nil && f.EpisodeNumber != nil {
			fileSet[seKey{*f.SeasonNumber, *f.EpisodeNumber}] = struct{}{}
		}
	}
	covered := 0
	for _, ep := range episodes {
		if _, ok := fileSet[seKey{ep.SeasonNumber, ep.EpisodeNumber}]; ok {
			covered++
		}
	}
	return covered
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
// Three classes of item are deliberately NOT reported as orphans:
//  1. Items whose media item could not be loaded (skipped defensively — we never
//     delete something we cannot inspect).
//  2. Request/pending items (see isRequestOrPending). These legitimately have zero
//     disk files because they have not been downloaded yet; deleting them would
//     destroy the user's request together with its metadata and episode records.
//  3. Monitored items. The user asked the app to keep tracking them (e.g. only
//     future seasons monitored, old seasons deleted from disk); hard-deleting
//     would destroy the monitors and metadata. Their status is recalculated to
//     reflect the empty disk state instead.
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
		if item.Monitored {
			slog.Info("sync: skipping orphan delete for monitored item",
				"media_item_id", itemID, "title", item.Title)
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

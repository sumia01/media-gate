package apiv1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) GetMediaItem(_ context.Context, req GetMediaItemRequestObject) (GetMediaItemResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}
	meta, _ := h.store.GetMediaMetadataByMediaItem(item.ID)

	return GetMediaItem200JSONResponse(mediaItemToAPI(item, meta)), nil
}

func (h *Handlers) UpdateMediaItem(_ context.Context, req UpdateMediaItemRequestObject) (UpdateMediaItemResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	if req.Body.MediaProfileId != nil {
		profileID := uint(*req.Body.MediaProfileId)
		if _, err := h.store.GetMediaProfile(profileID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return UpdateMediaItem404JSONResponse{
					Code:    http.StatusNotFound,
					Message: "media profile not found",
				}, nil
			}
			return nil, err
		}
		item.MediaProfileID = &profileID
	}

	if req.Body.MonitorNewSeasons != nil {
		item.MonitorNewSeasons = *req.Body.MonitorNewSeasons
	}

	if err := h.store.UpdateMediaItem(item); err != nil {
		return nil, err
	}

	meta, _ := h.store.GetMediaMetadataByMediaItem(item.ID)
	return UpdateMediaItem200JSONResponse(mediaItemToAPI(item, meta)), nil
}

func (h *Handlers) DeleteMediaItem(_ context.Context, req DeleteMediaItemRequestObject) (DeleteMediaItemResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	// Collect file paths before DB cascade deletes the records.
	mediaFiles, _ := h.store.ListMediaFilesByMediaItem(item.ID)

	// Remove torrents from qBittorrent (best-effort).
	itemID := item.ID
	downloads, _ := h.store.ListDownloads(&itemID, nil)
	if client, err := h.getQBitClient(); err == nil {
		for _, dl := range downloads {
			if dl.ClientTorrentHash == "" {
				continue
			}
			if err := client.DeleteTorrent(dl.ClientTorrentHash, true); err != nil {
				slog.Warn("failed to remove torrent from qBittorrent", "hash", dl.ClientTorrentHash, "error", err)
			}
		}
	} else if len(downloads) > 0 {
		slog.Warn("qBittorrent not available, skipping torrent cleanup", "error", err)
	}

	// Determine library root to stop empty-dir cleanup.
	var libraryRoot string
	if lib, err := h.store.GetLibrary(item.LibraryID); err == nil {
		libraryRoot = lib.Path
	}

	// Delete imported library files from disk.
	// Phase 1: Remove tracked video files and collect their parent directories.
	releaseDirs := map[string]bool{}
	for _, mf := range mediaFiles {
		if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove library file", "path", mf.Path, "error", err)
		}
		releaseDirs[filepath.Dir(mf.Path)] = true
	}

	// Phase 2: Clean up release folders that now contain only companion files.
	for dir := range releaseDirs {
		if onlyCompanionsLeft(dir) {
			if err := os.RemoveAll(dir); err != nil {
				slog.Warn("failed to remove release dir", "path", dir, "error", err)
			}
		}
		if libraryRoot != "" {
			removeEmptyParents(filepath.Dir(dir), libraryRoot)
		}
	}

	// Delete poster file.
	posterPath := filepath.Join(h.posterDir, fmt.Sprintf("%d.jpg", item.ID))
	_ = os.Remove(posterPath)

	// Delete DB record (CASCADE removes MediaFile, Download, Episode, etc.).
	if err := h.store.DeleteMediaItem(item.ID); err != nil {
		return nil, err
	}

	h.bus.Publish(eventbus.MediaItemDeleted, eventbus.MediaItemPayload{
		MediaItemID: item.ID, LibraryID: item.LibraryID, Title: item.Title,
	})

	return DeleteMediaItem204Response{}, nil
}

// removeEmptyParents removes empty directories from dir up to (but not including) stopAt.
func removeEmptyParents(dir, stopAt string) {
	for dir != stopAt && strings.HasPrefix(dir, stopAt) {
		if err := os.Remove(dir); err != nil {
			break // not empty or permission error
		}
		dir = filepath.Dir(dir)
	}
}

// onlyCompanionsLeft returns true if a directory contains no video files —
// only companion files (subtitles, NFO, images) or is empty/already removed.
// It recurses into subdirectories.
func onlyCompanionsLeft(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if !onlyCompanionsLeft(filepath.Join(dir, e.Name())) {
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

func (h *Handlers) SearchMediaCandidates(_ context.Context, req SearchMediaCandidatesRequestObject) (SearchMediaCandidatesResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return SearchMediaCandidates404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	query := item.Title
	if req.Params.Query != nil && *req.Params.Query != "" {
		query = *req.Params.Query
	}

	var source string
	if req.Params.Source != nil {
		source = string(*req.Params.Source)
	}

	candidates, err := h.matchSvc.SearchCandidates(query, item.MediaType, item.Year, source)
	if err != nil {
		return nil, err
	}

	return SearchMediaCandidates200JSONResponse{Candidates: candidatesToAPI(candidates)}, nil
}

func (h *Handlers) ManualMatch(_ context.Context, req ManualMatchRequestObject) (ManualMatchResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ManualMatch404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	if err := h.matchSvc.ManualMatch(item.ID, string(req.Body.Source), req.Body.ExternalId); err != nil {
		return nil, err
	}

	// Recalculate status based on current files (e.g. partial if some episodes already downloaded).
	if err := h.syncSvc.RecalcMediaItemStatus(item.ID); err != nil {
		slog.Warn("manual match: status recalc failed", "media_item_id", item.ID, "error", err)
	}

	// Re-fetch the updated item
	item, err = h.store.GetMediaItem(item.ID)
	if err != nil {
		return nil, err
	}
	meta, _ := h.store.GetMediaMetadataByMediaItem(item.ID)

	return ManualMatch200JSONResponse(mediaItemToAPI(item, meta)), nil
}

func (h *Handlers) UnmatchMedia(_ context.Context, req UnmatchMediaRequestObject) (UnmatchMediaResponseObject, error) {
	item, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UnmatchMedia404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	if err := h.matchSvc.Unmatch(item.ID); err != nil {
		return nil, err
	}

	item, err = h.store.GetMediaItem(item.ID)
	if err != nil {
		return nil, err
	}

	return UnmatchMedia200JSONResponse(mediaItemToAPI(item, nil)), nil
}

func (h *Handlers) ResyncMediaItem(_ context.Context, req ResyncMediaItemRequestObject) (ResyncMediaItemResponseObject, error) {
	_, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ResyncMediaItem404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	updated, added, removed, err := h.syncSvc.ResyncMediaItem(uint(req.Id))
	if err != nil {
		return nil, err
	}

	return ResyncMediaItem200JSONResponse{
		Updated: updated,
		Added:   added,
		Removed: removed,
	}, nil
}

func (h *Handlers) ListMediaFiles(_ context.Context, req ListMediaFilesRequestObject) (ListMediaFilesResponseObject, error) {
	_, err := h.store.GetMediaItem(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListMediaFiles404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	files, err := h.store.ListMediaFilesByMediaItem(uint(req.Id))
	if err != nil {
		return nil, err
	}

	apiFiles := make([]MediaFile, len(files))
	for i := range files {
		apiFiles[i] = mediaFileToAPI(&files[i])
	}

	// Sort by season number, then episode number, then filename
	sort.Slice(apiFiles, func(i, j int) bool {
		si := derefInt(apiFiles[i].SeasonNumber)
		sj := derefInt(apiFiles[j].SeasonNumber)
		if si != sj {
			return si < sj
		}
		ei := derefInt(apiFiles[i].EpisodeNumber)
		ej := derefInt(apiFiles[j].EpisodeNumber)
		if ei != ej {
			return ei < ej
		}
		return apiFiles[i].FileName < apiFiles[j].FileName
	})

	return ListMediaFiles200JSONResponse{Files: apiFiles}, nil
}

func (h *Handlers) ListMediaEpisodes(_ context.Context, req ListMediaEpisodesRequestObject) (ListMediaEpisodesResponseObject, error) {
	itemID := uint(req.Id)
	_, err := h.store.GetMediaItem(itemID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListMediaEpisodes404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media item not found",
			}, nil
		}
		return nil, err
	}

	episodes, err := h.store.ListEpisodesByMediaItem(itemID)
	if err != nil {
		return nil, err
	}

	files, err := h.store.ListMediaFilesByMediaItem(itemID)
	if err != nil {
		return nil, err
	}

	monitors, err := h.store.ListSeasonMonitorsByMediaItem(itemID)
	if err != nil {
		return nil, err
	}

	downloads, err := h.store.ListDownloads(&itemID, nil)
	if err != nil {
		return nil, err
	}

	// Build file presence lookup: "S{season}E{episode}" → true
	fileLookup := make(map[string]bool)
	for _, f := range files {
		if f.SeasonNumber != nil && f.EpisodeNumber != nil {
			key := fmt.Sprintf("S%dE%d", *f.SeasonNumber, *f.EpisodeNumber)
			fileLookup[key] = true
		}
	}

	// Build monitor lookup: seasonNumber → monitored
	monitorLookup := make(map[int]bool)
	for _, m := range monitors {
		monitorLookup[m.SeasonNumber] = m.Monitored
	}

	// Build download status lookups.
	// Priority: downloading > pending > downloaded > importing > seeding (completed/failed ignored).
	dlStatusPriority := map[string]int{
		"downloading": 5,
		"pending":     4,
		"downloaded":  3,
		"importing":   2,
		"seeding":     1,
	}
	// episodeDownloadStatus: episodeDBID → best download status
	episodeDownloadStatus := make(map[uint]string)
	// seasonDownloadStatus: seasonNumber → best download status (for season-level downloads)
	seasonDownloadStatus := make(map[int]string)
	// itemDownloadStatus: best download status for item-level downloads
	var itemDownloadStatus string
	for _, dl := range downloads {
		pri := dlStatusPriority[dl.Status]
		if pri == 0 {
			continue // skip completed/failed
		}
		if dl.EpisodeID != nil {
			if cur, ok := episodeDownloadStatus[*dl.EpisodeID]; !ok || pri > dlStatusPriority[cur] {
				episodeDownloadStatus[*dl.EpisodeID] = dl.Status
			}
		} else if dl.SeasonNumber != nil {
			sn := *dl.SeasonNumber
			if cur, ok := seasonDownloadStatus[sn]; !ok || pri > dlStatusPriority[cur] {
				seasonDownloadStatus[sn] = dl.Status
			}
		} else {
			if dlStatusPriority[itemDownloadStatus] < pri {
				itemDownloadStatus = dl.Status
			}
		}
	}

	// Group episodes by season
	seasonMap := make(map[int][]Episode)
	for _, ep := range episodes {
		key := fmt.Sprintf("S%dE%d", ep.SeasonNumber, ep.EpisodeNumber)
		hasFile := fileLookup[key]
		apiEp := Episode{
			Id:            int64(ep.ID),
			MediaItemId:   int64(ep.MediaItemID),
			SeasonNumber:  ep.SeasonNumber,
			EpisodeNumber: ep.EpisodeNumber,
			HasFile:       &hasFile,
		}
		if ep.Title != "" {
			apiEp.Title = &ep.Title
		}
		if ep.Overview != "" {
			apiEp.Overview = &ep.Overview
		}
		if ep.AirDate != "" {
			apiEp.AirDate = &ep.AirDate
		}
		if ep.Runtime != nil {
			apiEp.Runtime = ep.Runtime
		}

		// Resolve download status: episode-level > season-level > item-level
		if status, ok := episodeDownloadStatus[ep.ID]; ok {
			ds := EpisodeDownloadStatus(status)
			apiEp.DownloadStatus = &ds
		} else if status, ok := seasonDownloadStatus[ep.SeasonNumber]; ok {
			ds := EpisodeDownloadStatus(status)
			apiEp.DownloadStatus = &ds
		} else if itemDownloadStatus != "" {
			ds := EpisodeDownloadStatus(itemDownloadStatus)
			apiEp.DownloadStatus = &ds
		}

		seasonMap[ep.SeasonNumber] = append(seasonMap[ep.SeasonNumber], apiEp)
	}

	// Build sorted season summaries
	var seasons []SeasonSummary
	for sn, eps := range seasonMap {
		available := 0
		for _, ep := range eps {
			if ep.HasFile != nil && *ep.HasFile {
				available++
			}
		}
		monitored, ok := monitorLookup[sn]
		if !ok {
			monitored = true // default to monitored
		}
		seasons = append(seasons, SeasonSummary{
			SeasonNumber:      sn,
			TotalEpisodes:     len(eps),
			AvailableEpisodes: available,
			Monitored:         monitored,
			Episodes:          &eps,
		})
	}

	// Sort by season number
	sort.Slice(seasons, func(i, j int) bool {
		return seasons[i].SeasonNumber < seasons[j].SeasonNumber
	})

	return ListMediaEpisodes200JSONResponse{Seasons: seasons}, nil
}

package apiv1

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/importer"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) CreateDownload(_ context.Context, req CreateDownloadRequestObject) (CreateDownloadResponseObject, error) {
	dl := &store.Download{
		MediaItemID: uint(req.Body.MediaItemId),
		IndexerID:   uint(req.Body.IndexerId),
		IndexerName: req.Body.IndexerName,
		Title:       req.Body.Title,
		DownloadURL: req.Body.DownloadUrl,
		Status:      "pending",
	}
	if req.Body.EpisodeId != nil {
		eid := uint(*req.Body.EpisodeId)
		dl.EpisodeID = &eid
	}
	if req.Body.SeasonNumber != nil {
		dl.SeasonNumber = req.Body.SeasonNumber
	}
	if req.Body.DetailsUrl != nil {
		dl.DetailsURL = *req.Body.DetailsUrl
	}
	if req.Body.Size != nil {
		dl.Size = *req.Body.Size
	}
	if req.Body.ImdbId != nil {
		dl.ImdbID = *req.Body.ImdbId
	}

	if err := h.store.CreateDownload(dl); err != nil {
		return CreateDownload400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	h.bus.Publish(eventbus.DownloadCreated, eventbus.DownloadPayload{
		DownloadID: dl.ID, MediaItemID: dl.MediaItemID, Title: dl.Title, Status: "pending",
	})

	return CreateDownload201JSONResponse(downloadToAPI(dl)), nil
}

func (h *Handlers) ListDownloads(_ context.Context, req ListDownloadsRequestObject) (ListDownloadsResponseObject, error) {
	var mediaItemID *uint
	if req.Params.MediaItemId != nil {
		id := uint(*req.Params.MediaItemId)
		mediaItemID = &id
	}

	var status *string
	if req.Params.Status != nil {
		s := string(*req.Params.Status)
		status = &s
	}

	downloads, err := h.store.ListDownloads(mediaItemID, status)
	if err != nil {
		return nil, err
	}

	apiDownloads := make([]Download, len(downloads))
	for i := range downloads {
		apiDownloads[i] = downloadToAPI(&downloads[i])
	}

	// Enrich with real-time progress data when filtering by media item
	if mediaItemID != nil {
		if client, err := h.getQBitClient(); err == nil {
			for i := range apiDownloads {
				hash := apiDownloads[i].ClientTorrentHash
				if hash == nil || *hash == "" {
					continue
				}
				info, err := client.GetTorrent(*hash)
				if err != nil {
					continue
				}
				p := float32(info.Progress)
				apiDownloads[i].Progress = &p
				apiDownloads[i].DownloadSpeed = &info.DownloadSpeed
				apiDownloads[i].UploadSpeed = &info.UploadSpeed
			}
		}
	}

	return ListDownloads200JSONResponse{Downloads: apiDownloads}, nil
}

func (h *Handlers) GetDownload(_ context.Context, req GetDownloadRequestObject) (GetDownloadResponseObject, error) {
	dl, err := h.store.GetDownload(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetDownload404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "download not found",
			}, nil
		}
		return nil, err
	}
	return GetDownload200JSONResponse(downloadToAPI(dl)), nil
}

func (h *Handlers) UpdateDownloadStatus(_ context.Context, req UpdateDownloadStatusRequestObject) (UpdateDownloadStatusResponseObject, error) {
	dl, err := h.store.GetDownload(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateDownloadStatus404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "download not found",
			}, nil
		}
		return nil, err
	}

	dl.Status = string(req.Body.Status)
	if err := h.store.UpdateDownload(dl); err != nil {
		return nil, err
	}

	return UpdateDownloadStatus200JSONResponse(downloadToAPI(dl)), nil
}

func (h *Handlers) DeleteDownload(_ context.Context, req DeleteDownloadRequestObject) (DeleteDownloadResponseObject, error) {
	dl, err := h.store.GetDownload(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteDownload404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "download not found",
			}, nil
		}
		return nil, err
	}

	deleteFiles := req.Params.DeleteFiles != nil && *req.Params.DeleteFiles

	if dl.ClientTorrentHash != "" {
		if client, err := h.getQBitClient(); err == nil {
			if err := client.DeleteTorrent(dl.ClientTorrentHash, deleteFiles); err != nil {
				slog.Warn("failed to remove torrent from qBittorrent", "hash", dl.ClientTorrentHash, "error", err)
			}
		}
	}

	// Remove imported library files if the download was linked and deleteFiles requested.
	if deleteFiles && dl.LinkedToLibrary {
		h.cleanupImportedFiles(dl)
		// Recalculate media item status after file removal.
		if err := h.syncSvc.RecalcMediaItemStatus(dl.MediaItemID); err != nil {
			slog.Warn("delete download: status recalc failed", "media_item_id", dl.MediaItemID, "error", err)
		}
	}

	if err := h.store.DeleteDownload(dl.ID); err != nil {
		return nil, err
	}

	return DeleteDownload204Response{}, nil
}

// cleanupImportedFiles removes library files that were imported from a specific download.
// It reconstructs the release folder path and removes matching MediaFile records + disk files.
func (h *Handlers) cleanupImportedFiles(dl *store.Download) {
	item, err := h.store.GetMediaItem(dl.MediaItemID)
	if err != nil {
		slog.Warn("cleanup: media item not found, skipping library file cleanup", "download_id", dl.ID, "error", err)
		return
	}
	lib, err := h.store.GetLibrary(item.LibraryID)
	if err != nil {
		slog.Warn("cleanup: library not found, skipping library file cleanup", "download_id", dl.ID, "error", err)
		return
	}

	meta, _ := h.store.GetMediaMetadataByMediaItem(dl.MediaItemID)
	targetDir := importer.BuildTargetDir(lib, item, meta, dl.SeasonNumber)
	releaseDir := filepath.Join(targetDir, importer.BuildReleaseFolderName(dl.Title))

	// Find MediaFiles belonging to this release folder.
	allFiles, _ := h.store.ListMediaFilesByMediaItem(item.ID)
	var matchedPaths []string
	prefix := releaseDir + string(filepath.Separator)
	for _, mf := range allFiles {
		if strings.HasPrefix(mf.Path, prefix) {
			if err := os.Remove(mf.Path); err != nil && !os.IsNotExist(err) {
				slog.Warn("cleanup: failed to remove library file", "path", mf.Path, "error", err)
			}
			matchedPaths = append(matchedPaths, mf.Path)
		}
	}

	// Remove MediaFile DB records.
	if len(matchedPaths) > 0 {
		if err := h.store.DeleteMediaFilesByPaths(matchedPaths); err != nil {
			slog.Warn("cleanup: failed to delete media file records", "download_id", dl.ID, "error", err)
		}
	}

	// Remove the release folder if only companion files remain.
	if onlyCompanionsLeft(releaseDir) {
		if err := os.RemoveAll(releaseDir); err != nil {
			slog.Warn("cleanup: failed to remove release dir", "path", releaseDir, "error", err)
		}
	}

	// Clean up empty parent directories up to library root.
	removeEmptyParents(filepath.Dir(releaseDir), lib.Path)
}

func (h *Handlers) ListDownloadFiles(_ context.Context, req ListDownloadFilesRequestObject) (ListDownloadFilesResponseObject, error) {
	dl, err := h.store.GetDownload(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListDownloadFiles404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "download not found",
			}, nil
		}
		return nil, err
	}

	if dl.ClientTorrentHash == "" {
		return ListDownloadFiles200JSONResponse{Files: []TorrentFile{}}, nil
	}

	client, err := h.getQBitClient()
	if err != nil {
		return ListDownloadFiles200JSONResponse{Files: []TorrentFile{}}, nil
	}

	qFiles, err := client.GetTorrentFiles(dl.ClientTorrentHash)
	if err != nil {
		slog.Warn("failed to get torrent files from qBittorrent", "hash", dl.ClientTorrentHash, "error", err)
		return ListDownloadFiles200JSONResponse{Files: []TorrentFile{}}, nil
	}

	apiFiles := make([]TorrentFile, len(qFiles))
	for i, f := range qFiles {
		apiFiles[i] = TorrentFile{
			Name:     f.Name,
			Size:     f.Size,
			Progress: float32(f.Progress),
		}
	}

	return ListDownloadFiles200JSONResponse{Files: apiFiles}, nil
}

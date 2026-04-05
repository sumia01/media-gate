package apiv1

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

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

	if err := h.downloadSvc.Create(dl); err != nil {
		return CreateDownload400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

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

	downloads, err := h.downloadSvc.ListWithProgress(mediaItemID, status)
	if err != nil {
		return nil, err
	}

	apiDownloads := make([]Download, len(downloads))
	for i := range downloads {
		apiDownloads[i] = downloadToAPI(&downloads[i].Download)
		if downloads[i].Progress != nil {
			apiDownloads[i].Progress = downloads[i].Progress
		}
		if downloads[i].DownloadSpeed != nil {
			apiDownloads[i].DownloadSpeed = downloads[i].DownloadSpeed
		}
		if downloads[i].UploadSpeed != nil {
			apiDownloads[i].UploadSpeed = downloads[i].UploadSpeed
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
	dl, err := h.downloadSvc.UpdateStatus(uint(req.Id), string(req.Body.Status))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateDownloadStatus404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "download not found",
			}, nil
		}
		return nil, err
	}
	return UpdateDownloadStatus200JSONResponse(downloadToAPI(dl)), nil
}

func (h *Handlers) DeleteDownload(_ context.Context, req DeleteDownloadRequestObject) (DeleteDownloadResponseObject, error) {
	deleteFiles := req.Params.DeleteFiles != nil && *req.Params.DeleteFiles
	if err := h.mediaSvc.DeleteDownload(uint(req.Id), deleteFiles); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteDownload404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "download not found",
			}, nil
		}
		return nil, err
	}
	return DeleteDownload204Response{}, nil
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

	qFiles, err := h.downloadSvc.ListTorrentFiles(dl.ClientTorrentHash)
	if err != nil {
		slog.Warn("failed to get torrent files from qBittorrent", "hash", dl.ClientTorrentHash, "error", err)
		return ListDownloadFiles200JSONResponse{Files: []TorrentFile{}}, nil
	}
	if qFiles == nil {
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

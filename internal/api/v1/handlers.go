package apiv1

import (
	"context"
	"errors"
	"net/http"

	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

// Ensure Handlers implements the generated StrictServerInterface.
var _ StrictServerInterface = (*Handlers)(nil)

type Handlers struct {
	lib      *library.Service
	store    store.Store
	queue    *jobqueue.Queue
	settings *settings.Service
}

func NewHandlers(lib *library.Service, s store.Store, q *jobqueue.Queue, set *settings.Service) *Handlers {
	return &Handlers{lib: lib, store: s, queue: q, settings: set}
}

func (h *Handlers) GetHealth(_ context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: "ok"}, nil
}

func (h *Handlers) CreateLibrary(_ context.Context, req CreateLibraryRequestObject) (CreateLibraryResponseObject, error) {
	lib := &store.Library{
		Name:      req.Body.Name,
		Path:      req.Body.Path,
		MediaType: string(req.Body.MediaType),
	}

	if err := h.lib.Create(lib); err != nil {
		return CreateLibrary400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateLibrary201JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) ListLibraries(_ context.Context, _ ListLibrariesRequestObject) (ListLibrariesResponseObject, error) {
	libs, err := h.lib.List()
	if err != nil {
		return nil, err
	}

	result := make(ListLibraries200JSONResponse, len(libs))
	for i := range libs {
		result[i] = libraryToAPI(&libs[i])
	}
	return result, nil
}

func (h *Handlers) GetLibrary(_ context.Context, req GetLibraryRequestObject) (GetLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	return GetLibrary200JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) UpdateLibrary(_ context.Context, req UpdateLibraryRequestObject) (UpdateLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	lib.Name = req.Body.Name
	lib.Path = req.Body.Path
	lib.MediaType = string(req.Body.MediaType)

	if err := h.lib.Update(lib); err != nil {
		return nil, err
	}

	return UpdateLibrary200JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) DeleteLibrary(_ context.Context, req DeleteLibraryRequestObject) (DeleteLibraryResponseObject, error) {
	id := uint(req.Id)

	if err := h.store.DeleteMediaItemsByLibrary(id); err != nil {
		return nil, err
	}

	if err := h.lib.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	return DeleteLibrary204Response{}, nil
}

func (h *Handlers) ScanLibrary(_ context.Context, req ScanLibraryRequestObject) (ScanLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ScanLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	entries, err := h.lib.Scan(lib)
	if err != nil {
		return ScanLibrary400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	apiEntries := make([]ScanEntry, len(entries))
	for i, e := range entries {
		apiEntries[i] = ScanEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDirectory,
			Size:        e.Size,
			ModifiedAt:  e.ModifiedAt,
		}
	}

	return ScanLibrary200JSONResponse{Entries: apiEntries}, nil
}

func (h *Handlers) BrowseFolder(_ context.Context, req BrowseFolderRequestObject) (BrowseFolderResponseObject, error) {
	var path string
	if req.Params.Path != nil {
		path = *req.Params.Path
	}

	browsedPath, entries, err := h.lib.Browse(path)
	if err != nil {
		return BrowseFolder400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	apiEntries := make([]ScanEntry, len(entries))
	for i, e := range entries {
		apiEntries[i] = ScanEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDirectory,
			Size:        e.Size,
			ModifiedAt:  e.ModifiedAt,
		}
	}

	return BrowseFolder200JSONResponse{
		Path:    browsedPath,
		Entries: apiEntries,
	}, nil
}

func (h *Handlers) ListMediaItems(_ context.Context, req ListMediaItemsRequestObject) (ListMediaItemsResponseObject, error) {
	_, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListMediaItems404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	items, err := h.store.ListMediaItemsByLibrary(uint(req.Id))
	if err != nil {
		return nil, err
	}

	apiItems := make([]MediaItem, len(items))
	for i, item := range items {
		apiItems[i] = mediaItemToAPI(&item)
	}

	return ListMediaItems200JSONResponse{
		Items: apiItems,
		Total: len(apiItems),
	}, nil
}

func (h *Handlers) TriggerSync(_ context.Context, req TriggerSyncRequestObject) (TriggerSyncResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TriggerSync404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	job, err := h.queue.Enqueue(jobqueue.JobTypeSyncLibrary, lib.ID, lib.Name)
	if err != nil {
		return TriggerSync409JSONResponse{
			Code:    http.StatusConflict,
			Message: err.Error(),
		}, nil
	}

	return TriggerSync202JSONResponse(jobToAPI(job)), nil
}

func (h *Handlers) ListJobs(_ context.Context, _ ListJobsRequestObject) (ListJobsResponseObject, error) {
	jobs := h.queue.ListJobs()
	apiJobs := make([]Job, len(jobs))
	for i, j := range jobs {
		apiJobs[i] = jobToAPI(j)
	}
	return ListJobs200JSONResponse{Jobs: apiJobs}, nil
}

func (h *Handlers) ListSettings(_ context.Context, _ ListSettingsRequestObject) (ListSettingsResponseObject, error) {
	items, err := h.settings.List()
	if err != nil {
		return nil, err
	}
	apiSettings := make([]Setting, len(items))
	for i, s := range items {
		apiSettings[i] = settingToAPI(&s)
	}
	return ListSettings200JSONResponse{Settings: apiSettings}, nil
}

func (h *Handlers) UpdateSettings(_ context.Context, req UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	kvs := make([]settings.KeyValue, len(req.Body.Settings))
	for i, s := range req.Body.Settings {
		kvs[i] = settings.KeyValue{Key: s.Key, Value: s.Value}
	}
	if err := h.settings.Update(kvs); err != nil {
		return nil, err
	}

	items, err := h.settings.List()
	if err != nil {
		return nil, err
	}
	apiSettings := make([]Setting, len(items))
	for i, s := range items {
		apiSettings[i] = settingToAPI(&s)
	}
	return UpdateSettings200JSONResponse{Settings: apiSettings}, nil
}

func (h *Handlers) TestTmdbConnection(_ context.Context, req TestTmdbConnectionRequestObject) (TestTmdbConnectionResponseObject, error) {
	success, msg, err := h.settings.TestTMDB(req.Body.ApiKey)
	if err != nil {
		return nil, err
	}
	return TestTmdbConnection200JSONResponse{Success: success, Message: &msg}, nil
}

func (h *Handlers) TestTvdbConnection(_ context.Context, req TestTvdbConnectionRequestObject) (TestTvdbConnectionResponseObject, error) {
	success, msg, err := h.settings.TestTVDB(req.Body.ApiKey)
	if err != nil {
		return nil, err
	}
	return TestTvdbConnection200JSONResponse{Success: success, Message: &msg}, nil
}

func libraryToAPI(lib *store.Library) Library {
	return Library{
		Id:        int64(lib.ID),
		Name:      lib.Name,
		Path:      lib.Path,
		MediaType: LibraryMediaType(lib.MediaType),
		CreatedAt: lib.CreatedAt,
		UpdatedAt: lib.UpdatedAt,
	}
}

func mediaItemToAPI(item *store.MediaItem) MediaItem {
	return MediaItem{
		Id:         int64(item.ID),
		LibraryId:  int64(item.LibraryID),
		Title:      item.Title,
		FolderName: item.FolderName,
		Path:       item.Path,
		MediaType:  MediaItemMediaType(item.MediaType),
		Status:     MediaItemStatus(item.Status),
		Year:       item.Year,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
}

func jobToAPI(j *jobqueue.Job) Job {
	apiJob := Job{
		Id:          j.ID,
		Type:        JobType(j.Type),
		Status:      JobStatus(j.Status),
		CreatedAt:   j.CreatedAt,
		StartedAt:   j.StartedAt,
		CompletedAt: j.CompletedAt,
	}
	if j.LibraryID != 0 {
		libID := int64(j.LibraryID)
		apiJob.LibraryId = &libID
	}
	if j.LibraryName != "" {
		apiJob.LibraryName = &j.LibraryName
	}
	if j.Error != "" {
		apiJob.Error = &j.Error
	}
	if j.Progress != nil {
		apiJob.Progress = &struct {
			Current *int    `json:"current,omitempty"`
			Message *string `json:"message,omitempty"`
			Total   *int    `json:"total,omitempty"`
		}{
			Current: &j.Progress.Current,
			Message: &j.Progress.Message,
			Total:   &j.Progress.Total,
		}
	}
	return apiJob
}

func settingToAPI(s *store.Setting) Setting {
	return Setting{
		Key:       s.Key,
		Value:     s.Value,
		Sensitive: &s.Sensitive,
	}
}

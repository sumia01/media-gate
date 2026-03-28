package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

// Ensure Handlers implements the generated StrictServerInterface.
var _ StrictServerInterface = (*Handlers)(nil)

type Handlers struct {
	lib       *library.Service
	store     store.Store
	queue     *jobqueue.Queue
	settings  *settings.Service
	matchSvc  *matching.Service
	posterDir string
}

func NewHandlers(lib *library.Service, s store.Store, q *jobqueue.Queue, set *settings.Service, matchSvc *matching.Service, posterDir string) *Handlers {
	return &Handlers{lib: lib, store: s, queue: q, settings: set, matchSvc: matchSvc, posterDir: posterDir}
}

func (h *Handlers) PosterHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		posterPath := filepath.Join(h.posterDir, fmt.Sprintf("%d.jpg", id))
		info, err := os.Stat(posterPath)
		if err != nil {
			http.Error(w, "poster not found", http.StatusNotFound)
			return
		}

		f, err := os.Open(posterPath)
		if err != nil {
			http.Error(w, "poster not found", http.StatusNotFound)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeContent(w, r, posterPath, info.ModTime(), f)
	}
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

	// Delete media files for all items in this library
	items, err := h.store.ListMediaItemsByLibrary(id)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		_ = h.store.DeleteMediaFilesByMediaItem(item.ID)
	}

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

	// Batch fetch metadata for all items
	ids := make([]uint, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	metas, err := h.store.ListMediaMetadataByMediaItemIDs(ids)
	if err != nil {
		return nil, err
	}
	metaMap := make(map[uint]*store.MediaMetadata, len(metas))
	for i := range metas {
		metaMap[metas[i].MediaItemID] = &metas[i]
	}

	apiItems := make([]MediaItem, len(items))
	for i, item := range items {
		apiItems[i] = mediaItemToAPI(&item, metaMap[item.ID])
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

func (h *Handlers) TriggerMatch(_ context.Context, req TriggerMatchRequestObject) (TriggerMatchResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TriggerMatch404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	job, err := h.queue.Enqueue(jobqueue.JobTypeMatchLibrary, lib.ID, lib.Name)
	if err != nil {
		return TriggerMatch409JSONResponse{
			Code:    http.StatusConflict,
			Message: err.Error(),
		}, nil
	}

	return TriggerMatch202JSONResponse(jobToAPI(job)), nil
}

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

	if item.Source != "request" {
		return DeleteMediaItem409JSONResponse{
			Code:    http.StatusConflict,
			Message: "only requested media items can be deleted",
		}, nil
	}

	// Delete media files, metadata, and poster
	_ = h.store.DeleteMediaFilesByMediaItem(item.ID)
	_ = h.store.DeleteMediaMetadataByMediaItem(item.ID)

	// Delete poster file
	posterPath := filepath.Join(h.posterDir, fmt.Sprintf("%d.jpg", item.ID))
	_ = os.Remove(posterPath)

	if err := h.store.DeleteMediaItem(item.ID); err != nil {
		return nil, err
	}

	return DeleteMediaItem204Response{}, nil
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

	apiCandidates := make([]MatchCandidate, len(candidates))
	for i, c := range candidates {
		apiCandidates[i] = candidateToAPI(c)
	}

	return SearchMediaCandidates200JSONResponse{Candidates: apiCandidates}, nil
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

func (h *Handlers) SearchMediaForLibrary(_ context.Context, req SearchMediaForLibraryRequestObject) (SearchMediaForLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return SearchMediaForLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	candidates, err := h.matchSvc.SearchForLibrary(lib, req.Params.Query)
	if err != nil {
		return nil, err
	}

	apiCandidates := make([]MatchCandidate, len(candidates))
	for i, c := range candidates {
		apiCandidates[i] = candidateToAPI(c)
	}

	return SearchMediaForLibrary200JSONResponse{Candidates: apiCandidates}, nil
}

func (h *Handlers) AddMediaToLibrary(_ context.Context, req AddMediaToLibraryRequestObject) (AddMediaToLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return AddMediaToLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	item, err := h.matchSvc.AddMediaToLibrary(lib, string(req.Body.Source), req.Body.ExternalId)
	if err != nil {
		if errors.Is(err, matching.ErrAlreadyExists) {
			return AddMediaToLibrary409JSONResponse{
				Code:    http.StatusConflict,
				Message: err.Error(),
			}, nil
		}
		return nil, err
	}

	meta, _ := h.store.GetMediaMetadataByMediaItem(item.ID)
	return AddMediaToLibrary201JSONResponse(mediaItemToAPI(item, meta)), nil
}

// --- Quality Profile handlers ---

func (h *Handlers) ListQualityProfiles(_ context.Context, _ ListQualityProfilesRequestObject) (ListQualityProfilesResponseObject, error) {
	profiles, err := h.store.ListQualityProfiles()
	if err != nil {
		return nil, err
	}

	apiProfiles := make([]QualityProfile, len(profiles))
	for i := range profiles {
		apiProfiles[i] = qualityProfileToAPI(&profiles[i])
	}

	return ListQualityProfiles200JSONResponse{Profiles: apiProfiles}, nil
}

func (h *Handlers) CreateQualityProfile(_ context.Context, req CreateQualityProfileRequestObject) (CreateQualityProfileResponseObject, error) {
	resJSON, _ := json.Marshal(req.Body.Resolutions)
	profile := &store.QualityProfile{
		Name:        req.Body.Name,
		Resolutions: string(resJSON),
	}
	if req.Body.Sources != nil {
		srcJSON, _ := json.Marshal(*req.Body.Sources)
		profile.Sources = string(srcJSON)
	}
	if req.Body.ExcludeTags != nil {
		tagJSON, _ := json.Marshal(*req.Body.ExcludeTags)
		profile.ExcludeTags = string(tagJSON)
	}

	if err := h.store.CreateQualityProfile(profile); err != nil {
		return CreateQualityProfile400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateQualityProfile201JSONResponse(qualityProfileToAPI(profile)), nil
}

func (h *Handlers) GetQualityProfile(_ context.Context, req GetQualityProfileRequestObject) (GetQualityProfileResponseObject, error) {
	profile, err := h.store.GetQualityProfile(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetQualityProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "quality profile not found",
			}, nil
		}
		return nil, err
	}

	return GetQualityProfile200JSONResponse(qualityProfileToAPI(profile)), nil
}

func (h *Handlers) UpdateQualityProfile(_ context.Context, req UpdateQualityProfileRequestObject) (UpdateQualityProfileResponseObject, error) {
	profile, err := h.store.GetQualityProfile(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateQualityProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "quality profile not found",
			}, nil
		}
		return nil, err
	}

	resJSON, _ := json.Marshal(req.Body.Resolutions)
	profile.Name = req.Body.Name
	profile.Resolutions = string(resJSON)
	if req.Body.Sources != nil {
		srcJSON, _ := json.Marshal(*req.Body.Sources)
		profile.Sources = string(srcJSON)
	} else {
		profile.Sources = ""
	}
	if req.Body.ExcludeTags != nil {
		tagJSON, _ := json.Marshal(*req.Body.ExcludeTags)
		profile.ExcludeTags = string(tagJSON)
	} else {
		profile.ExcludeTags = ""
	}

	if err := h.store.UpdateQualityProfile(profile); err != nil {
		return nil, err
	}

	return UpdateQualityProfile200JSONResponse(qualityProfileToAPI(profile)), nil
}

func (h *Handlers) DeleteQualityProfile(_ context.Context, req DeleteQualityProfileRequestObject) (DeleteQualityProfileResponseObject, error) {
	if err := h.store.DeleteQualityProfile(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteQualityProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "quality profile not found",
			}, nil
		}
		return nil, err
	}

	return DeleteQualityProfile204Response{}, nil
}

// --- Conversion helpers ---

func libraryToAPI(lib *store.Library) Library {
	apiLib := Library{
		Id:        int64(lib.ID),
		Name:      lib.Name,
		Path:      lib.Path,
		MediaType: LibraryMediaType(lib.MediaType),
		CreatedAt: lib.CreatedAt,
		UpdatedAt: lib.UpdatedAt,
	}
	if lib.QualityProfileID != nil {
		qpID := int64(*lib.QualityProfileID)
		apiLib.QualityProfileId = &qpID
	}
	return apiLib
}

func mediaItemToAPI(item *store.MediaItem, meta *store.MediaMetadata) MediaItem {
	apiItem := MediaItem{
		Id:        int64(item.ID),
		LibraryId: int64(item.LibraryID),
		Title:     item.Title,
		MediaType: MediaItemMediaType(item.MediaType),
		Status:    MediaItemStatus(item.Status),
		Source:    MediaItemSource(item.Source),
		Year:      item.Year,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
	if item.QualityProfileID != nil {
		qpID := int64(*item.QualityProfileID)
		apiItem.QualityProfileId = &qpID
	}
	if item.MonitorNewSeasons {
		apiItem.MonitorNewSeasons = &item.MonitorNewSeasons
	}
	if meta != nil {
		apiMeta := mediaMetadataToAPI(meta)
		apiItem.Metadata = &apiMeta
	}
	return apiItem
}

func mediaMetadataToAPI(meta *store.MediaMetadata) MediaMetadata {
	m := MediaMetadata{
		Id:         int64(meta.ID),
		Source:     MediaMetadataSource(meta.Source),
		ExternalId: meta.ExternalID,
		Title:      meta.Title,
		Confidence: float32(meta.Confidence),
	}
	if meta.Overview != "" {
		m.Overview = &meta.Overview
	}
	if meta.PosterPath != "" {
		m.PosterPath = &meta.PosterPath
	}
	if meta.Genres != "" {
		m.Genres = &meta.Genres
	}
	if meta.Year != nil {
		m.Year = meta.Year
	}
	if meta.Rating != nil {
		r := float32(*meta.Rating)
		m.Rating = &r
	}
	if meta.Status != "" {
		m.Status = &meta.Status
	}
	if meta.Runtime != nil {
		m.Runtime = meta.Runtime
	}
	if meta.Seasons != nil {
		m.Seasons = meta.Seasons
	}
	return m
}

func qualityProfileToAPI(p *store.QualityProfile) QualityProfile {
	api := QualityProfile{
		Id:        int64(p.ID),
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}

	// Parse JSON arrays back to slices
	var resolutions []string
	if err := json.Unmarshal([]byte(p.Resolutions), &resolutions); err == nil {
		api.Resolutions = resolutions
	}
	if p.Sources != "" {
		var sources []string
		if err := json.Unmarshal([]byte(p.Sources), &sources); err == nil {
			api.Sources = &sources
		}
	}
	if p.ExcludeTags != "" {
		var tags []string
		if err := json.Unmarshal([]byte(p.ExcludeTags), &tags); err == nil {
			api.ExcludeTags = &tags
		}
	}

	return api
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

func candidateToAPI(c matching.Candidate) MatchCandidate {
	mc := MatchCandidate{
		Source:     MatchCandidateSource(c.Source),
		ExternalId: c.ExternalID,
		Title:      c.Title,
		Confidence: float32(c.Confidence),
	}
	if c.Overview != "" {
		mc.Overview = &c.Overview
	}
	if c.Year != nil {
		mc.Year = c.Year
	}
	if c.PosterURL != "" {
		mc.PosterUrl = &c.PosterURL
	}
	if c.ExistingMediaID != nil {
		id := int64(*c.ExistingMediaID)
		mc.ExistingMediaId = &id
	}
	return mc
}

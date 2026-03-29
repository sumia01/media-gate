package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/sync"
)

// Ensure Handlers implements the generated StrictServerInterface.
var _ StrictServerInterface = (*Handlers)(nil)

type Handlers struct {
	lib        *library.Service
	store      store.Store
	queue      *jobqueue.Queue
	settings   *settings.Service
	matchSvc   *matching.Service
	syncSvc    *sync.Service
	indexerSvc *indexer.Service
	posterDir  string
}

func NewHandlers(lib *library.Service, s store.Store, q *jobqueue.Queue, set *settings.Service, matchSvc *matching.Service, syncSvc *sync.Service, indexerSvc *indexer.Service, posterDir string) *Handlers {
	return &Handlers{lib: lib, store: s, queue: q, settings: set, matchSvc: matchSvc, syncSvc: syncSvc, indexerSvc: indexerSvc, posterDir: posterDir}
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
		_ = h.store.DeleteEpisodesByMediaItem(item.ID)
		_ = h.store.DeleteMediaMetadataByMediaItem(item.ID)
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

	apiEntries := scanEntriesToAPI(entries)

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

	apiEntries := scanEntriesToAPI(entries)

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

	opts := jobqueue.EnqueueOpts{}
	if req.Params.FullRematch != nil && *req.Params.FullRematch {
		opts.FullRematch = true
	}

	job, err := h.queue.Enqueue(jobqueue.JobTypeMatchLibrary, lib.ID, lib.Name, opts)
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

	if item.Source != "request" {
		return DeleteMediaItem409JSONResponse{
			Code:    http.StatusConflict,
			Message: "only requested media items can be deleted",
		}, nil
	}

	// Delete media files, metadata, episodes, and poster
	_ = h.store.DeleteMediaFilesByMediaItem(item.ID)
	_ = h.store.DeleteMediaMetadataByMediaItem(item.ID)
	_ = h.store.DeleteEpisodesByMediaItem(item.ID)

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

	return SearchMediaForLibrary200JSONResponse{Candidates: candidatesToAPI(candidates)}, nil
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

func (h *Handlers) GlobalSearch(_ context.Context, req GlobalSearchRequestObject) (GlobalSearchResponseObject, error) {
	candidates, err := h.matchSvc.SearchCandidates(req.Params.Query, string(req.Params.MediaType), nil, "")
	if err != nil {
		return nil, err
	}

	return GlobalSearch200JSONResponse{Candidates: candidatesToAPI(candidates)}, nil
}

func (h *Handlers) GetExternalMediaDetail(_ context.Context, req GetExternalMediaDetailRequestObject) (GetExternalMediaDetailResponseObject, error) {
	detail, err := h.matchSvc.GetExternalDetail(string(req.Source), string(req.Params.MediaType), req.ExternalId)
	if err != nil {
		return nil, err
	}

	apiDetail := ExternalMediaDetail{
		Source:     ExternalMediaDetailSource(detail.Source),
		ExternalId: detail.ExternalID,
		Title:      detail.Title,
		MediaType:  ExternalMediaDetailMediaType(detail.MediaType),
	}
	if detail.Overview != "" {
		apiDetail.Overview = &detail.Overview
	}
	if detail.PosterURL != "" {
		apiDetail.PosterUrl = &detail.PosterURL
	}
	if detail.Year != nil {
		apiDetail.Year = detail.Year
	}
	if detail.Genres != "" {
		apiDetail.Genres = &detail.Genres
	}
	if detail.Status != "" {
		apiDetail.Status = &detail.Status
	}
	if detail.Runtime != nil {
		apiDetail.Runtime = detail.Runtime
	}
	if detail.Seasons != nil {
		apiDetail.Seasons = detail.Seasons
	}
	if detail.ImdbID != "" {
		apiDetail.ImdbId = &detail.ImdbID
	}
	if detail.Credits != "" {
		var credits []CreditPerson
		if err := json.Unmarshal([]byte(detail.Credits), &credits); err == nil {
			apiDetail.Credits = &credits
		}
	}

	return GetExternalMediaDetail200JSONResponse(apiDetail), nil
}

// --- Resync handler ---

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

// --- Media File handlers ---

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

// --- Episode handlers ---

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

// --- Media Profile handlers ---

func (h *Handlers) ListMediaProfiles(_ context.Context, _ ListMediaProfilesRequestObject) (ListMediaProfilesResponseObject, error) {
	profiles, err := h.store.ListMediaProfiles()
	if err != nil {
		return nil, err
	}

	apiProfiles := make([]MediaProfile, len(profiles))
	for i := range profiles {
		apiProfiles[i] = mediaProfileToAPI(&profiles[i])
	}

	return ListMediaProfiles200JSONResponse{Profiles: apiProfiles}, nil
}

func (h *Handlers) CreateMediaProfile(_ context.Context, req CreateMediaProfileRequestObject) (CreateMediaProfileResponseObject, error) {
	profile := mediaProfileFromAPI(req.Body.Name, req.Body.Resolutions, req.Body.Languages, req.Body.Sources, req.Body.ExcludeTags)

	if err := h.store.CreateMediaProfile(profile); err != nil {
		return CreateMediaProfile400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateMediaProfile201JSONResponse(mediaProfileToAPI(profile)), nil
}

func (h *Handlers) GetMediaProfile(_ context.Context, req GetMediaProfileRequestObject) (GetMediaProfileResponseObject, error) {
	profile, err := h.store.GetMediaProfile(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetMediaProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media profile not found",
			}, nil
		}
		return nil, err
	}

	return GetMediaProfile200JSONResponse(mediaProfileToAPI(profile)), nil
}

func (h *Handlers) UpdateMediaProfile(_ context.Context, req UpdateMediaProfileRequestObject) (UpdateMediaProfileResponseObject, error) {
	profile, err := h.store.GetMediaProfile(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateMediaProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media profile not found",
			}, nil
		}
		return nil, err
	}

	updateMediaProfileFromAPI(profile, req.Body.Name, req.Body.Resolutions, req.Body.Languages, req.Body.Sources, req.Body.ExcludeTags)

	if err := h.store.UpdateMediaProfile(profile); err != nil {
		return nil, err
	}

	return UpdateMediaProfile200JSONResponse(mediaProfileToAPI(profile)), nil
}

func (h *Handlers) DeleteMediaProfile(_ context.Context, req DeleteMediaProfileRequestObject) (DeleteMediaProfileResponseObject, error) {
	if err := h.store.DeleteMediaProfile(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteMediaProfile404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "media profile not found",
			}, nil
		}
		return nil, err
	}

	return DeleteMediaProfile204Response{}, nil
}

// --- Indexer Definition handlers ---

func (h *Handlers) ListIndexerDefinitions(_ context.Context, _ ListIndexerDefinitionsRequestObject) (ListIndexerDefinitionsResponseObject, error) {
	defs := h.indexerSvc.ListDefinitions()
	apiDefs := make([]IndexerDefinition, len(defs))
	for i, d := range defs {
		apiDefs[i] = definitionToAPI(&d)
	}
	return ListIndexerDefinitions200JSONResponse{Definitions: apiDefs}, nil
}

func (h *Handlers) GetIndexerDefinition(_ context.Context, req GetIndexerDefinitionRequestObject) (GetIndexerDefinitionResponseObject, error) {
	def, err := h.indexerSvc.GetDefinition(req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetIndexerDefinition404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer definition not found",
			}, nil
		}
		return nil, err
	}
	return GetIndexerDefinition200JSONResponse(definitionToAPI(def)), nil
}

// --- Indexer CRUD handlers ---

func (h *Handlers) ListIndexers(_ context.Context, _ ListIndexersRequestObject) (ListIndexersResponseObject, error) {
	indexers, err := h.indexerSvc.List()
	if err != nil {
		return nil, err
	}
	apiIndexers := make([]Indexer, len(indexers))
	for i, idx := range indexers {
		apiIndexers[i] = indexerInfoToAPI(&idx)
	}
	return ListIndexers200JSONResponse{Indexers: apiIndexers}, nil
}

func (h *Handlers) CreateIndexer(_ context.Context, req CreateIndexerRequestObject) (CreateIndexerResponseObject, error) {
	settings := make(map[string]string)
	if req.Body.Settings != nil {
		settings = *req.Body.Settings
	}

	priority := 0
	if req.Body.Priority != nil {
		priority = *req.Body.Priority
	}

	info, err := h.indexerSvc.Create(req.Body.Name, req.Body.DefinitionId, settings, priority)
	if err != nil {
		return CreateIndexer400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateIndexer201JSONResponse(indexerInfoToAPI(info)), nil
}

func (h *Handlers) GetIndexer(_ context.Context, req GetIndexerRequestObject) (GetIndexerResponseObject, error) {
	info, err := h.indexerSvc.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetIndexer404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return GetIndexer200JSONResponse(indexerInfoToAPI(info)), nil
}

func (h *Handlers) UpdateIndexer(_ context.Context, req UpdateIndexerRequestObject) (UpdateIndexerResponseObject, error) {
	var settings map[string]string
	if req.Body.Settings != nil {
		settings = *req.Body.Settings
	}

	info, err := h.indexerSvc.Update(uint(req.Id), req.Body.Name, settings, req.Body.Enabled, req.Body.Priority)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateIndexer404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return UpdateIndexer200JSONResponse(indexerInfoToAPI(info)), nil
}

func (h *Handlers) DeleteIndexer(_ context.Context, req DeleteIndexerRequestObject) (DeleteIndexerResponseObject, error) {
	if err := h.indexerSvc.Delete(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteIndexer404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return DeleteIndexer204Response{}, nil
}

func (h *Handlers) TestIndexerConnection(ctx context.Context, req TestIndexerConnectionRequestObject) (TestIndexerConnectionResponseObject, error) {
	var overrideSettings map[string]string
	if req.Body != nil && req.Body.Settings != nil {
		overrideSettings = *req.Body.Settings
	}

	success, msg, err := h.indexerSvc.TestConnection(uint(req.Id), overrideSettings)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TestIndexerConnection404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return TestIndexerConnection200JSONResponse{Success: success, Message: &msg}, nil
}

func (h *Handlers) SearchIndexers(ctx context.Context, req SearchIndexersRequestObject) (SearchIndexersResponseObject, error) {
	params := indexer.SearchParams{}

	if req.Params.Query != nil {
		params.Query = *req.Params.Query
	}
	if req.Params.ImdbId != nil {
		params.ImdbID = *req.Params.ImdbId
	}
	if req.Params.Type != nil {
		params.Type = string(*req.Params.Type)
	}
	if req.Params.Season != nil {
		params.Season = *req.Params.Season
	}
	if req.Params.Episode != nil {
		params.Episode = *req.Params.Episode
	}
	if req.Params.Categories != nil {
		params.Categories = strings.Split(*req.Params.Categories, ",")
	}
	if req.Params.IndexerIds != nil {
		for _, idStr := range strings.Split(*req.Params.IndexerIds, ",") {
			id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 64)
			if err == nil {
				params.IndexerIDs = append(params.IndexerIDs, uint(id))
			}
		}
	}
	if req.Params.Limit != nil {
		params.Limit = *req.Params.Limit
	}

	results, err := h.indexerSvc.Search(ctx, params)
	if err != nil {
		return nil, err
	}

	apiResults := make([]TorrentResult, len(results))
	for i, r := range results {
		apiResults[i] = torrentResultToAPI(&r)
	}

	return SearchIndexers200JSONResponse{Results: apiResults}, nil
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
	if lib.MediaProfileID != nil {
		mpID := int64(*lib.MediaProfileID)
		apiLib.MediaProfileId = &mpID
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
	if item.MediaProfileID != nil {
		mpID := int64(*item.MediaProfileID)
		apiItem.MediaProfileId = &mpID
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
	if meta.ImdbID != "" {
		m.ImdbId = &meta.ImdbID
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
	if meta.Credits != "" {
		var credits []CreditPerson
		if err := json.Unmarshal([]byte(meta.Credits), &credits); err == nil {
			m.Credits = &credits
		}
	}
	return m
}

func mediaProfileToAPI(p *store.MediaProfile) MediaProfile {
	api := MediaProfile{
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
	var languages []string
	if err := json.Unmarshal([]byte(p.Languages), &languages); err == nil {
		api.Languages = languages
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

func mediaFileToAPI(f *store.MediaFile) MediaFile {
	api := MediaFile{
		Id:          int64(f.ID),
		MediaItemId: int64(f.MediaItemID),
		Path:        f.Path,
		FileName:    f.FileName,
		AddedAt:     f.AddedAt,
	}
	if f.Size > 0 {
		api.Size = &f.Size
	}
	if f.Resolution != "" {
		api.Resolution = &f.Resolution
	}
	if f.SourceType != "" {
		api.SourceType = &f.SourceType
	}
	if f.SeasonNumber != nil {
		api.SeasonNumber = f.SeasonNumber
	}
	if f.EpisodeNumber != nil {
		api.EpisodeNumber = f.EpisodeNumber
	}
	return api
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

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func scanEntriesToAPI(entries []library.ScanEntry) []ScanEntry {
	result := make([]ScanEntry, len(entries))
	for i, e := range entries {
		result[i] = ScanEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDirectory,
			Size:        e.Size,
			ModifiedAt:  e.ModifiedAt,
		}
	}
	return result
}

func candidatesToAPI(candidates []matching.Candidate) []MatchCandidate {
	result := make([]MatchCandidate, len(candidates))
	for i, c := range candidates {
		result[i] = candidateToAPI(c)
	}
	return result
}

func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func mediaProfileFromAPI(name string, resolutions, languages []string, sources, excludeTags *[]string) *store.MediaProfile {
	p := &store.MediaProfile{
		Name:        name,
		Resolutions: marshalJSON(resolutions),
		Languages:   marshalJSON(languages),
	}
	if sources != nil {
		p.Sources = marshalJSON(*sources)
	}
	if excludeTags != nil {
		p.ExcludeTags = marshalJSON(*excludeTags)
	}
	return p
}

func updateMediaProfileFromAPI(p *store.MediaProfile, name string, resolutions, languages []string, sources, excludeTags *[]string) {
	p.Name = name
	p.Resolutions = marshalJSON(resolutions)
	p.Languages = marshalJSON(languages)
	if sources != nil {
		p.Sources = marshalJSON(*sources)
	} else {
		p.Sources = ""
	}
	if excludeTags != nil {
		p.ExcludeTags = marshalJSON(*excludeTags)
	} else {
		p.ExcludeTags = ""
	}
}

func definitionToAPI(d *indexer.DefinitionInfo) IndexerDefinition {
	api := IndexerDefinition{
		Id:       d.ID,
		Name:     d.Name,
		Type:     d.Type,
		Language: d.Language,
	}
	if d.Description != "" {
		api.Description = &d.Description
	}
	if len(d.Links) > 0 {
		api.Links = &d.Links
	}
	if len(d.Settings) > 0 {
		settings := make([]IndexerDefinitionSetting, len(d.Settings))
		for i, s := range d.Settings {
			settings[i] = IndexerDefinitionSetting{
				Name:  s.Name,
				Type:  s.Type,
				Label: s.Label,
			}
			if s.Default != "" {
				settings[i].Default = &s.Default
			}
		}
		api.Settings = &settings
	}
	return api
}

func indexerInfoToAPI(info *indexer.IndexerInfo) Indexer {
	api := Indexer{
		Id:           int64(info.ID),
		Name:         info.Name,
		DefinitionId: info.DefinitionID,
		Enabled:      info.Enabled,
		Priority:     info.Priority,
	}
	if len(info.Settings) > 0 {
		s := info.Settings
		api.Settings = &s
	}
	return api
}

func torrentResultToAPI(r *indexer.TorrentResult) TorrentResult {
	api := TorrentResult{
		IndexerId:   int64(r.IndexerID),
		IndexerName: r.IndexerName,
		Title:       r.Title,
		Size:        r.Size,
		Seeders:     r.Seeders,
		Leechers:    r.Leechers,
		Date:        r.Date,
	}
	if r.DetailsURL != "" {
		api.DetailsUrl = &r.DetailsURL
	}
	if r.DownloadURL != "" {
		api.DownloadUrl = &r.DownloadURL
	}
	if r.Category != "" {
		api.Category = &r.Category
	}
	if r.CategoryDesc != "" {
		api.CategoryDesc = &r.CategoryDesc
	}
	if r.ImdbID != "" {
		api.ImdbId = &r.ImdbID
	}
	if r.DownloadVolumeFactor != 1.0 {
		dvf := float32(r.DownloadVolumeFactor)
		api.DownloadVolumeFactor = &dvf
	}
	if r.UploadVolumeFactor != 1.0 {
		uvf := float32(r.UploadVolumeFactor)
		api.UploadVolumeFactor = &uvf
	}
	return api
}

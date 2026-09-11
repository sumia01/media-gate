package matching

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/dateutil"
	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/ratelimit"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

const (
	autoMatchThreshold = 0.8
	tmdbPosterBase     = "https://image.tmdb.org/t/p/w500"
)

var (
	ErrNoRequestedScope      = errors.New("select at least one season or episode")
	ErrStaleMatchCandidate   = errors.New("stale match candidate")
	ErrStaleRefreshCandidate = errors.New("stale metadata refresh candidate")
)

type Candidate struct {
	Source          string
	ExternalID      int
	Title           string
	Overview        string
	Year            *int
	PosterURL       string
	Confidence      float64
	ExistingMediaID *uint
}

// StatusRecalculator recalculates a media item's status based on current files
// and episodes. Implemented by sync.Service; defined here to avoid circular imports.
type StatusRecalculator interface {
	RecalcMediaItemStatus(itemID uint) error
}

type Service struct {
	store        store.Store
	settings     *settings.Service
	posterDir    string
	httpClient   *http.Client
	statusRecalc StatusRecalculator
	bus          *eventbus.Bus
	posterMu     *sync.Mutex
	requestMu    *sync.Mutex

	tmdbMu     sync.Mutex
	tmdbKey    string
	tmdbCached *tmdb.Client

	tvdbMu     sync.Mutex
	tvdbKey    string
	tvdbCached *tvdb.Client
}

func NewService(s store.Store, set *settings.Service, posterDir string, httpClient *http.Client) *Service {
	return &Service{
		store:      s,
		settings:   set,
		posterDir:  posterDir,
		httpClient: httpClient,
		posterMu:   &sync.Mutex{},
		requestMu:  &sync.Mutex{},
	}
}

// SetStatusRecalculator injects the status recalculator after construction
// to break the init cycle between matching and sync packages.
func (s *Service) SetStatusRecalculator(r StatusRecalculator) {
	s.statusRecalc = r
}

// SetBus injects the event bus for publishing media item events.
func (s *Service) SetBus(b *eventbus.Bus) {
	s.bus = b
}

// WithStore returns a shallow copy of the Service that uses the given store.
// Useful for running operations inside a database transaction.
func (s *Service) WithStore(st store.Store) *Service {
	return &Service{
		store:        st,
		settings:     s.settings,
		posterDir:    s.posterDir,
		httpClient:   s.httpClient,
		statusRecalc: s.statusRecalc,
		bus:          s.bus,
		posterMu:     s.posterMu,
		requestMu:    s.requestMu,
		tmdbKey:      s.tmdbKey,
		tmdbCached:   s.tmdbCached,
		tvdbKey:      s.tvdbKey,
		tvdbCached:   s.tvdbCached,
	}
}

func (s *Service) MatchLibrary(lib *store.Library, fullRematch bool, progressFn func(current, total int, itemID uint)) error {
	source := s.settings.GetWithDefault(settings.KeyMetadataPrimarySource, "tmdb")
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return fmt.Errorf("no API key configured for %s", source)
	}

	rpsStr := s.settings.GetWithDefault(s.rateLimitKey(source), "4")
	rps, _ := strconv.Atoi(rpsStr)
	if rps <= 0 {
		rps = 4
	}

	var items []store.MediaItem
	if fullRematch {
		items, err = s.store.ListMediaItemsByLibrary(lib.ID)
	} else {
		items, err = s.store.ListNewMediaItemsByLibrary(lib.ID)
	}
	if err != nil {
		return fmt.Errorf("listing items: %w", err)
	}

	if len(items) == 0 {
		if progressFn != nil {
			progressFn(0, 0, 0)
		}
		return nil
	}

	limiter := ratelimit.New(rps)
	defer limiter.Stop()

	ctx := context.Background()
	matched := 0
	for i := range items {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}

		// Report BEFORE matching so subscribers can indicate which item is
		// being worked on right now; "current" means "working on the Nth".
		if progressFn != nil {
			progressFn(i+1, len(items), items[i].ID)
		}

		if err := s.matchSingleItem(items[i].ID, source, apiKey, lib.MediaType, fullRematch); err != nil {
			slog.Warn("match failed for item", "item_id", items[i].ID, "title", items[i].Title, "error", err)
		} else if current, err := s.store.GetMediaItem(items[i].ID); err == nil && current.Status == "available" {
			matched++
		}
	}

	slog.Info("matching complete", "library", lib.Name, "total", len(items), "matched", matched)
	return nil
}

func (s *Service) matchSingleItem(mediaItemID uint, source, apiKey, mediaType string, fullRematch bool) error {
	expected, err := s.captureMatchVersion(mediaItemID)
	if err != nil {
		return err
	}
	if !fullRematch && (expected.metadata != nil || expected.item.Status != "new") {
		return nil
	}

	candidates, err := s.searchSource(expected.item.Title, mediaType, expected.item.Year, source, apiKey)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	if best.Confidence < autoMatchThreshold {
		return nil
	}

	meta, episodes, err := s.fetchMatchData(source, apiKey, mediaType, best.ExternalID, best.Confidence)
	if err != nil {
		return err
	}
	_, _, err = s.replaceMatch(expected, meta, episodes, matchActor{component: "matching"})
	return err
}

func (s *Service) SearchCandidates(query, mediaType string, year *int, source string) ([]Candidate, error) {
	if source == "" {
		source = s.settings.GetWithDefault(settings.KeyMetadataPrimarySource, "tmdb")
	}
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, fmt.Errorf("no API key configured for %s", source)
	}
	return s.searchSource(query, mediaType, year, source, apiKey)
}

// ExternalDetail holds metadata fetched from an external source without persisting anything.
type ExternalDetail struct {
	Source     string
	ExternalID int
	Title      string
	Overview   string
	PosterURL  string
	Year       *int
	MediaType  string
	Genres     string
	Credits    string
	Status     string
	Runtime    *int
	Seasons    *int
	ImdbID     string
	TrailerURL string
	// ContentRatings is the full provider certification list as JSON, same
	// shape as store.MediaMetadata.ContentRatings. Filtering to the user's
	// selected countries happens at the API layer, as it does for library items.
	ContentRatings string
}

// GetExternalDetail fetches full metadata from TMDB/TVDB for preview without creating DB records.
func (s *Service) GetExternalDetail(source, mediaType string, externalID int) (*ExternalDetail, error) {
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, fmt.Errorf("no API key configured for %s", source)
	}

	meta := &store.MediaMetadata{Source: source, ExternalID: externalID}

	if source == "tvdb" {
		if err := s.fetchTVDBDetails(apiKey, externalID, meta); err != nil {
			return nil, err
		}
	} else {
		if err := s.fetchTMDBDetails(apiKey, mediaType, externalID, meta); err != nil {
			return nil, err
		}
	}

	return &ExternalDetail{
		Source:     source,
		ExternalID: externalID,
		Title:      meta.Title,
		Overview:   meta.Overview,
		PosterURL:  s.posterURL(source, meta),
		Year:       meta.Year,
		MediaType:  mediaType,
		Genres:     meta.Genres,
		Credits:    meta.Credits,
		Status:     meta.Status,
		Runtime:    meta.Runtime,
		Seasons:    meta.Seasons,
		ImdbID:     meta.ImdbID,
		TrailerURL: meta.TrailerURL,
		// Already populated by the fetch above — the preview path reuses the
		// same provider mapping as a real match, it just never persists it.
		ContentRatings: meta.ContentRatings,
	}, nil
}

func (s *Service) ManualMatch(mediaItemID uint, source string, externalID int, actorUserID uint) (*store.MediaItem, *store.MediaMetadata, error) {
	if err := validateMatchActor(s.store, matchActor{userID: actorUserID}); err != nil {
		return nil, nil, err
	}
	expected, err := s.captureMatchVersion(mediaItemID)
	if err != nil {
		return nil, nil, err
	}

	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, nil, fmt.Errorf("no API key configured for %s", source)
	}

	meta, episodes, err := s.fetchMatchData(source, apiKey, expected.item.MediaType, externalID, 1.0)
	if err != nil {
		return nil, nil, err
	}
	return s.replaceMatch(expected, meta, episodes, matchActor{userID: actorUserID})
}

func (s *Service) Unmatch(mediaItemID, actorUserID uint) error {
	s.posterMu.Lock()
	defer s.posterMu.Unlock()
	err := s.store.WithTx(func(tx store.Store) error {
		if err := validateMatchActor(tx, matchActor{userID: actorUserID}); err != nil {
			return err
		}
		item, err := tx.GetMediaItem(mediaItemID)
		if err != nil {
			return err
		}
		if item.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		meta, err := tx.GetMediaMetadataByMediaItem(mediaItemID)
		if err != nil {
			return err
		}
		episodes, err := tx.ListEpisodesByMediaItem(mediaItemID)
		if err != nil {
			return fmt.Errorf("listing episodes: %w", err)
		}
		overrides, err := tx.ListEpisodeMonitorsByMediaItem(mediaItemID)
		if err != nil {
			return fmt.Errorf("listing episode overrides: %w", err)
		}
		beforeMonitoring, err := mediasync.SnapshotMonitoring(tx, mediaItemID)
		if err != nil {
			return fmt.Errorf("snapshotting monitoring before unmatch: %w", err)
		}

		if err := tx.DeleteMediaMetadataByMediaItem(mediaItemID); err != nil {
			return fmt.Errorf("deleting metadata: %w", err)
		}
		if err := tx.DeleteEpisodesByMediaItem(mediaItemID); err != nil {
			return fmt.Errorf("deleting episodes: %w", err)
		}
		if err := tx.DeleteEpisodeMonitorsByMediaItem(mediaItemID); err != nil {
			return fmt.Errorf("deleting episode overrides: %w", err)
		}
		if item.Source == "request" {
			item.Status = "requested"
		} else {
			item.Status = "new"
		}
		if err := tx.UpdateMediaItem(item); err != nil {
			return fmt.Errorf("updating unmatched item: %w", err)
		}
		afterMonitoring, err := mediasync.SnapshotMonitoring(tx, mediaItemID)
		if err != nil {
			return fmt.Errorf("snapshotting monitoring after unmatch: %w", err)
		}

		details := mediasync.BuildMonitoringActivityDetails(beforeMonitoring, afterMonitoring)
		details.OldProvider = providerIdentity(meta)
		details.Removed = len(episodes)
		details.DatabaseRecordsRemoved = 1 + len(episodes) + len(overrides)
		details.Targets, details.Truncated = boundedEpisodeTargets(episodeKeysFromStored(episodes), details.Truncated)
		details.Total += len(episodes)
		operationID, err := store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		activity, err := store.NewUserMediaActivity(item, actorUserID, store.MediaActivityActionUnmatched, operationID, store.MediaActivityVisibilityShared, details)
		if err != nil {
			return fmt.Errorf("creating unmatch activity: %w", err)
		}
		if err := tx.AppendMediaActivity(activity); err != nil {
			return fmt.Errorf("recording unmatch activity: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publishActivityAdded(mediaItemID)
	return nil
}

// SearchForLibrary searches external sources based on library media type.
// It also annotates candidates with existing media item IDs if they already exist in the library.
func (s *Service) SearchForLibrary(lib *store.Library, query string) ([]Candidate, error) {
	source := s.settings.GetWithDefault(settings.KeyMetadataPrimarySource, "tmdb")
	candidates, err := s.SearchCandidates(query, lib.MediaType, nil, source)
	if err != nil {
		return nil, err
	}

	// Check which candidates already exist in the library
	items, err := s.store.ListMediaItemsByLibrary(lib.ID)
	if err != nil {
		return nil, fmt.Errorf("listing library items: %w", err)
	}

	// Build a lookup: source+externalID → media item ID
	ids := make([]uint, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	metas, err := s.store.ListMediaMetadataByMediaItemIDs(ids)
	if err != nil {
		return nil, fmt.Errorf("listing metadata: %w", err)
	}

	type metaKey struct {
		source     string
		externalID int
	}
	existingMap := make(map[metaKey]uint, len(metas))
	for _, m := range metas {
		existingMap[metaKey{source: m.Source, externalID: m.ExternalID}] = m.MediaItemID
	}

	for i, c := range candidates {
		if mediaItemID, ok := existingMap[metaKey{source: c.Source, externalID: c.ExternalID}]; ok {
			candidates[i].ExistingMediaID = &mediaItemID
		}
	}

	return candidates, nil
}

// AddMediaRequest holds parameters for adding media to a library with optional
// monitoring configuration.
type AddMediaRequest struct {
	Source            string
	ExternalID        int
	RequesterID       *uint
	Monitored         *bool
	MonitorNewSeasons *bool
	MediaProfileID    *uint
	SeasonMonitors    []SeasonMonitorReq
	EpisodeMonitors   []EpisodeMonitorReq
}

// SeasonMonitorReq represents a season monitor setting.
type SeasonMonitorReq struct {
	SeasonNumber int
	Monitored    bool
}

// EpisodeMonitorReq represents an episode monitor setting.
type EpisodeMonitorReq struct {
	SeasonNumber  int
	EpisodeNumber int
	Monitored     bool
}

// AddMediaToLibraryFull creates a media item and its metadata/episodes/monitors
// atomically, then downloads the poster and recalculates status outside the
// transaction.
//
// Ordering is deliberate to avoid holding SQLite's single write lock during
// network I/O (which caused "database is locked" storms) and to make the
// status recalc actually run:
//  1. Fetch ALL external metadata + episodes into memory BEFORE the tx.
//  2. Open a SHORT write transaction that ONLY does DB writes from that data.
//  3. Recalc status and publish the event AFTER commit, via the top-level
//     store, so the recalculator can see the committed item (previously it ran
//     inside the tx against a connection that couldn't see the uncommitted row,
//     so it silently no-op'd).
func (s *Service) AddMediaToLibraryFull(topStore store.Store, lib *store.Library, req AddMediaRequest) (*store.MediaItem, *store.MediaMetadata, bool, error) {
	// Existing media can receive additional request attribution without another
	// metadata fetch. Request rows are idempotent for each user and scope.
	existing, err := topStore.GetMediaItemByExternalID(lib.ID, req.Source, req.ExternalID)
	if err == nil {
		return s.addRequestToExisting(topStore, existing.ID, req)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, false, fmt.Errorf("checking for duplicates: %w", err)
	}

	apiKey, err := s.resolveAPIKey(req.Source)
	if err != nil {
		return nil, nil, false, fmt.Errorf("no API key configured for %s", req.Source)
	}

	// Fetch ALL external metadata and episodes BEFORE opening the transaction.
	// No network I/O happens while the write lock is held. If the fetch fails,
	// we abort here before any write, so nothing is half-created.
	meta, episodes, err := s.fetchMatchData(req.Source, apiKey, lib.MediaType, req.ExternalID, 1.0)
	if err != nil {
		return nil, nil, false, fmt.Errorf("fetching metadata: %w", err)
	}
	originalReq := req
	req = normalizeSeriesRequest(lib.MediaType, req, episodes, meta.Seasons)
	scopes := buildRequestScopes(lib.MediaType, req, episodes, meta.Seasons)
	if !hasRequestedContent(lib.MediaType, req, scopes) {
		return nil, nil, false, ErrNoRequestedScope
	}

	var resultItem *store.MediaItem
	activityAdded := false
	racedExisting := false
	retryExisting := false

	// Short write transaction: only DB writes from the already-fetched data.
	// If anything fails the whole tx rolls back — nothing is half-written.
	s.requestMu.Lock()
	err = topStore.WithTx(func(tx store.Store) error {
		existing, err := tx.GetMediaItemByExternalID(lib.ID, originalReq.Source, originalReq.ExternalID)
		if err == nil {
			if existing.DeletionPending {
				return store.ErrMediaDeletionPending
			}
			resultItem, activityAdded, err = applyRequestToExistingTx(tx, existing.ID, originalReq)
			racedExisting = err == nil
			return err
		}
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("rechecking for duplicates: %w", err)
		}

		txSvc := s.WithStore(tx)

		item := &store.MediaItem{
			LibraryID: lib.ID,
			Title:     "pending",
			MediaType: lib.MediaType,
			Status:    "requested",
			Source:    "request",
		}
		// Populate title/year from the fetched metadata so the item is created
		// once with its final values (no second update needed).
		if meta.Title != "" {
			item.Title = meta.Title
		}
		item.Year = meta.Year

		if req.Monitored != nil {
			item.Monitored = *req.Monitored
		}
		if req.MonitorNewSeasons != nil {
			item.MonitorNewSeasons = *req.MonitorNewSeasons
		}
		if req.MediaProfileID != nil {
			profileID := *req.MediaProfileID
			item.MediaProfileID = &profileID
		}
		assignedProfile, err := requestedProfile(tx, item.MediaProfileID)
		if err != nil {
			return err
		}

		if err := tx.CreateMediaItem(item); err != nil {
			retryExisting = true
			return fmt.Errorf("creating media item: %w", err)
		}

		// Persist metadata + episodes from the in-memory fetch (no network here).
		if err := txSvc.persistMatch(item, meta, episodes); err != nil {
			return fmt.Errorf("applying match: %w", err)
		}

		for _, sm := range req.SeasonMonitors {
			if err := tx.CreateSeasonMonitor(&store.SeasonMonitor{
				MediaItemID:  item.ID,
				SeasonNumber: sm.SeasonNumber,
				Monitored:    sm.Monitored,
			}); err != nil {
				return fmt.Errorf("creating season monitor for S%02d: %w", sm.SeasonNumber, err)
			}
		}

		for _, em := range req.EpisodeMonitors {
			if err := tx.UpsertEpisodeMonitor(&store.EpisodeMonitor{
				MediaItemID:   item.ID,
				SeasonNumber:  em.SeasonNumber,
				EpisodeNumber: em.EpisodeNumber,
				Monitored:     em.Monitored,
			}); err != nil {
				return fmt.Errorf("creating episode monitor for S%02dE%02d: %w", em.SeasonNumber, em.EpisodeNumber, err)
			}
		}

		if err := createMediaRequests(tx, item.ID, req.RequesterID, scopes); err != nil {
			return err
		}
		if req.RequesterID != nil {
			after, err := mediasync.SnapshotMonitoring(tx, item.ID)
			if err != nil {
				return fmt.Errorf("snapshotting monitoring after request: %w", err)
			}
			before := mediasync.NewInitialMonitoringState(*item)
			if err := appendRequestActivity(tx, item, *req.RequesterID, scopes, true, before, after, assignedProfile); err != nil {
				return err
			}
			activityAdded = true
		}

		resultItem = item
		return nil
	})
	s.requestMu.Unlock()
	if err != nil {
		// A separately-constructed service or process can win after our recheck.
		// Retry only a failed first write; later failures must remain atomic errors.
		if retryExisting {
			if existing, lookupErr := topStore.GetMediaItemByExternalID(lib.ID, originalReq.Source, originalReq.ExternalID); lookupErr == nil {
				return s.addRequestToExisting(topStore, existing.ID, originalReq)
			}
		}
		return nil, nil, false, err
	}
	if racedExisting {
		return s.finishExistingRequest(topStore, resultItem, originalReq, activityAdded)
	}

	if activityAdded {
		s.publishActivityAdded(resultItem.ID)
	}

	// Post-commit side effects on the top-level store. The poster is fetched
	// outside the transaction (no DB write lock held during network I/O) and
	// before afterMatch, which publishes MediaItemMatched: that event makes the
	// frontend re-request the poster, so the file has to be in place first.
	s.DownloadPoster(resultItem.ID)

	// The recalculator now sees the committed item + episodes, so status is
	// actually recalculated; the matched event is also published here.
	s.afterMatch(resultItem, meta, req.Source, req.ExternalID)

	// Re-read metadata to include poster path and any post-commit changes.
	resultMeta, _ := topStore.GetMediaMetadataByMediaItem(resultItem.ID)
	slog.Info("matching: media added to library", "media_item_id", resultItem.ID, "title", resultItem.Title, "library_id", lib.ID, "library", lib.Name)
	return resultItem, resultMeta, true, nil
}

func (s *Service) addRequestToExisting(topStore store.Store, itemID uint, req AddMediaRequest) (*store.MediaItem, *store.MediaMetadata, bool, error) {
	var resultItem *store.MediaItem
	activityAdded := false
	s.requestMu.Lock()
	err := topStore.WithTx(func(tx store.Store) error {
		var err error
		resultItem, activityAdded, err = applyRequestToExistingTx(tx, itemID, req)
		return err
	})
	s.requestMu.Unlock()
	if err != nil {
		return nil, nil, false, err
	}
	return s.finishExistingRequest(topStore, resultItem, req, activityAdded)
}

func applyRequestToExistingTx(tx store.Store, itemID uint, req AddMediaRequest) (*store.MediaItem, bool, error) {
	current, err := tx.GetMediaItem(itemID)
	if err != nil {
		return nil, false, err
	}
	if current.DeletionPending {
		return nil, false, store.ErrMediaDeletionPending
	}
	storedEpisodes, err := tx.ListEpisodesByMediaItem(current.ID)
	if err != nil {
		return nil, false, fmt.Errorf("listing existing episodes: %w", err)
	}
	episodes := make([]episodeData, len(storedEpisodes))
	for i := range storedEpisodes {
		episodes[i] = episodeData{seasonNumber: storedEpisodes[i].SeasonNumber, episodeNumber: storedEpisodes[i].EpisodeNumber}
	}
	var seasonCount *int
	meta, err := tx.GetMediaMetadataByMediaItem(current.ID)
	if err == nil {
		seasonCount = meta.Seasons
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, false, fmt.Errorf("loading existing metadata: %w", err)
	}

	req = normalizeSeriesRequest(current.MediaType, req, episodes, seasonCount)
	scopes := buildRequestScopes(current.MediaType, req, episodes, seasonCount)
	if !hasRequestedContent(current.MediaType, req, scopes) {
		return nil, false, ErrNoRequestedScope
	}

	var before *mediasync.MonitoringState
	if req.RequesterID != nil {
		before, err = mediasync.SnapshotMonitoring(tx, current.ID)
		if err != nil {
			return nil, false, fmt.Errorf("snapshotting monitoring before request: %w", err)
		}
	}
	assignedProfile, err := applyExistingRequest(tx, current, req, scopes)
	if err != nil {
		return nil, false, err
	}
	if err := createMediaRequests(tx, current.ID, req.RequesterID, scopes); err != nil {
		return nil, false, err
	}
	if req.RequesterID == nil {
		return current, false, nil
	}
	after, err := mediasync.SnapshotMonitoring(tx, current.ID)
	if err != nil {
		return nil, false, fmt.Errorf("snapshotting monitoring after request: %w", err)
	}
	if err := appendRequestActivity(tx, current, *req.RequesterID, scopes, false, before, after, assignedProfile); err != nil {
		return nil, false, err
	}
	return current, true, nil
}

func (s *Service) finishExistingRequest(topStore store.Store, resultItem *store.MediaItem, req AddMediaRequest, activityAdded bool) (*store.MediaItem, *store.MediaMetadata, bool, error) {
	if activityAdded {
		s.publishActivityAdded(resultItem.ID)
	}
	if req.Monitored != nil && *req.Monitored && s.statusRecalc != nil {
		if err := s.statusRecalc.RecalcMediaItemStatus(resultItem.ID); err != nil {
			slog.Warn("request: status recalc failed", "media_item_id", resultItem.ID, "error", err)
		}
	}
	if fresh, err := topStore.GetMediaItem(resultItem.ID); err == nil {
		resultItem = fresh
	}
	meta, _ := topStore.GetMediaMetadataByMediaItem(resultItem.ID)
	if s.bus != nil {
		s.bus.Publish(eventbus.MediaRequestAdded, eventbus.MediaItemPayload{
			MediaItemID: resultItem.ID,
			LibraryID:   resultItem.LibraryID,
			Title:       resultItem.Title,
		})
	}
	return resultItem, meta, false, nil
}

type requestScope struct {
	scope         string
	seasonNumber  int
	episodeNumber int
}

func normalizeSeriesRequest(mediaType string, req AddMediaRequest, episodes []episodeData, seasonCount *int) AddMediaRequest {
	if mediaType != "series" || req.Monitored == nil || !*req.Monitored {
		return req
	}
	if req.MonitorNewSeasons == nil {
		monitorNewSeasons := true
		req.MonitorNewSeasons = &monitorNewSeasons
	}
	fillKnownSeasons := len(req.SeasonMonitors) == 0
	if !fillKnownSeasons && *req.MonitorNewSeasons {
		fillKnownSeasons = true
		for _, season := range req.SeasonMonitors {
			fillKnownSeasons = fillKnownSeasons && season.Monitored
		}
		for _, episode := range req.EpisodeMonitors {
			fillKnownSeasons = fillKnownSeasons && episode.Monitored
		}
	}
	if !fillKnownSeasons {
		return req
	}
	seen := make(map[int]struct{})
	for _, season := range req.SeasonMonitors {
		seen[season.SeasonNumber] = struct{}{}
	}
	addSeason := func(seasonNumber int) {
		if _, ok := seen[seasonNumber]; ok {
			return
		}
		seen[seasonNumber] = struct{}{}
		req.SeasonMonitors = append(req.SeasonMonitors, SeasonMonitorReq{
			SeasonNumber: seasonNumber,
			Monitored:    true,
		})
	}
	if seasonCount != nil {
		for season := 1; season <= *seasonCount; season++ {
			addSeason(season)
		}
	}
	for _, episode := range episodes {
		addSeason(episode.seasonNumber)
	}
	sort.Slice(req.SeasonMonitors, func(i, j int) bool {
		return req.SeasonMonitors[i].SeasonNumber < req.SeasonMonitors[j].SeasonNumber
	})
	return req
}

func buildRequestScopes(mediaType string, req AddMediaRequest, episodes []episodeData, seasonCount *int) []requestScope {
	seen := map[requestScope]struct{}{
		requestScope{scope: store.MediaRequestScopeMedia}: {},
	}
	if mediaType != "series" || req.Monitored == nil || !*req.Monitored {
		return sortedRequestScopes(seen)
	}
	if req.MonitorNewSeasons != nil && *req.MonitorNewSeasons {
		seen[requestScope{scope: store.MediaRequestScopeFutureSeasons}] = struct{}{}
	}
	if len(req.SeasonMonitors) == 0 {
		return sortedRequestScopes(seen)
	}

	type episodeKey struct {
		season  int
		episode int
	}
	overrides := make(map[episodeKey]bool, len(req.EpisodeMonitors))
	for _, episode := range req.EpisodeMonitors {
		overrides[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}] = episode.Monitored
	}

	seasonEpisodes := make(map[int][]int)
	for _, episode := range episodes {
		seasonEpisodes[episode.seasonNumber] = append(seasonEpisodes[episode.seasonNumber], episode.episodeNumber)
	}
	seasonDefaults := make(map[int]bool, len(req.SeasonMonitors))
	allSubmittedSeasons := true
	for _, season := range req.SeasonMonitors {
		seasonDefaults[season.SeasonNumber] = season.Monitored
		allSubmittedSeasons = allSubmittedSeasons && season.Monitored
	}
	allKnownSeasons := true
	if seasonCount != nil {
		for seasonNumber := 1; seasonNumber <= *seasonCount; seasonNumber++ {
			if !seasonDefaults[seasonNumber] {
				allKnownSeasons = false
				break
			}
		}
	}
	allKnownEpisodes := len(episodes) > 0
	for _, episode := range episodes {
		monitored := seasonDefaults[episode.seasonNumber]
		if override, ok := overrides[episodeKey{season: episode.seasonNumber, episode: episode.episodeNumber}]; ok {
			monitored = override
		}
		if !monitored {
			allKnownEpisodes = false
			break
		}
	}
	for _, monitored := range overrides {
		if !monitored {
			allKnownEpisodes = false
			break
		}
	}
	if len(episodes) == 0 {
		allKnownEpisodes = allSubmittedSeasons
		for _, monitored := range overrides {
			allKnownEpisodes = allKnownEpisodes && monitored
		}
	}
	if allSubmittedSeasons && allKnownSeasons && allKnownEpisodes {
		seen[requestScope{scope: store.MediaRequestScopeWholeSeries}] = struct{}{}
		return sortedRequestScopes(seen)
	}

	add := func(scope requestScope) {
		seen[scope] = struct{}{}
	}
	for _, season := range req.SeasonMonitors {
		hasExclusion := false
		for key, monitored := range overrides {
			if key.season == season.SeasonNumber && !monitored {
				hasExclusion = true
				break
			}
		}
		if season.Monitored && !hasExclusion {
			add(requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: season.SeasonNumber})
			continue
		}

		knownEpisodes := seasonEpisodes[season.SeasonNumber]
		for _, episodeNumber := range knownEpisodes {
			monitored := season.Monitored
			if override, ok := overrides[episodeKey{season: season.SeasonNumber, episode: episodeNumber}]; ok {
				monitored = override
			}
			if monitored {
				add(requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: season.SeasonNumber, episodeNumber: episodeNumber})
			}
		}
		if season.Monitored && !hasExclusion && len(knownEpisodes) == 0 {
			add(requestScope{scope: store.MediaRequestScopeSeason, seasonNumber: season.SeasonNumber})
		}
	}
	for key, monitored := range overrides {
		if monitored {
			add(requestScope{scope: store.MediaRequestScopeEpisode, seasonNumber: key.season, episodeNumber: key.episode})
		}
	}
	return sortedRequestScopes(seen)
}

func sortedRequestScopes(seen map[requestScope]struct{}) []requestScope {
	scopes := make([]requestScope, 0, len(seen))
	for scope := range seen {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].seasonNumber != scopes[j].seasonNumber {
			return scopes[i].seasonNumber < scopes[j].seasonNumber
		}
		if scopes[i].episodeNumber != scopes[j].episodeNumber {
			return scopes[i].episodeNumber < scopes[j].episodeNumber
		}
		return scopes[i].scope < scopes[j].scope
	})
	return scopes
}

func hasRequestedContent(mediaType string, req AddMediaRequest, scopes []requestScope) bool {
	if mediaType != "series" || req.Monitored == nil || !*req.Monitored {
		return true
	}
	for _, scope := range scopes {
		if scope.scope != store.MediaRequestScopeMedia {
			return true
		}
	}
	return false
}

func applyExistingRequest(st store.Store, item *store.MediaItem, req AddMediaRequest, scopes []requestScope) (*store.MediaProfile, error) {
	var assignedProfile *store.MediaProfile
	if item.MediaProfileID == nil && req.MediaProfileID != nil {
		profileID := *req.MediaProfileID
		var err error
		assignedProfile, err = requestedProfile(st, &profileID)
		if err != nil {
			return nil, err
		}
		item.MediaProfileID = &profileID
	}
	if req.Monitored == nil || !*req.Monitored {
		if assignedProfile == nil {
			return nil, nil
		}
		if err := st.UpdateMediaItem(item); err != nil {
			return nil, fmt.Errorf("updating existing media profile: %w", err)
		}
		return assignedProfile, nil
	}
	item.Monitored = true
	if req.MonitorNewSeasons != nil && *req.MonitorNewSeasons {
		item.MonitorNewSeasons = true
	}
	if err := st.UpdateMediaItem(item); err != nil {
		return nil, fmt.Errorf("updating existing media monitoring: %w", err)
	}

	monitors, err := st.ListSeasonMonitorsByMediaItem(item.ID)
	if err != nil {
		return nil, fmt.Errorf("listing existing season monitors: %w", err)
	}
	bySeason := make(map[int]*store.SeasonMonitor, len(monitors))
	for i := range monitors {
		bySeason[monitors[i].SeasonNumber] = &monitors[i]
	}
	monitorSeason := func(seasonNumber int) error {
		if monitor := bySeason[seasonNumber]; monitor != nil {
			if !monitor.Monitored {
				monitor.Monitored = true
				return st.UpdateSeasonMonitor(monitor)
			}
			return nil
		}
		monitor := &store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: seasonNumber, Monitored: true}
		if err := st.CreateSeasonMonitor(monitor); err != nil {
			return err
		}
		bySeason[seasonNumber] = monitor
		return nil
	}

	for _, scope := range scopes {
		switch scope.scope {
		case store.MediaRequestScopeWholeSeries:
			for _, season := range req.SeasonMonitors {
				if err := monitorSeason(season.SeasonNumber); err != nil {
					return nil, fmt.Errorf("monitoring requested season: %w", err)
				}
				if err := st.DeleteEpisodeMonitorsBySeason(item.ID, season.SeasonNumber); err != nil {
					return nil, fmt.Errorf("clearing requested season overrides: %w", err)
				}
			}
		case store.MediaRequestScopeSeason:
			if err := monitorSeason(scope.seasonNumber); err != nil {
				return nil, fmt.Errorf("monitoring requested season: %w", err)
			}
			if err := st.DeleteEpisodeMonitorsBySeason(item.ID, scope.seasonNumber); err != nil {
				return nil, fmt.Errorf("clearing requested season overrides: %w", err)
			}
		case store.MediaRequestScopeEpisode:
			if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{
				MediaItemID:   item.ID,
				SeasonNumber:  scope.seasonNumber,
				EpisodeNumber: scope.episodeNumber,
				Monitored:     true,
			}); err != nil {
				return nil, fmt.Errorf("monitoring requested episode: %w", err)
			}
		}
	}
	return assignedProfile, nil
}

func requestedProfile(st store.Store, profileID *uint) (*store.MediaProfile, error) {
	if profileID == nil {
		return nil, nil
	}
	profile, err := st.GetMediaProfile(*profileID)
	if err != nil {
		return nil, fmt.Errorf("loading requested media profile: %w", err)
	}
	return profile, nil
}

func createMediaRequests(st store.Store, mediaItemID uint, requesterID *uint, scopes []requestScope) error {
	if requesterID == nil {
		return nil
	}
	for _, scope := range scopes {
		request := &store.MediaRequest{
			MediaItemID: mediaItemID,
			UserID:      requesterID,
			Scope:       scope.scope,
		}
		if scope.scope == store.MediaRequestScopeSeason || scope.scope == store.MediaRequestScopeEpisode {
			seasonNumber := scope.seasonNumber
			request.SeasonNumber = &seasonNumber
		}
		if scope.scope == store.MediaRequestScopeEpisode {
			episodeNumber := scope.episodeNumber
			request.EpisodeNumber = &episodeNumber
		}
		if err := st.CreateMediaRequest(request); err != nil {
			return fmt.Errorf("recording media request: %w", err)
		}
	}
	return nil
}

func appendRequestActivity(st store.Store, item *store.MediaItem, requesterID uint, scopes []requestScope, mediaAdded bool, before, after *mediasync.MonitoringState, assignedProfile *store.MediaProfile) error {
	details := mediasync.BuildMonitoringActivityDetails(before, after)
	details.MediaAdded = &mediaAdded
	targets := make([]store.MediaActivityTarget, 0, min(len(scopes), store.MediaActivityMaxDetails))
	for _, scope := range scopes {
		if len(targets) == store.MediaActivityMaxDetails {
			details.Truncated = true
			if len(scopes) > details.Total {
				details.Total = len(scopes)
			}
			break
		}
		target := store.MediaActivityTarget{Scope: scope.scope}
		if scope.scope == store.MediaRequestScopeSeason || scope.scope == store.MediaRequestScopeEpisode {
			seasonNumber := scope.seasonNumber
			target.SeasonNumber = &seasonNumber
		}
		if scope.scope == store.MediaRequestScopeEpisode {
			episodeNumber := scope.episodeNumber
			target.EpisodeNumber = &episodeNumber
		}
		targets = append(targets, target)
	}
	if len(targets) == 1 {
		details.Target = &targets[0]
	} else {
		details.Targets = targets
	}

	operationID, err := store.NewMediaActivityOperationID()
	if err != nil {
		return err
	}
	activity, err := store.NewUserMediaActivity(item, requesterID, store.MediaActivityActionRequestMade, operationID, store.MediaActivityVisibilityShared, details)
	if err != nil {
		return fmt.Errorf("creating request activity: %w", err)
	}
	if err := st.AppendMediaActivity(activity); err != nil {
		return fmt.Errorf("recording request activity: %w", err)
	}
	if assignedProfile != nil {
		label := requestedProfileActivityLabel(assignedProfile)
		settingsDetails := store.MediaActivityDetails{
			FieldChanges: []store.MediaActivityFieldChange{{Field: "media_profile", After: &label}},
			Total:        1,
		}
		settingsActivity, err := store.NewUserMediaActivity(item, requesterID, store.MediaActivityActionSettingsChanged, operationID, store.MediaActivityVisibilityShared, settingsDetails)
		if err != nil {
			return fmt.Errorf("creating request profile activity: %w", err)
		}
		if err := st.AppendMediaActivity(settingsActivity); err != nil {
			return fmt.Errorf("recording request profile activity: %w", err)
		}
	}
	return nil
}

func requestedProfileActivityLabel(profile *store.MediaProfile) string {
	suffix := fmt.Sprintf(" (ID %d)", profile.ID)
	name := store.BoundMediaActivityText(profile.Name, store.MediaActivityMaxTitleBytes-len(suffix))
	return name + suffix
}

type matchActor struct {
	userID    uint
	component string
}

type matchVersion struct {
	item     *store.MediaItem
	metadata *store.MediaMetadata
}

type episodeKey struct {
	season  int
	episode int
}

func (s *Service) captureMatchVersion(mediaItemID uint) (*matchVersion, error) {
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		return nil, err
	}
	if item.DeletionPending {
		return nil, store.ErrMediaDeletionPending
	}
	meta, err := s.store.GetMediaMetadataByMediaItem(mediaItemID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if errors.Is(err, store.ErrNotFound) {
		meta = nil
	}
	return &matchVersion{item: item, metadata: meta}, nil
}

func validateMatchActor(st store.Store, actor matchActor) error {
	if actor.userID == 0 {
		if actor.component == "" {
			return store.ErrInvalidMediaActivity
		}
		return nil
	}
	if _, err := st.GetUser(actor.userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrActivityActorNotFound
		}
		return err
	}
	return nil
}

func sameMatchItemVersion(expected, current *store.MediaItem) bool {
	return expected != nil && current != nil && expected.ID == current.ID &&
		expected.LibraryID == current.LibraryID && expected.MediaType == current.MediaType &&
		expected.Source == current.Source && expected.UpdatedAt.Equal(current.UpdatedAt)
}

func sameMatchMetadataVersion(expected, current *store.MediaMetadata) bool {
	if expected == nil || current == nil {
		return expected == current
	}
	return expected.ID == current.ID && expected.MediaItemID == current.MediaItemID &&
		expected.Source == current.Source && expected.ExternalID == current.ExternalID &&
		expected.UpdatedAt.Equal(current.UpdatedAt)
}

func (s *Service) replaceMatch(expected *matchVersion, candidate *store.MediaMetadata, episodes []episodeData, actor matchActor) (*store.MediaItem, *store.MediaMetadata, error) {
	operationID, err := store.NewMediaActivityOperationID()
	if err != nil {
		return nil, nil, err
	}

	var resultItem *store.MediaItem
	var resultMeta *store.MediaMetadata
	activityAdded := false
	s.posterMu.Lock()
	defer s.posterMu.Unlock()
	err = s.store.WithTx(func(tx store.Store) error {
		if err := validateMatchActor(tx, actor); err != nil {
			return err
		}
		currentItem, err := tx.GetMediaItem(expected.item.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrStaleMatchCandidate
			}
			return err
		}
		if currentItem.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		if !sameMatchItemVersion(expected.item, currentItem) {
			return ErrStaleMatchCandidate
		}
		currentMeta, err := tx.GetMediaMetadataByMediaItem(currentItem.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if errors.Is(err, store.ErrNotFound) {
			currentMeta = nil
		}
		if !sameMatchMetadataVersion(expected.metadata, currentMeta) {
			return ErrStaleMatchCandidate
		}
		currentEpisodes, err := tx.ListEpisodesByMediaItem(currentItem.ID)
		if err != nil {
			return fmt.Errorf("listing current episodes: %w", err)
		}

		persistedMeta := *candidate
		persistedMeta.ID = 0
		persistedMeta.MediaItemID = currentItem.ID
		persistedMeta.CreatedAt = time.Time{}
		persistedMeta.UpdatedAt = time.Time{}
		if actor.component != "" && sameTrackedMatchSemantics(currentMeta, &persistedMeta, currentEpisodes, episodes) {
			itemCopy, metaCopy := *currentItem, *currentMeta
			resultItem, resultMeta = &itemCopy, &metaCopy
			return nil
		}
		details := buildMatchActivityDetails(currentMeta, &persistedMeta, currentEpisodes, episodes, actor.component != "")
		if currentMeta != nil {
			if err := tx.DeleteMediaMetadataByMediaItem(currentItem.ID); err != nil {
				return fmt.Errorf("deleting previous metadata: %w", err)
			}
		}
		if err := tx.CreateMediaMetadata(&persistedMeta); err != nil {
			return fmt.Errorf("saving metadata: %w", err)
		}
		sameProvider := currentMeta != nil && currentMeta.Source == persistedMeta.Source && currentMeta.ExternalID == persistedMeta.ExternalID
		if err := replaceMatchEpisodes(tx, currentItem.ID, currentEpisodes, episodes, sameProvider); err != nil {
			return err
		}
		if currentItem.Source != "request" {
			currentItem.Status = "available"
		}
		if err := tx.UpdateMediaItem(currentItem); err != nil {
			return fmt.Errorf("updating matched item: %w", err)
		}

		var activity *store.MediaActivity
		if actor.userID != 0 {
			activity, err = store.NewUserMediaActivity(currentItem, actor.userID, store.MediaActivityActionMatchChanged, operationID, store.MediaActivityVisibilityShared, details)
		} else {
			activity, err = store.NewSystemMediaActivity(currentItem, actor.component, store.MediaActivityActionMatchChanged, operationID, details)
		}
		if err != nil {
			return fmt.Errorf("creating match activity: %w", err)
		}
		if err := tx.AppendMediaActivity(activity); err != nil {
			return fmt.Errorf("recording match activity: %w", err)
		}
		activityAdded = true
		itemCopy, metaCopy := *currentItem, persistedMeta
		resultItem, resultMeta = &itemCopy, &metaCopy
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if !activityAdded {
		return resultItem, resultMeta, nil
	}

	s.publishActivityAdded(resultItem.ID)
	s.downloadPosterForCommitted(resultItem.ID, resultMeta, operationID)
	s.afterMatch(resultItem, resultMeta, resultMeta.Source, resultMeta.ExternalID)
	if fresh, err := s.store.GetMediaItem(resultItem.ID); err == nil {
		resultItem = fresh
	}
	if fresh, err := s.store.GetMediaMetadataByMediaItem(resultItem.ID); err == nil {
		resultMeta = fresh
	}
	return resultItem, resultMeta, nil
}

func sameTrackedMatchSemantics(oldMeta, newMeta *store.MediaMetadata, oldEpisodes []store.Episode, newEpisodes []episodeData) bool {
	if oldMeta == nil || newMeta == nil || oldMeta.Source != newMeta.Source || oldMeta.ExternalID != newMeta.ExternalID ||
		strings.TrimSpace(oldMeta.Title) != strings.TrimSpace(newMeta.Title) ||
		strings.TrimSpace(oldMeta.Status) != strings.TrimSpace(newMeta.Status) ||
		!sameOptionalInt(oldMeta.Year, newMeta.Year) || !sameOptionalInt(oldMeta.Seasons, newMeta.Seasons) || !sameOptionalInt(oldMeta.Runtime, newMeta.Runtime) {
		return false
	}
	added, removed := changedEpisodeKeys(oldEpisodes, newEpisodes)
	return len(added) == 0 && len(removed) == 0
}

func sameOptionalInt(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func buildMatchActivityDetails(oldMeta, newMeta *store.MediaMetadata, oldEpisodes []store.Episode, newEpisodes []episodeData, automatic bool) store.MediaActivityDetails {
	added, removed := changedEpisodeKeys(oldEpisodes, newEpisodes)
	changedTargets := append(append([]episodeKey(nil), added...), removed...)
	sortEpisodeKeys(changedTargets)
	fieldChanges := matchFieldChanges(oldMeta, newMeta)
	details := store.MediaActivityDetails{
		OldProvider:  providerIdentity(oldMeta),
		NewProvider:  providerIdentity(newMeta),
		Automatic:    &automatic,
		Added:        len(added),
		Removed:      len(removed),
		FieldChanges: fieldChanges,
		Total:        len(added) + len(removed) + len(fieldChanges),
	}
	details.Targets, details.Truncated = boundedEpisodeTargets(changedTargets, false)
	return details
}

func matchFieldChanges(oldMeta, newMeta *store.MediaMetadata) []store.MediaActivityFieldChange {
	changes := make([]store.MediaActivityFieldChange, 0, 5)
	appendChange := func(field string, before, after *string) {
		if sameActivityString(before, after) {
			return
		}
		changes = append(changes, store.MediaActivityFieldChange{Field: field, Before: before, After: after})
	}
	appendChange("title", metadataString(oldMeta, func(meta *store.MediaMetadata) string { return meta.Title }), metadataString(newMeta, func(meta *store.MediaMetadata) string { return meta.Title }))
	appendChange("year", metadataInt(oldMeta, func(meta *store.MediaMetadata) *int { return meta.Year }), metadataInt(newMeta, func(meta *store.MediaMetadata) *int { return meta.Year }))
	appendChange("status", metadataString(oldMeta, func(meta *store.MediaMetadata) string { return meta.Status }), metadataString(newMeta, func(meta *store.MediaMetadata) string { return meta.Status }))
	appendChange("season_count", metadataInt(oldMeta, func(meta *store.MediaMetadata) *int { return meta.Seasons }), metadataInt(newMeta, func(meta *store.MediaMetadata) *int { return meta.Seasons }))
	appendChange("runtime", metadataInt(oldMeta, func(meta *store.MediaMetadata) *int { return meta.Runtime }), metadataInt(newMeta, func(meta *store.MediaMetadata) *int { return meta.Runtime }))
	return changes
}

func metadataString(meta *store.MediaMetadata, value func(*store.MediaMetadata) string) *string {
	if meta == nil {
		return nil
	}
	bounded := store.BoundMediaActivityText(strings.TrimSpace(value(meta)), store.MediaActivityMaxTitleBytes)
	if bounded == "" {
		return nil
	}
	return &bounded
}

func metadataInt(meta *store.MediaMetadata, value func(*store.MediaMetadata) *int) *string {
	if meta == nil || value(meta) == nil {
		return nil
	}
	formatted := strconv.Itoa(*value(meta))
	return &formatted
}

func sameActivityString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func providerIdentity(meta *store.MediaMetadata) *store.MediaActivityProviderIdentity {
	if meta == nil {
		return nil
	}
	return &store.MediaActivityProviderIdentity{
		Source:     meta.Source,
		ExternalID: meta.ExternalID,
		Title:      store.BoundMediaActivityText(meta.Title, store.MediaActivityMaxTitleBytes),
	}
}

func changedEpisodeKeys(oldEpisodes []store.Episode, newEpisodes []episodeData) (added, removed []episodeKey) {
	oldSet := make(map[episodeKey]struct{}, len(oldEpisodes))
	newSet := make(map[episodeKey]struct{}, len(newEpisodes))
	for _, episode := range oldEpisodes {
		oldSet[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}] = struct{}{}
	}
	for _, episode := range newEpisodes {
		newSet[episodeKey{season: episode.seasonNumber, episode: episode.episodeNumber}] = struct{}{}
	}
	for key := range newSet {
		if _, ok := oldSet[key]; !ok {
			added = append(added, key)
		}
	}
	for key := range oldSet {
		if _, ok := newSet[key]; !ok {
			removed = append(removed, key)
		}
	}
	sortEpisodeKeys(added)
	sortEpisodeKeys(removed)
	return added, removed
}

func episodeKeysFromStored(episodes []store.Episode) []episodeKey {
	keys := make([]episodeKey, 0, len(episodes))
	for _, episode := range episodes {
		keys = append(keys, episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber})
	}
	sortEpisodeKeys(keys)
	return keys
}

func sortEpisodeKeys(keys []episodeKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].season != keys[j].season {
			return keys[i].season < keys[j].season
		}
		return keys[i].episode < keys[j].episode
	})
}

func boundedEpisodeTargets(keys []episodeKey, alreadyTruncated bool) ([]store.MediaActivityTarget, bool) {
	limit := min(len(keys), store.MediaActivityMaxDetails)
	targets := make([]store.MediaActivityTarget, limit)
	for i := 0; i < limit; i++ {
		season, episode := keys[i].season, keys[i].episode
		targets[i] = store.MediaActivityTarget{Scope: store.MediaActivityScopeEpisode, SeasonNumber: &season, EpisodeNumber: &episode}
	}
	return targets, alreadyTruncated || len(keys) > limit
}

// fetchMatchData fetches full metadata and, for series, the episode lists for
// all seasons from the external source into in-memory structures. It performs
// NO database writes and NO other side effects, so it is safe to call BEFORE
// opening a database transaction — this keeps all network I/O out of the
// SQLite write lock. The returned metadata has no MediaItemID set;
// persistMatch assigns it.
func (s *Service) fetchMatchData(source, apiKey, mediaType string, externalID int, confidence float64) (*store.MediaMetadata, []episodeData, error) {
	meta := &store.MediaMetadata{
		Source:     source,
		ExternalID: externalID,
		Confidence: confidence,
		MatchedAt:  time.Now(),
	}

	switch source {
	case "tmdb":
		if err := s.fetchTMDBDetails(apiKey, mediaType, externalID, meta); err != nil {
			return nil, nil, err
		}
	case "tvdb":
		if err := s.fetchTVDBDetails(apiKey, externalID, meta); err != nil {
			return nil, nil, err
		}
	}

	if mediaType == "series" && meta.Seasons != nil && *meta.Seasons > 0 {
		episodes, err := s.fetchEpisodesForSeasons(source, apiKey, externalID, *meta.Seasons)
		if err != nil {
			return nil, nil, err
		}
		return meta, episodes, nil
	}

	return meta, nil, nil
}

// persistMatch writes the already-fetched metadata and episodes for item. It
// performs ONLY database writes (no network I/O), so it is safe to call inside
// a short transaction. It binds meta to item.ID and, for series, replaces the
// item's episode records with the fetched set (mirroring the previous
// fetch-and-store behavior).
func (s *Service) persistMatch(item *store.MediaItem, meta *store.MediaMetadata, episodes []episodeData) error {
	meta.MediaItemID = item.ID
	if err := s.store.CreateMediaMetadata(meta); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}

	if item.Source != "request" {
		item.Status = "available"
	}
	if err := s.store.UpdateMediaItem(item); err != nil {
		return err
	}

	// For series with season info, replace episode records with the fetched set.
	if item.MediaType == "series" && meta.Seasons != nil && *meta.Seasons > 0 {
		if err := s.store.DeleteEpisodesByMediaItem(item.ID); err != nil {
			return fmt.Errorf("deleting existing episodes: %w", err)
		}
		for _, ep := range episodes {
			if err := s.store.CreateEpisode(newEpisodeFromData(item.ID, ep)); err != nil {
				return fmt.Errorf("creating episode S%02dE%02d: %w", ep.seasonNumber, ep.episodeNumber, err)
			}
		}
	}

	return nil
}

// afterMatch runs the post-persist side effects: recalculating the item's
// status (now that episodes are stored) and publishing the matched event. It
// performs NO network I/O. Callers that persisted inside a transaction MUST
// invoke this AFTER the transaction commits, using a service bound to the
// top-level store, so the recalculator sees the committed item instead of
// silently no-op'ing on a "not found" from an uncommitted row.
func (s *Service) afterMatch(item *store.MediaItem, meta *store.MediaMetadata, source string, externalID int) {
	// Recalculate status now that episodes are stored — a series with only
	// some aired episodes on disk should be "partial", not "available".
	if s.statusRecalc != nil {
		if err := s.statusRecalc.RecalcMediaItemStatus(item.ID); err != nil {
			slog.Warn("applyMatch: status recalc failed", "media_item_id", item.ID, "error", err)
		}
	}

	// Notify frontend so it can refresh the media item.
	if s.bus != nil {
		slog.Info("matching: match applied", "media_item_id", item.ID, "title", meta.Title, "source", source, "external_id", externalID)
		s.bus.Publish(eventbus.MediaItemMatched, eventbus.MediaItemPayload{
			MediaItemID: item.ID,
			LibraryID:   item.LibraryID,
			Title:       item.Title,
		})
	}
}

func (s *Service) publishActivityAdded(mediaItemID uint) {
	if s.bus != nil {
		s.bus.Publish(eventbus.MediaActivityAdded, eventbus.MediaActivityPayload{MediaItemID: mediaItemID})
	}
}

// DownloadPoster fetches the poster image for a media item by loading its
// stored metadata first. This is intentionally separate from applyMatch so it
// can run outside a DB transaction.
func (s *Service) DownloadPoster(itemID uint) {
	s.posterMu.Lock()
	defer s.posterMu.Unlock()
	item, err := s.store.GetMediaItem(itemID)
	if err != nil || item.DeletionPending {
		return
	}
	meta, err := s.store.GetMediaMetadataByMediaItem(itemID)
	if err != nil || meta == nil {
		return
	}
	s.downloadPosterFor(itemID, meta)
	if current, err := s.store.GetMediaItem(itemID); err != nil || current.DeletionPending {
		_ = os.Remove(filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", itemID)))
	}
}

// downloadPosterFor fetches and atomically installs the poster described by
// already-persisted metadata.
func (s *Service) downloadPosterFor(itemID uint, meta *store.MediaMetadata) {
	posterURL := s.posterURL(meta.Source, meta)
	if posterURL == "" {
		return
	}

	dest := filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", itemID))
	if err := downloadPoster(s.httpClient, posterURL, dest); err != nil {
		slog.Warn("poster download failed", "item_id", itemID, "error", err)
		return
	}
}

// downloadPosterForCommitted stages provider I/O away from the database and
// installs the image only if the committed metadata candidate is still current.
func (s *Service) downloadPosterForCommitted(itemID uint, expected *store.MediaMetadata, operationID string) {
	if !s.posterCandidateCurrent(itemID, expected) {
		return
	}
	posterURL := s.posterURL(expected.Source, expected)
	if posterURL == "" {
		s.removePosterForCommitted(itemID, expected)
		return
	}
	staged := filepath.Join(s.posterDir, fmt.Sprintf(".poster-%d-%s.jpg", itemID, operationID))
	defer func() { _ = os.Remove(staged) }()
	if err := downloadPoster(s.httpClient, posterURL, staged); err != nil {
		slog.Warn("poster download failed", "item_id", itemID, "error", err)
		s.removePosterForCommitted(itemID, expected)
		return
	}
	if !s.posterCandidateCurrent(itemID, expected) {
		return
	}
	dest := filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", itemID))
	if err := os.Rename(staged, dest); err != nil {
		slog.Warn("poster install failed", "item_id", itemID, "error", err)
		s.removePosterForCommitted(itemID, expected)
		return
	}
	if !s.posterCandidateCurrent(itemID, expected) {
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("stale poster cleanup failed", "item_id", itemID, "error", err)
		}
	}
}

func (s *Service) removePosterForCommitted(itemID uint, expected *store.MediaMetadata) {
	if !s.posterCandidateCurrent(itemID, expected) {
		return
	}
	dest := filepath.Join(s.posterDir, fmt.Sprintf("%d.jpg", itemID))
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("old poster cleanup failed", "item_id", itemID, "error", err)
	}
}

func (s *Service) posterCandidateCurrent(itemID uint, expected *store.MediaMetadata) bool {
	item, err := s.store.GetMediaItem(itemID)
	if err != nil || item.DeletionPending {
		return false
	}
	current, err := s.store.GetMediaMetadataByMediaItem(itemID)
	return err == nil && sameMatchMetadataVersion(expected, current)
}

type episodeData struct {
	seasonNumber  int
	episodeNumber int
	title         string
	overview      string
	airDate       string
	runtime       int
}

// SeasonEpisodeData holds a season's episode list fetched from an external source (no DB).
type SeasonEpisodeData struct {
	SeasonNumber  int
	TotalEpisodes int
	Episodes      []EpisodeInfo
}

// EpisodeInfo holds episode data from an external source.
type EpisodeInfo struct {
	SeasonNumber  int
	EpisodeNumber int
	Title         string
	AirDate       string
	Runtime       int
}

// FetchExternalEpisodes retrieves episode data from TMDB/TVDB without persisting anything.
func (s *Service) FetchExternalEpisodes(source string, externalID, seasonCount int) ([]SeasonEpisodeData, error) {
	apiKey, err := s.resolveAPIKey(source)
	if err != nil {
		return nil, fmt.Errorf("no API key configured for %s", source)
	}

	var result []SeasonEpisodeData
	for season := 1; season <= seasonCount; season++ {
		episodes, err := s.fetchEpisodesFromSource(source, apiKey, externalID, season)
		if err != nil {
			slog.Warn("failed to fetch external episodes", "season", season, "source", source, "error", err)
			continue
		}
		infos := make([]EpisodeInfo, len(episodes))
		for i, ep := range episodes {
			infos[i] = EpisodeInfo{
				SeasonNumber:  ep.seasonNumber,
				EpisodeNumber: ep.episodeNumber,
				Title:         ep.title,
				AirDate:       ep.airDate,
				Runtime:       ep.runtime,
			}
		}
		result = append(result, SeasonEpisodeData{
			SeasonNumber:  season,
			TotalEpisodes: len(episodes),
			Episodes:      infos,
		})
	}
	return result, nil
}

// fetchEpisodesForSeasons requires every provider window so a replacement can
// never turn a transient season failure into catalog deletion.
func (s *Service) fetchEpisodesForSeasons(source, apiKey string, externalID, seasonCount int) ([]episodeData, error) {
	var all []episodeData
	for season := 1; season <= seasonCount; season++ {
		episodes, err := s.fetchEpisodesFromSource(source, apiKey, externalID, season)
		if err != nil {
			return nil, fmt.Errorf("fetching %s season %d episodes: %w", source, season, err)
		}
		all = append(all, episodes...)
	}
	return all, nil
}

func (s *Service) fetchEpisodesFromSource(source, apiKey string, externalID, season int) ([]episodeData, error) {
	switch source {
	case "tmdb":
		client := s.cachedTMDB(apiKey)
		details, err := client.GetTVSeason(externalID, season)
		if err != nil {
			return nil, err
		}
		episodes := make([]episodeData, len(details.Episodes))
		for i, ep := range details.Episodes {
			episodes[i] = episodeData{
				seasonNumber:  ep.SeasonNumber,
				episodeNumber: ep.EpisodeNumber,
				title:         ep.Name,
				overview:      ep.Overview,
				airDate:       ep.AirDate,
				runtime:       ep.Runtime,
			}
		}
		return episodes, nil
	case "tvdb":
		client := s.cachedTVDB(apiKey)
		entries, err := client.GetSeriesEpisodes(externalID, season)
		if err != nil {
			return nil, err
		}
		episodes := make([]episodeData, len(entries))
		for i, ep := range entries {
			episodes[i] = episodeData{
				seasonNumber:  ep.SeasonNumber,
				episodeNumber: ep.Number,
				title:         ep.Name,
				overview:      ep.Overview,
				airDate:       ep.Aired,
				runtime:       ep.Runtime,
			}
		}
		return episodes, nil
	default:
		return nil, fmt.Errorf("unknown source: %s", source)
	}
}

func (s *Service) searchSource(query, mediaType string, year *int, source, apiKey string) ([]Candidate, error) {
	switch source {
	case "tmdb":
		return s.searchTMDB(apiKey, query, mediaType, year)
	case "tvdb":
		return s.searchTVDB(apiKey, query, year)
	default:
		return nil, fmt.Errorf("unknown source: %s", source)
	}
}

func (s *Service) searchTMDB(apiKey, query, mediaType string, year *int) ([]Candidate, error) {
	client := s.cachedTMDB(apiKey)

	var candidates []Candidate

	if mediaType == "movie" {
		results, err := client.SearchMovie(query, year)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			c := Candidate{
				Source:     "tmdb",
				ExternalID: r.ID,
				Title:      r.Title,
				Overview:   r.Overview,
			}
			if r.PosterPath != "" {
				c.PosterURL = tmdbPosterBase + r.PosterPath
			}
			if y := dateutil.ParseYear(r.ReleaseDate); y != nil {
				c.Year = y
			}
			c.Confidence = Score(query, year, c.Title, c.Year)
			candidates = append(candidates, c)
		}
	} else {
		results, err := client.SearchTV(query, year)
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			c := Candidate{
				Source:     "tmdb",
				ExternalID: r.ID,
				Title:      r.Name,
				Overview:   r.Overview,
			}
			if r.PosterPath != "" {
				c.PosterURL = tmdbPosterBase + r.PosterPath
			}
			if y := dateutil.ParseYear(r.FirstAirDate); y != nil {
				c.Year = y
			}
			c.Confidence = Score(query, year, c.Title, c.Year)
			candidates = append(candidates, c)
		}
	}

	return candidates, nil
}

func (s *Service) searchTVDB(apiKey, query string, year *int) ([]Candidate, error) {
	client := s.cachedTVDB(apiKey)
	results, err := client.SearchSeries(query, year)
	if err != nil {
		return nil, err
	}

	var candidates []Candidate
	for _, r := range results {
		c := Candidate{
			Source:     "tvdb",
			ExternalID: r.ID(),
			Title:      r.Name,
			Overview:   r.Overview,
			PosterURL:  r.ImageURL,
		}
		if y := dateutil.ParseYear(r.FirstAirDate); y != nil {
			c.Year = y
		}
		c.Confidence = Score(query, year, c.Title, c.Year)
		candidates = append(candidates, c)
	}

	return candidates, nil
}

func (s *Service) fetchTMDBDetails(apiKey, mediaType string, externalID int, meta *store.MediaMetadata) error {
	client := s.cachedTMDB(apiKey)

	if mediaType == "movie" {
		details, err := client.GetMovie(externalID)
		if err != nil {
			return fmt.Errorf("fetching TMDB movie: %w", err)
		}
		meta.Title = details.Title
		meta.Overview = details.Overview
		meta.PosterPath = details.PosterPath
		meta.Status = details.Status
		meta.ImdbID = details.ImdbID
		if details.Runtime > 0 {
			rt := details.Runtime
			meta.Runtime = &rt
		}
		if y := dateutil.ParseYear(details.ReleaseDate); y != nil {
			meta.Year = y
		}
		meta.ReleaseDate = details.ReleaseDate
		meta.Genres = genresToJSON(details.Genres)
		meta.Credits = tmdbCreditsToJSON(details.Credits)
		meta.TrailerURL = tmdb.BestTrailerURL(details.Videos)
		meta.ContentRatings = contentRatingsToJSON(tmdb.MovieCertifications(details.ReleaseDates))
	} else {
		details, err := client.GetTV(externalID)
		if err != nil {
			return fmt.Errorf("fetching TMDB TV: %w", err)
		}
		meta.Title = details.Name
		meta.Overview = details.Overview
		meta.PosterPath = details.PosterPath
		meta.Status = details.Status
		if details.ExternalIds != nil {
			meta.ImdbID = details.ExternalIds.ImdbID
		}
		if details.NumberOfSeasons > 0 {
			ns := details.NumberOfSeasons
			meta.Seasons = &ns
		}
		if y := dateutil.ParseYear(details.FirstAirDate); y != nil {
			meta.Year = y
		}
		meta.ReleaseDate = details.FirstAirDate
		meta.Genres = genresToJSON(details.Genres)
		meta.Credits = tmdbCreditsToJSON(details.Credits)
		meta.TrailerURL = tmdb.BestTrailerURL(details.Videos)
		meta.ContentRatings = contentRatingsToJSON(tmdb.TVCertifications(details.ContentRatings))
	}
	return nil
}

func (s *Service) fetchTVDBDetails(apiKey string, externalID int, meta *store.MediaMetadata) error {
	client := s.cachedTVDB(apiKey)
	details, err := client.GetSeries(externalID)
	if err != nil {
		return fmt.Errorf("fetching TVDB series: %w", err)
	}
	meta.Title = details.Name
	meta.Overview = details.Overview
	meta.PosterPath = details.Image
	meta.Status = details.Status.Name
	meta.ImdbID = details.ImdbID()
	if len(details.Seasons) > 0 {
		ns := details.MaxSeasonNumber()
		if ns > 0 {
			meta.Seasons = &ns
		}
	}
	if y := dateutil.ParseYear(details.FirstAired); y != nil {
		meta.Year = y
	}
	meta.ReleaseDate = details.FirstAired
	meta.Credits = tvdbCharactersToJSON(details.Characters)
	meta.ContentRatings = tvdbContentRatingsToJSON(details.ContentRatings)
	return nil
}

func (s *Service) posterURL(source string, meta *store.MediaMetadata) string {
	if meta.PosterPath == "" {
		return ""
	}
	if source == "tmdb" {
		if strings.HasPrefix(meta.PosterPath, "/") {
			return tmdbPosterBase + meta.PosterPath
		}
		return ""
	}
	// TVDB: PosterPath is already a full URL
	return meta.PosterPath
}

func (s *Service) resolveAPIKey(source string) (string, error) {
	switch source {
	case "tmdb":
		return s.settings.Get(settings.KeyTMDBApiKey)
	case "tvdb":
		return s.settings.Get(settings.KeyTVDBApiKey)
	default:
		return "", fmt.Errorf("unknown source: %s", source)
	}
}

// TMDBClient returns a cached TMDB client configured with the current API key,
// or nil if no key is configured.
func (s *Service) TMDBClient() *tmdb.Client {
	apiKey, err := s.settings.Get(settings.KeyTMDBApiKey)
	if err != nil || apiKey == "" {
		return nil
	}
	return s.cachedTMDB(apiKey)
}

type MetadataRefreshResult struct {
	Changed       bool
	ActivityAdded bool
}

type refreshWindow struct {
	episodes []episodeData
}

// RefreshSeriesMetadata fetches provider candidates without a write lock, then
// atomically applies the semantic status/season/episode diff and future-season
// policy against the same fresh transaction snapshot.
func (s *Service) RefreshSeriesMetadata(item *store.MediaItem, meta *store.MediaMetadata) (MetadataRefreshResult, error) {
	var result MetadataRefreshResult
	if item == nil || meta == nil {
		return result, ErrStaleRefreshCandidate
	}
	if item.DeletionPending {
		return result, store.ErrMediaDeletionPending
	}
	expectedItem, expectedMeta := *item, *meta
	apiKey, err := s.resolveAPIKey(expectedMeta.Source)
	if err != nil {
		return result, err
	}

	var providerSeasons int
	var providerStatus string
	switch expectedMeta.Source {
	case "tmdb":
		details, err := s.cachedTMDB(apiKey).GetTV(expectedMeta.ExternalID)
		if err != nil {
			return result, fmt.Errorf("fetching TMDB TV %d: %w", expectedMeta.ExternalID, err)
		}
		providerSeasons, providerStatus = details.NumberOfSeasons, details.Status
	case "tvdb":
		details, err := s.cachedTVDB(apiKey).GetSeries(expectedMeta.ExternalID)
		if err != nil {
			return result, fmt.Errorf("fetching TVDB series %d: %w", expectedMeta.ExternalID, err)
		}
		providerSeasons, providerStatus = details.MaxSeasonNumber(), details.Status.Name
	default:
		return result, fmt.Errorf("unknown source: %s", expectedMeta.Source)
	}

	oldSeasons := metadataSeasonCount(&expectedMeta)
	windows := refreshSeasonWindows(oldSeasons, providerSeasons)
	successful := make([]refreshWindow, 0, len(windows))
	failedWindows := 0
	for _, season := range windows {
		episodes, err := s.fetchEpisodesFromSource(expectedMeta.Source, apiKey, expectedMeta.ExternalID, season)
		if err != nil {
			failedWindows++
			slog.Warn("metadata refresh: failed provider window", "item_id", expectedItem.ID, "season", season, "source", expectedMeta.Source, "error", err)
			continue
		}
		successful = append(successful, refreshWindow{episodes: episodes})
	}

	err = s.store.WithTx(func(tx store.Store) error {
		currentItem, err := tx.GetMediaItem(expectedItem.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrStaleRefreshCandidate
			}
			return err
		}
		if currentItem.DeletionPending {
			return store.ErrMediaDeletionPending
		}
		currentMeta, err := tx.GetMediaMetadataByMediaItem(expectedItem.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrStaleRefreshCandidate
			}
			return err
		}
		if !sameMatchItemVersion(&expectedItem, currentItem) || !sameMatchMetadataVersion(&expectedMeta, currentMeta) {
			return ErrStaleRefreshCandidate
		}
		storedEpisodes, err := tx.ListEpisodesByMediaItem(currentItem.ID)
		if err != nil {
			return fmt.Errorf("listing episodes: %w", err)
		}
		seasonMonitors, err := tx.ListSeasonMonitorsByMediaItem(currentItem.ID)
		if err != nil {
			return fmt.Errorf("listing season monitors: %w", err)
		}

		currentSeasons := metadataSeasonCount(currentMeta)
		newSeasons := providerSeasons
		if newSeasons <= 0 {
			newSeasons = currentSeasons
		}
		currentStatus := strings.TrimSpace(currentMeta.Status)
		newStatus := strings.TrimSpace(providerStatus)
		if newStatus == "" {
			newStatus = currentStatus
		}

		fieldChanges := make([]store.MediaActivityFieldChange, 0, 2)
		if currentStatus != newStatus {
			before, after := activityString(currentStatus), activityString(newStatus)
			fieldChanges = append(fieldChanges, store.MediaActivityFieldChange{Field: "status", Before: before, After: after})
			currentMeta.Status = newStatus
		}
		if currentSeasons != newSeasons {
			before, after := strconv.Itoa(currentSeasons), strconv.Itoa(newSeasons)
			fieldChanges = append(fieldChanges, store.MediaActivityFieldChange{Field: "season_count", Before: &before, After: &after})
			currentMeta.Seasons = &newSeasons
		}

		existing := make(map[episodeKey]struct{}, len(storedEpisodes))
		for _, episode := range storedEpisodes {
			existing[episodeKey{season: episode.SeasonNumber, episode: episode.EpisodeNumber}] = struct{}{}
		}
		additions := make([]episodeData, 0)
		for _, window := range successful {
			for _, episode := range window.episodes {
				key := episodeKey{season: episode.seasonNumber, episode: episode.episodeNumber}
				if _, ok := existing[key]; ok {
					continue
				}
				existing[key] = struct{}{}
				additions = append(additions, episode)
			}
		}
		sort.Slice(additions, func(i, j int) bool {
			if additions[i].seasonNumber != additions[j].seasonNumber {
				return additions[i].seasonNumber < additions[j].seasonNumber
			}
			return additions[i].episodeNumber < additions[j].episodeNumber
		})
		policyChanges, err := applyFutureSeasonPolicy(tx, currentItem, seasonMonitors, currentSeasons, newSeasons)
		if err != nil {
			return err
		}
		if len(fieldChanges) == 0 && len(additions) == 0 && len(policyChanges) == 0 {
			return nil
		}

		// Bump both versions for every semantic refresh change. Metadata protects
		// match candidates; the item version invalidates saved monitor decisions.
		if err := tx.UpdateMediaMetadata(currentMeta); err != nil {
			return fmt.Errorf("updating metadata: %w", err)
		}
		for _, episode := range additions {
			if err := tx.CreateEpisode(newEpisodeFromData(currentItem.ID, episode)); err != nil {
				return fmt.Errorf("creating episode S%02dE%02d: %w", episode.seasonNumber, episode.episodeNumber, err)
			}
		}
		if err := tx.UpdateMediaItem(currentItem); err != nil {
			return fmt.Errorf("touching refreshed media item: %w", err)
		}
		operationID, err := store.NewMediaActivityOperationID()
		if err != nil {
			return err
		}
		targetKeys := make([]episodeKey, len(additions))
		for i, episode := range additions {
			targetKeys[i] = episodeKey{season: episode.seasonNumber, episode: episode.episodeNumber}
		}
		targets, truncated := boundedEpisodeTargets(targetKeys, false)
		metadataDetails := store.MediaActivityDetails{
			FieldChanges:              fieldChanges,
			Targets:                   targets,
			Added:                     len(additions),
			Total:                     len(fieldChanges) + len(additions),
			Truncated:                 truncated,
			Partial:                   failedWindows > 0,
			FailedProviderWindows:     failedWindows,
			SuccessfulProviderWindows: len(successful),
		}
		activity, err := store.NewSystemMediaActivity(currentItem, "metarefresh", store.MediaActivityActionMetadataChanged, operationID, metadataDetails)
		if err != nil {
			return fmt.Errorf("creating metadata activity: %w", err)
		}
		if err := tx.AppendMediaActivity(activity); err != nil {
			return fmt.Errorf("recording metadata activity: %w", err)
		}
		if len(policyChanges) > 0 {
			policyDetails := store.MediaActivityDetails{
				Reason:            "future_season_policy",
				MonitoringChanges: policyChanges[:min(len(policyChanges), store.MediaActivityMaxDetails)],
				Total:             len(policyChanges),
				Truncated:         len(policyChanges) > store.MediaActivityMaxDetails,
			}
			policyActivity, err := store.NewSystemMediaActivity(currentItem, "metarefresh", store.MediaActivityActionMonitoringChanged, operationID, policyDetails)
			if err != nil {
				return fmt.Errorf("creating future-season activity: %w", err)
			}
			if err := tx.AppendMediaActivity(policyActivity); err != nil {
				return fmt.Errorf("recording future-season activity: %w", err)
			}
		}
		result.Changed = true
		result.ActivityAdded = true
		return nil
	})
	if err != nil {
		return MetadataRefreshResult{}, err
	}
	return result, nil
}

func metadataSeasonCount(meta *store.MediaMetadata) int {
	if meta == nil || meta.Seasons == nil || *meta.Seasons < 0 {
		return 0
	}
	return *meta.Seasons
}

func refreshSeasonWindows(oldSeasons, providerSeasons int) []int {
	if oldSeasons <= 0 && providerSeasons <= 0 {
		return nil
	}
	if providerSeasons > oldSeasons {
		windows := make([]int, 0, providerSeasons-oldSeasons+1)
		if oldSeasons > 0 {
			windows = append(windows, oldSeasons)
		}
		for season := oldSeasons + 1; season <= providerSeasons; season++ {
			windows = append(windows, season)
		}
		return windows
	}
	windows := make([]int, oldSeasons)
	for season := 1; season <= oldSeasons; season++ {
		windows[season-1] = season
	}
	return windows
}

func applyFutureSeasonPolicy(st store.Store, item *store.MediaItem, monitors []store.SeasonMonitor, oldSeasons, newSeasons int) ([]store.MediaActivityMonitoringChange, error) {
	if !item.Monitored || !item.MonitorNewSeasons || newSeasons <= oldSeasons {
		return nil, nil
	}
	bySeason := make(map[int]*store.SeasonMonitor, len(monitors))
	for i := range monitors {
		bySeason[monitors[i].SeasonNumber] = &monitors[i]
	}
	changes := make([]store.MediaActivityMonitoringChange, 0, newSeasons-oldSeasons)
	for season := oldSeasons + 1; season <= newSeasons; season++ {
		monitor := bySeason[season]
		var before *bool
		if monitor == nil {
			monitor = &store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: season, Monitored: true}
			if err := st.CreateSeasonMonitor(monitor); err != nil {
				return nil, fmt.Errorf("creating future season %d monitor: %w", season, err)
			}
		} else {
			value := monitor.Monitored
			before = &value
			if monitor.Monitored {
				continue
			}
			monitor.Monitored = true
			if err := st.UpdateSeasonMonitor(monitor); err != nil {
				return nil, fmt.Errorf("enabling future season %d monitor: %w", season, err)
			}
		}
		after := true
		seasonNumber := season
		changes = append(changes, store.MediaActivityMonitoringChange{
			Target:          store.MediaActivityTarget{Scope: store.MediaActivityScopeSeason, SeasonNumber: &seasonNumber},
			Before:          before,
			After:           &after,
			EffectiveBefore: false,
			EffectiveAfter:  true,
		})
	}
	return changes, nil
}

func activityString(value string) *string {
	if value == "" {
		return nil
	}
	bounded := store.BoundMediaActivityText(value, store.MediaActivityMaxTitleBytes)
	return &bounded
}

// newEpisodeFromData converts an episodeData (provider response) into a
// store.Episode ready for persistence.
func newEpisodeFromData(mediaItemID uint, ep episodeData) *store.Episode {
	var runtime *int
	if ep.runtime > 0 {
		r := ep.runtime
		runtime = &r
	}
	return &store.Episode{
		MediaItemID:   mediaItemID,
		SeasonNumber:  ep.seasonNumber,
		EpisodeNumber: ep.episodeNumber,
		Title:         ep.title,
		Overview:      ep.overview,
		AirDate:       ep.airDate,
		Runtime:       runtime,
	}
}

// cachedTMDB returns a cached TMDB client for the given key, re-creating if the key changed.
func (s *Service) cachedTMDB(apiKey string) *tmdb.Client {
	s.tmdbMu.Lock()
	defer s.tmdbMu.Unlock()
	if s.tmdbCached == nil || s.tmdbKey != apiKey {
		s.tmdbKey = apiKey
		s.tmdbCached = tmdb.NewClient(apiKey, s.httpClient)
	}
	return s.tmdbCached
}

// cachedTVDB returns a cached TVDB client for the given key, re-creating if the key changed.
func (s *Service) cachedTVDB(apiKey string) *tvdb.Client {
	s.tvdbMu.Lock()
	defer s.tvdbMu.Unlock()
	if s.tvdbCached == nil || s.tvdbKey != apiKey {
		s.tvdbKey = apiKey
		s.tvdbCached = tvdb.NewClient(apiKey, s.httpClient)
	}
	return s.tvdbCached
}

func (s *Service) rateLimitKey(source string) string {
	if source == "tvdb" {
		return settings.KeyTVDBRateLimit
	}
	return settings.KeyTMDBRateLimit
}

func genresToJSON(genres []tmdb.Genre) string {
	names := make([]string, len(genres))
	for i, g := range genres {
		names[i] = g.Name
	}
	b, _ := json.Marshal(names)
	return string(b)
}

// ContentRating is one country's content/age certification, normalized across
// providers. Country is ISO 3166-1 alpha-2 upper case; Rating is the raw
// provider string ("TV-MA", "PG-13", "16"), which is regionally meaningful and
// deliberately not translated to a common scale.
type ContentRating struct {
	Country string `json:"country"`
	Rating  string `json:"rating"`
}

// tvdbAlpha3ToAlpha2 maps the lower-case ISO 3166-1 alpha-3 codes TVDB reports
// on contentRatings onto the alpha-2 codes TMDB uses, so one stored country
// preference matches items regardless of which provider matched them.
//
// Codes outside this map are dropped rather than stored under an alpha-3 key:
// the settings picker offers alpha-2 countries only, so an unmapped code could
// never be selected for display anyway, and storing it would just be dead data.
var tvdbAlpha3ToAlpha2 = map[string]string{
	"arg": "AR", "aus": "AU", "aut": "AT", "bel": "BE", "bgr": "BG",
	"bra": "BR", "can": "CA", "che": "CH", "chn": "CN", "cze": "CZ",
	"deu": "DE", "dnk": "DK", "esp": "ES", "est": "EE", "fin": "FI",
	"fra": "FR", "gbr": "GB", "grc": "GR", "hkg": "HK", "hrv": "HR",
	"hun": "HU", "idn": "ID", "ind": "IN", "irl": "IE", "isl": "IS",
	"isr": "IL", "ita": "IT", "jpn": "JP", "kor": "KR", "ltu": "LT",
	"lux": "LU", "lva": "LV", "mex": "MX", "mys": "MY", "nld": "NL",
	"nor": "NO", "nzl": "NZ", "phl": "PH", "pol": "PL", "prt": "PT",
	"rou": "RO", "rus": "RU", "sgp": "SG", "svk": "SK", "svn": "SI",
	"srb": "RS", "swe": "SE", "tha": "TH", "tur": "TR", "twn": "TW",
	"ukr": "UA", "usa": "US", "vnm": "VN", "zaf": "ZA",
}

// tvdbRatingCandidate is one country's competing rating during selection.
type tvdbRatingCandidate struct {
	rating   string
	isSeries bool
	order    int
}

// beats reports whether c should replace cur as the country's rating.
func (c tvdbRatingCandidate) beats(cur tvdbRatingCandidate) bool {
	if c.isSeries != cur.isSeries {
		return c.isSeries
	}
	return c.order > cur.order
}

// tvdbContentRatingsToJSON normalizes TVDB's extended-record contentRatings
// into the shared storage shape.
//
// TVDB can report several ratings for one country — typically a series-level
// entry alongside an episode-level one. Selection prefers the series-level
// rating, then the highest Order among equals.
//
// Order's meaning is NOT documented by TVDB (its published schema declares a
// bare integer), so this deliberately does not rely on it being presentation
// order. Where it is observably a severity rank, highest-wins yields the
// strictest rating — the safe direction, since displaying TV-Y for a TV-MA
// series is a far worse error than the reverse.
func tvdbContentRatingsToJSON(ratings []tvdb.ContentRating) string {
	if len(ratings) == 0 {
		return ""
	}
	best := make(map[string]tvdbRatingCandidate)
	for _, r := range ratings {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		country, ok := tvdbAlpha3ToAlpha2[strings.ToLower(strings.TrimSpace(r.Country))]
		if !ok {
			continue
		}
		c := tvdbRatingCandidate{
			rating:   name,
			isSeries: strings.EqualFold(strings.TrimSpace(r.ContentType), "series"),
			order:    r.Order,
		}
		if cur, seen := best[country]; !seen || c.beats(cur) {
			best[country] = c
		}
	}
	out := make(map[string]string, len(best))
	for country, c := range best {
		out[country] = c.rating
	}
	return contentRatingsToJSON(out)
}

// contentRatingsToJSON serializes a country -> rating map into the stored JSON
// array, sorted by country so the column is stable across refetches (an
// unstable order would rewrite the row on every metadata refresh).
func contentRatingsToJSON(ratings map[string]string) string {
	if len(ratings) == 0 {
		return ""
	}
	countries := make([]string, 0, len(ratings))
	for c := range ratings {
		countries = append(countries, c)
	}
	sort.Strings(countries)

	list := make([]ContentRating, 0, len(countries))
	for _, c := range countries {
		list = append(list, ContentRating{Country: c, Rating: ratings[c]})
	}
	b, _ := json.Marshal(list)
	return string(b)
}

type CreditPerson struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	Type  string `json:"type"`
	Image string `json:"image,omitempty"`
	Order int    `json:"order"`
}

func tmdbCreditsToJSON(credits *tmdb.Credits) string {
	if credits == nil {
		return ""
	}
	var people []CreditPerson

	// Top 10 cast by order
	for i, c := range credits.Cast {
		if i >= 10 {
			break
		}
		people = append(people, CreditPerson{
			Name:  c.Name,
			Role:  c.Character,
			Type:  "cast",
			Image: c.ProfilePath,
			Order: c.Order,
		})
	}

	// Up to 5 key crew (Director, Writer, Screenplay)
	keyJobs := map[string]bool{"Director": true, "Writer": true, "Screenplay": true}
	crewCount := 0
	for _, c := range credits.Crew {
		if crewCount >= 5 {
			break
		}
		if !keyJobs[c.Job] {
			continue
		}
		people = append(people, CreditPerson{
			Name:  c.Name,
			Role:  c.Job,
			Type:  "crew",
			Image: c.ProfilePath,
			Order: crewCount,
		})
		crewCount++
	}

	if len(people) == 0 {
		return ""
	}
	b, _ := json.Marshal(people)
	return string(b)
}

func tvdbCharactersToJSON(characters []tvdb.Character) string {
	if len(characters) == 0 {
		return ""
	}
	var people []CreditPerson

	castTypes := map[string]bool{"Actor": true, "Guest Star": true}
	crewTypes := map[string]bool{"Director": true, "Writer": true}
	castCount, crewCount := 0, 0

	for _, c := range characters {
		if castTypes[c.PeopleType] && castCount < 10 {
			people = append(people, CreditPerson{
				Name:  c.PersonName,
				Role:  c.Name,
				Type:  "cast",
				Image: c.PersonImgURL,
				Order: c.Sort,
			})
			castCount++
		} else if crewTypes[c.PeopleType] && crewCount < 5 {
			people = append(people, CreditPerson{
				Name:  c.PersonName,
				Role:  c.PeopleType,
				Type:  "crew",
				Image: c.PersonImgURL,
				Order: crewCount,
			})
			crewCount++
		}
	}

	if len(people) == 0 {
		return ""
	}
	b, _ := json.Marshal(people)
	return string(b)
}

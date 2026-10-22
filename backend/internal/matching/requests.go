package matching

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

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

		activityAdded, err = recordRequestAttribution(tx, item, requestAttributionInput{
			requesterID: req.RequesterID, scopes: scopes, mediaAdded: true, assignedProfile: assignedProfile,
		})
		if err != nil {
			return err
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
	activityAdded, err := recordRequestAttribution(tx, current, requestAttributionInput{
		requesterID: req.RequesterID, scopes: scopes, before: before, assignedProfile: assignedProfile,
	})
	if err != nil {
		return nil, false, err
	}
	return current, activityAdded, nil
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

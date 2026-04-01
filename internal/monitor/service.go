package monitor

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const defaultPollInterval = 15 * time.Minute

// activeStatuses are download statuses that indicate work is in progress or complete.
// If a download has one of these statuses, the item/episode should not be re-downloaded.
var activeStatuses = map[string]bool{
	"pending":     true,
	"downloading": true,
	"downloaded":  true,
	"importing":   true,
	"seeding":     true,
	"completed":   true,
}

type Service struct {
	store      store.Store
	indexerSvc *indexer.Service
	settings   *settings.Service
	bus        *eventbus.Bus
	stopCh     chan struct{}
}

func NewService(s store.Store, indexerSvc *indexer.Service, settingsSvc *settings.Service, bus *eventbus.Bus) *Service {
	return &Service{
		store:      s,
		indexerSvc: indexerSvc,
		settings:   settingsSvc,
		bus:        bus,
		stopCh:     make(chan struct{}),
	}
}

func (s *Service) Start() {
	go s.run()
}

func (s *Service) Stop() {
	close(s.stopCh)
}

func (s *Service) run() {
	settingsCh := s.settings.Subscribe()
	interval := s.settings.GetDurationWithDefault(settings.KeyWorkerMonitorInterval, defaultPollInterval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("monitor worker started", "interval", interval)

	// Run once after a short startup delay to let other services initialize.
	time.Sleep(30 * time.Second)
	s.processOnce()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.processOnce()
		case key := <-settingsCh:
			if key == settings.KeyWorkerMonitorInterval {
				newInterval := s.settings.GetDurationWithDefault(settings.KeyWorkerMonitorInterval, defaultPollInterval)
				if newInterval != interval {
					interval = newInterval
					ticker.Reset(interval)
					slog.Info("monitor interval updated", "interval", interval)
				}
			}
		}
	}
}

func (s *Service) processOnce() {
	items, err := s.store.ListMonitoredMediaItems()
	if err != nil {
		slog.Error("monitor: failed to list monitored items", "error", err)
		return
	}

	if len(items) == 0 {
		return
	}

	slog.Debug("monitor: processing monitored items", "count", len(items))

	for i := range items {
		item := &items[i]

		meta, err := s.store.GetMediaMetadataByMediaItem(item.ID)
		if err != nil || meta == nil {
			continue // not matched yet, skip
		}

		downloads, err := s.store.ListDownloads(&item.ID, nil)
		if err != nil {
			slog.Error("monitor: failed to list downloads", "item_id", item.ID, "error", err)
			continue
		}

		files, err := s.store.ListMediaFilesByMediaItem(item.ID)
		if err != nil {
			slog.Error("monitor: failed to list files", "item_id", item.ID, "error", err)
			continue
		}

		if item.MediaType == "movie" {
			s.processMovie(item, meta, downloads, files)
		} else if item.MediaType == "series" {
			s.processSeries(item, meta, downloads, files)
		}
	}
}

func (s *Service) processMovie(item *store.MediaItem, meta *store.MediaMetadata, downloads []store.Download, files []store.MediaFile) {
	// Already have files — no upgrade
	if len(files) > 0 {
		return
	}

	// Already have an active download
	if hasActiveDownload(downloads, nil) {
		return
	}

	// Check if released
	if !isReleased(meta) {
		return
	}

	// Search indexers
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := indexer.SearchParams{
		ImdbID: meta.ImdbID,
		Type:   "movie-search",
		Limit:  50,
	}
	if meta.ImdbID == "" {
		params.Query = item.Title
		params.Type = "search"
	}

	results, err := s.indexerSvc.Search(ctx, params)
	if err != nil {
		slog.Warn("monitor: search failed", "item_id", item.ID, "title", item.Title, "error", err)
		return
	}

	// Filter by quality profile
	filtered := s.filterByProfile(results, item)

	if len(filtered) == 0 {
		s.markSearchStarted(item)
		return
	}

	// Pick the best (highest seeders — already sorted by indexer search)
	best := filtered[0]

	// Final dedup check
	freshDownloads, _ := s.store.ListDownloads(&item.ID, nil)
	if hasActiveDownload(freshDownloads, nil) {
		return
	}

	s.createAutoDownload(item, best, nil, nil)
}

func (s *Service) processSeries(item *store.MediaItem, meta *store.MediaMetadata, downloads []store.Download, files []store.MediaFile) {
	episodes, err := s.store.ListEpisodesByMediaItem(item.ID)
	if err != nil {
		slog.Error("monitor: failed to list episodes", "item_id", item.ID, "error", err)
		return
	}

	monitors, err := s.store.ListSeasonMonitorsByMediaItem(item.ID)
	if err != nil {
		slog.Error("monitor: failed to list season monitors", "item_id", item.ID, "error", err)
		return
	}

	monitorLookup := make(map[int]bool)
	for _, m := range monitors {
		monitorLookup[m.SeasonNumber] = m.Monitored
	}

	packPref := s.settings.GetWithDefault(settings.KeyMonitorSeasonPackPref, "prefer_packs")

	// Build wanted episodes and count aired episodes per season
	today := time.Now().Format("2006-01-02")
	fileMap := buildFileMap(files)
	downloadMap := buildDownloadMap(downloads)

	airedPerSeason := make(map[int]int)

	type wantedEp struct {
		episode store.Episode
	}

	var wanted []wantedEp
	for _, ep := range episodes {
		// Must be aired
		if ep.AirDate == "" || ep.AirDate > today {
			continue
		}
		// Must be in a monitored season (default: true)
		if monitored, ok := monitorLookup[ep.SeasonNumber]; ok && !monitored {
			continue
		}

		airedPerSeason[ep.SeasonNumber]++

		// Must not have a file
		if fileMap[fileKey(ep.SeasonNumber, ep.EpisodeNumber)] {
			continue
		}
		// Must not have an active download
		epID := ep.ID
		if hasActiveDownloadForEpisode(downloadMap, &epID, ep.SeasonNumber) {
			continue
		}
		wanted = append(wanted, wantedEp{episode: ep})
	}

	if len(wanted) == 0 {
		return
	}

	// Group by season
	seasonWanted := make(map[int][]wantedEp)
	for _, w := range wanted {
		seasonWanted[w.episode.SeasonNumber] = append(seasonWanted[w.episode.SeasonNumber], w)
	}

	foundAny := false
	for seasonNum, wantedEps := range seasonWanted {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		params := indexer.SearchParams{
			ImdbID: meta.ImdbID,
			Type:   "tv-search",
			Season: strconv.Itoa(seasonNum),
			Limit:  100,
		}
		if meta.ImdbID == "" {
			params.Query = item.Title
			params.Type = "search"
		}

		results, err := s.indexerSvc.Search(ctx, params)
		cancel()
		if err != nil {
			slog.Warn("monitor: series search failed",
				"item_id", item.ID, "title", item.Title, "season", seasonNum, "error", err)
			continue
		}

		filtered := s.filterByProfile(results, item)
		if len(filtered) == 0 {
			continue
		}

		wantedRatio := float64(len(wantedEps)) / float64(airedPerSeason[seasonNum])

		// Try to find results for each wanted episode
		for _, w := range wantedEps {
			best := s.findBestForEpisode(filtered, w.episode, packPref, wantedRatio)
			if best == nil {
				continue
			}

			// Final dedup check
			epID := w.episode.ID
			freshDownloads, _ := s.store.ListDownloads(&item.ID, nil)
			freshMap := buildDownloadMap(freshDownloads)
			if hasActiveDownloadForEpisode(freshMap, &epID, w.episode.SeasonNumber) {
				continue
			}

			parsed := fileparse.ParseTorrentSeasonEpisode(best.Title)
			if parsed.Season != nil && parsed.Episode == nil {
				// Season pack — create one download for the whole season
				sn := seasonNum
				s.createAutoDownload(item, *best, nil, &sn)
				foundAny = true
				break // Don't create individual episode downloads for this season
			}

			eid := w.episode.ID
			sn := seasonNum
			s.createAutoDownload(item, *best, &eid, &sn)
			foundAny = true
		}
	}

	if !foundAny {
		s.markSearchStarted(item)
	}
}

// findBestForEpisode finds the best matching torrent result for a specific episode.
// The preference between season packs and individual episodes is controlled by packPref
// ("prefer_packs", "prefer_episodes", "packs_only") and the wantedRatio (fraction of
// aired episodes in the season that are missing).
func (s *Service) findBestForEpisode(results []indexer.TorrentResult, ep store.Episode, packPref string, wantedRatio float64) *indexer.TorrentResult {
	var bestEpisode *indexer.TorrentResult
	var bestSeasonPack *indexer.TorrentResult

	for i := range results {
		r := &results[i]
		parsed := fileparse.ParseTorrentSeasonEpisode(r.Title)

		if parsed.Season == nil || *parsed.Season != ep.SeasonNumber {
			continue
		}

		if parsed.Episode != nil {
			// Individual episode match
			if *parsed.Episode == ep.EpisodeNumber {
				if bestEpisode == nil {
					bestEpisode = r
				}
			}
			// Episode range match
			if parsed.EpisodeEnd != nil {
				if ep.EpisodeNumber >= *parsed.Episode && ep.EpisodeNumber <= *parsed.EpisodeEnd {
					if bestEpisode == nil {
						bestEpisode = r
					}
				}
			}
		} else {
			// Season pack (no episode number)
			if bestSeasonPack == nil {
				bestSeasonPack = r
			}
		}
	}

	switch packPref {
	case "packs_only":
		return bestSeasonPack
	case "prefer_packs":
		if wantedRatio >= 0.7 && bestSeasonPack != nil {
			return bestSeasonPack
		}
		if bestEpisode != nil {
			return bestEpisode
		}
		return bestSeasonPack
	default: // "prefer_episodes"
		if bestEpisode != nil {
			return bestEpisode
		}
		return bestSeasonPack
	}
}

func (s *Service) filterByProfile(results []indexer.TorrentResult, item *store.MediaItem) []indexer.TorrentResult {
	profile := s.resolveProfile(item)
	if profile == nil {
		return results
	}

	var resolutions, sources, excludeTags []string
	_ = json.Unmarshal([]byte(profile.Resolutions), &resolutions)
	if profile.Sources != "" {
		_ = json.Unmarshal([]byte(profile.Sources), &sources)
	}
	if profile.ExcludeTags != "" {
		_ = json.Unmarshal([]byte(profile.ExcludeTags), &excludeTags)
	}

	var filtered []indexer.TorrentResult
	for _, r := range results {
		if len(excludeTags) > 0 && fileparse.ContainsExcludedTag(r.Title, excludeTags) {
			continue
		}
		res := fileparse.ParseResolution(r.Title)
		src := fileparse.ParseSource(r.Title)
		if fileparse.MatchesProfile(res, src, resolutions, sources) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (s *Service) resolveProfile(item *store.MediaItem) *store.MediaProfile {
	if item.MediaProfileID != nil {
		profile, err := s.store.GetMediaProfile(*item.MediaProfileID)
		if err == nil {
			return profile
		}
		slog.Warn("monitor: item profile not found, trying library default",
			"item_id", item.ID, "profile_id", *item.MediaProfileID)
	}

	lib, err := s.store.GetLibrary(item.LibraryID)
	if err == nil && lib.MediaProfileID != nil {
		profile, err := s.store.GetMediaProfile(*lib.MediaProfileID)
		if err == nil {
			return profile
		}
	}

	return nil
}

func (s *Service) createAutoDownload(item *store.MediaItem, result indexer.TorrentResult, episodeID *uint, seasonNumber *int) {
	dl := &store.Download{
		MediaItemID:  item.ID,
		EpisodeID:    episodeID,
		SeasonNumber: seasonNumber,
		IndexerID:    result.IndexerID,
		IndexerName:  result.IndexerName,
		Title:        result.Title,
		DownloadURL:  result.DownloadURL,
		DetailsURL:   result.DetailsURL,
		Size:         result.Size,
		ImdbID:       result.ImdbID,
		Status:       "pending",
	}

	if err := s.store.CreateDownload(dl); err != nil {
		slog.Error("monitor: failed to create download",
			"item_id", item.ID, "title", result.Title, "error", err)
		return
	}

	slog.Info("monitor: auto-download created",
		"item_id", item.ID, "item_title", item.Title, "torrent", result.Title)

	s.bus.Publish(eventbus.DownloadCreated, eventbus.DownloadPayload{
		DownloadID:  dl.ID,
		MediaItemID: item.ID,
		Title:       result.Title,
		Status:      "pending",
	})

	s.bus.Publish(eventbus.MonitorGrabbed, eventbus.MonitorPayload{
		MediaItemID: item.ID,
		Title:       item.Title,
		ResultTitle: result.Title,
	})

	// Clear search started marker
	item.MonitorSearchStartedAt = nil
	_ = s.store.UpdateMediaItem(item)
}

func (s *Service) markSearchStarted(item *store.MediaItem) {
	if item.MonitorSearchStartedAt != nil {
		return // already tracking
	}
	now := time.Now()
	item.MonitorSearchStartedAt = &now
	if err := s.store.UpdateMediaItem(item); err != nil {
		slog.Error("monitor: failed to update search started", "item_id", item.ID, "error", err)
	}
}

// --- helpers ---

func isReleased(meta *store.MediaMetadata) bool {
	today := time.Now().Format("2006-01-02")
	if meta.ReleaseDate != "" {
		return meta.ReleaseDate <= today
	}
	// Fallback: use year
	if meta.Year != nil {
		return *meta.Year <= time.Now().Year()
	}
	return false
}

func hasActiveDownload(downloads []store.Download, episodeID *uint) bool {
	for _, dl := range downloads {
		if !activeStatuses[dl.Status] {
			continue
		}
		if episodeID == nil {
			return true // movie — any active download counts
		}
		if dl.EpisodeID != nil && *dl.EpisodeID == *episodeID {
			return true
		}
	}
	return false
}

type downloadKey struct {
	episodeID    uint
	seasonNumber int
}

func buildDownloadMap(downloads []store.Download) map[downloadKey]bool {
	m := make(map[downloadKey]bool)
	for _, dl := range downloads {
		if !activeStatuses[dl.Status] {
			continue
		}
		if dl.EpisodeID != nil {
			m[downloadKey{episodeID: *dl.EpisodeID}] = true
		}
		if dl.SeasonNumber != nil && dl.EpisodeID == nil {
			// Season pack download
			m[downloadKey{seasonNumber: *dl.SeasonNumber}] = true
		}
	}
	return m
}

func hasActiveDownloadForEpisode(m map[downloadKey]bool, episodeID *uint, seasonNumber int) bool {
	if episodeID != nil && m[downloadKey{episodeID: *episodeID}] {
		return true
	}
	// Also check if a season pack is active
	return m[downloadKey{seasonNumber: seasonNumber}]
}

func buildFileMap(files []store.MediaFile) map[string]bool {
	m := make(map[string]bool)
	for _, f := range files {
		if f.SeasonNumber != nil && f.EpisodeNumber != nil {
			m[fileKey(*f.SeasonNumber, *f.EpisodeNumber)] = true
		}
	}
	return m
}

func fileKey(season, episode int) string {
	return strconv.Itoa(season) + "x" + strconv.Itoa(episode)
}

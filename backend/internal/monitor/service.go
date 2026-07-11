package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/worker"
)

const defaultPollInterval = 15 * time.Minute

// maxBlocklistFailures is the number of failed grab attempts for a single
// (media item, download URL) after which the monitor stops re-grabbing that
// release. Policy: hard block (no cooldown) — once a release has produced this
// many terminally-failed download rows it is considered permanently broken for
// that item. See recordDownloadFailures and createAutoDownload.
const maxBlocklistFailures = 3

// activeStatuses is derived from the shared store.ActiveDownloadStatuses list.
var activeStatuses = func() map[string]bool {
	m := make(map[string]bool, len(store.ActiveDownloadStatuses))
	for _, s := range store.ActiveDownloadStatuses {
		m[s] = true
	}
	return m
}()

// terminalFailureStatuses is derived from store.TerminalFailureStatuses and
// marks downloads that have permanently failed (not active, will not progress).
var terminalFailureStatuses = func() map[string]bool {
	m := make(map[string]bool, len(store.TerminalFailureStatuses))
	for _, s := range store.TerminalFailureStatuses {
		m[s] = true
	}
	return m
}()

type Service struct {
	store      store.Store
	indexerSvc *indexer.Service
	settings   *settings.Service
	bus        *eventbus.Bus
	loop       *worker.Loop
}

func NewService(s store.Store, indexerSvc *indexer.Service, settingsSvc *settings.Service, bus *eventbus.Bus) *Service {
	svc := &Service{
		store:      s,
		indexerSvc: indexerSvc,
		settings:   settingsSvc,
		bus:        bus,
	}
	svc.loop = worker.New(worker.Config{
		Name:            "monitor",
		DefaultInterval: defaultPollInterval,
		IntervalKey:     settings.KeyWorkerMonitorInterval,
		Settings:        settingsSvc,
		Process:         svc.processOnce,
		StartupDelay:    30 * time.Second,
	})
	return svc
}

func (s *Service) Start() { s.loop.Start() }

func (s *Service) Stop() { s.loop.Stop() }

// Loop returns the underlying worker loop for registry purposes.
func (s *Service) Loop() *worker.Loop { return s.loop }

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

		slog.Debug("monitor: checking item", "item_id", item.ID, "title", item.Title, "type", item.MediaType)

		meta, err := s.store.GetMediaMetadataByMediaItem(item.ID)
		if err != nil || meta == nil {
			continue // not matched yet, skip
		}

		downloads, err := s.store.ListDownloads(&item.ID, nil)
		if err != nil {
			slog.Error("monitor: failed to list downloads", "item_id", item.ID, "error", err)
			continue
		}

		// Record any terminally-failed releases in the blocklist so we stop
		// re-grabbing broken torrents on subsequent cycles.
		s.recordDownloadFailures(item, downloads)

		files, err := s.store.ListMediaFilesByMediaItem(item.ID)
		if err != nil {
			slog.Error("monitor: failed to list files", "item_id", item.ID, "error", err)
			continue
		}

		switch item.MediaType {
		case "movie":
			s.processMovie(item, meta, downloads, files)
		case "series":
			s.processSeries(item, meta, downloads, files)
		}
	}
}

func (s *Service) processMovie(item *store.MediaItem, meta *store.MediaMetadata, downloads []store.Download, files []store.MediaFile) {
	// Already have files — no upgrade
	if len(files) > 0 {
		slog.Debug("monitor: movie already has files, skipping", "item_id", item.ID, "title", item.Title)
		return
	}

	// Already have an active download
	if hasActiveDownload(downloads, nil) {
		slog.Debug("monitor: movie already has active download, skipping", "item_id", item.ID, "title", item.Title)
		return
	}

	// Check if released
	if !isReleased(meta) {
		slog.Debug("monitor: movie not yet released, skipping", "item_id", item.ID, "title", item.Title)
		return
	}

	// Search indexers
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := indexer.SearchParams{
		Query:  item.Title,
		ImdbID: meta.ImdbID,
		Type:   "movie-search",
		Limit:  50,
	}
	if meta.ImdbID == "" {
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

	// Build episode monitor lookup for per-episode overrides
	epMonitors, _ := s.store.ListEpisodeMonitorsByMediaItem(item.ID)
	epMonitorLookup := make(map[string]bool, len(epMonitors))
	for _, em := range epMonitors {
		key := fmt.Sprintf("S%dE%d", em.SeasonNumber, em.EpisodeNumber)
		epMonitorLookup[key] = em.Monitored
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
		// Resolve monitored: episode-level override > season-level > not monitored
		epKey := fmt.Sprintf("S%dE%d", ep.SeasonNumber, ep.EpisodeNumber)
		if epMon, ok := epMonitorLookup[epKey]; ok {
			if !epMon {
				continue // explicitly unmonitored episode
			}
		} else if seasonMon, ok := monitorLookup[ep.SeasonNumber]; !ok || !seasonMon {
			continue // season not monitored and no episode override
		}

		airedPerSeason[ep.SeasonNumber]++

		// Must not have a file
		if fileMap[fileKey(ep.SeasonNumber, ep.EpisodeNumber)] {
			continue
		}
		// Must not have an active download
		epID := ep.ID
		if hasActiveDownloadForEpisode(downloadMap, &epID, ep.SeasonNumber, ep.EpisodeNumber) {
			continue
		}
		wanted = append(wanted, wantedEp{episode: ep})
	}

	if len(wanted) == 0 {
		slog.Debug("monitor: series has no missing aired episodes, skipping", "item_id", item.ID, "title", item.Title)
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
			Query:  item.Title,
			ImdbID: meta.ImdbID,
			Type:   "tv-search",
			Season: strconv.Itoa(seasonNum),
			Limit:  100,
		}
		if meta.ImdbID == "" {
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
			if hasActiveDownloadForEpisode(freshMap, &epID, w.episode.SeasonNumber, w.episode.EpisodeNumber) {
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
	filtered := results
	if profile := s.resolveProfile(item); profile != nil {
		globalTags := s.loadGlobalExcludeTags()
		filtered = indexer.FilterByMediaProfile(results, profile, globalTags...)
	}
	// Soft per-item preferred-release boost: stably float matching releases to
	// the top so the picker (first match) grabs them, without blocking grabs
	// when nothing matches. Runs even when no profile is set.
	return indexer.PreferReleases(filtered, item.PreferredRelease)
}

func (s *Service) loadGlobalExcludeTags() []string {
	raw, err := s.settings.Get(settings.KeyGlobalExcludeTags)
	if err != nil || raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	return tags
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
	// Guard against duplicate downloads (same torrent URL already active).
	exists, err := s.store.HasActiveDownloadByURL(item.ID, result.DownloadURL)
	if err != nil {
		slog.Error("monitor: failed to check duplicate download",
			"item_id", item.ID, "title", result.Title, "error", err)
		return
	}
	if exists {
		slog.Debug("monitor: skipping duplicate download",
			"item_id", item.ID, "title", result.Title)
		return
	}

	// Guard against re-grabbing a release that has already failed repeatedly
	// (dead URL, qBit error, repeated import failure). Failed downloads are not
	// "active", so without this the same broken URL would be grabbed every cycle.
	blocked, err := s.store.IsBlocklisted(item.ID, result.DownloadURL, maxBlocklistFailures)
	if err != nil {
		slog.Error("monitor: failed to check download blocklist",
			"item_id", item.ID, "title", result.Title, "error", err)
		return
	}
	if blocked {
		slog.Info("monitor: skipping blocklisted release",
			"item_id", item.ID, "title", result.Title, "url", result.DownloadURL)
		return
	}

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

// recordDownloadFailures scans an item's downloads for terminally-failed
// releases and records them in the persistent blocklist. The fail count for a
// URL is the number of failed download rows observed for it — each monitor grab
// that later fails produces one such row, so the count reflects how many times
// the release has been (re)grabbed and failed. The store keeps this as a
// high-water mark, so the counting is idempotent across cycles. Once a URL
// reaches maxBlocklistFailures the monitor stops re-grabbing it (see
// createAutoDownload).
func (s *Service) recordDownloadFailures(item *store.MediaItem, downloads []store.Download) {
	type agg struct {
		count   int
		title   string
		lastErr string
	}
	byURL := make(map[string]*agg)
	for _, dl := range downloads {
		if dl.DownloadURL == "" || !terminalFailureStatuses[dl.Status] {
			continue
		}
		a := byURL[dl.DownloadURL]
		if a == nil {
			a = &agg{}
			byURL[dl.DownloadURL] = a
		}
		a.count++
		a.title = dl.Title
		if dl.LastError != "" {
			a.lastErr = dl.LastError
		}
	}
	for url, a := range byURL {
		if err := s.store.RecordBlocklistFailure(item.ID, url, a.title, a.lastErr, a.count); err != nil {
			slog.Error("monitor: failed to record blocklist failure",
				"item_id", item.ID, "url", url, "error", err)
		}
	}
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
	episodeID     uint
	seasonNumber  int
	episodeNumber int // from title parsing when episodeID is unknown
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
		// Parse the title to detect season packs vs single episodes, and to
		// track downloads that lack an episode_id (e.g. created via the UI
		// when the episode doesn't exist in the DB yet).
		parsed := fileparse.ParseTorrentSeasonEpisode(dl.Title)
		if parsed.Season != nil && parsed.Episode == nil && dl.SeasonNumber != nil && dl.EpisodeID == nil {
			// Season pack
			m[downloadKey{seasonNumber: *dl.SeasonNumber}] = true
		}
		if parsed.Season != nil && parsed.Episode != nil {
			// Determine the episode range this download covers.
			isRange := parsed.EpisodeEnd != nil && *parsed.EpisodeEnd > *parsed.Episode
			// Track by parsed season+episode when either:
			//   - the download lacks an episode_id (e.g. created via the UI, or
			//     the episode row doesn't exist yet), or
			//   - the title is a multi-episode range: the episode_id only covers
			//     the first episode, so E02..EpisodeEnd would otherwise be
			//     unguarded and the monitor would grab overlapping releases.
			if dl.EpisodeID == nil || isRange {
				end := *parsed.Episode
				if isRange {
					end = *parsed.EpisodeEnd
				}
				for ep := *parsed.Episode; ep <= end; ep++ {
					m[downloadKey{seasonNumber: *parsed.Season, episodeNumber: ep}] = true
				}
			}
		}
	}
	return m
}

func hasActiveDownloadForEpisode(m map[downloadKey]bool, episodeID *uint, seasonNumber int, episodeNumber int) bool {
	if episodeID != nil && m[downloadKey{episodeID: *episodeID}] {
		return true
	}
	// Check season pack
	if m[downloadKey{seasonNumber: seasonNumber}] {
		return true
	}
	// Check title-parsed single episode (covers downloads missing episode_id)
	return m[downloadKey{seasonNumber: seasonNumber, episodeNumber: episodeNumber}]
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

package metarefresh

import (
	"log/slog"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/worker"
)

const defaultInterval = 6 * time.Hour

// StatusRecalculator recalculates a media item's status based on current files
// and episodes. Implemented by sync.Service.
type StatusRecalculator interface {
	RecalcMediaItemStatus(itemID uint) error
}

// Service periodically checks TMDB/TVDB for new seasons on monitored series
// and creates episode records so the monitor worker can grab them.
type Service struct {
	store    store.Store
	matchSvc *matching.Service
	syncSvc  StatusRecalculator
	bus      *eventbus.Bus
	loop     *worker.Loop
}

// NewService creates a metadata refresh worker.
func NewService(s store.Store, matchSvc *matching.Service, syncSvc StatusRecalculator, settingsSvc *settings.Service, bus *eventbus.Bus) *Service {
	svc := &Service{
		store:    s,
		matchSvc: matchSvc,
		syncSvc:  syncSvc,
		bus:      bus,
	}
	svc.loop = worker.New(worker.Config{
		Name:            "metadata-refresh",
		DefaultInterval: defaultInterval,
		IntervalKey:     settings.KeyWorkerMetadataRefreshInterval,
		Settings:        settingsSvc,
		Process:         svc.processOnce,
		StartupDelay:    2 * time.Minute,
	})
	return svc
}

// Start begins the periodic metadata refresh loop.
func (s *Service) Start() { s.loop.Start() }

// Stop halts the periodic metadata refresh loop.
func (s *Service) Stop() { s.loop.Stop() }

// Loop returns the underlying worker loop for registry purposes.
func (s *Service) Loop() *worker.Loop { return s.loop }

func (s *Service) processOnce() {
	items, err := s.store.ListMonitoredMediaItems()
	if err != nil {
		slog.Error("metadata-refresh: failed to list monitored items", "error", err)
		return
	}

	var checked, updated int
	for i := range items {
		item := &items[i]
		if item.MediaType != "series" {
			continue
		}

		meta, err := s.store.GetMediaMetadataByMediaItem(item.ID)
		if err != nil {
			continue // not matched yet
		}

		if meta.Status == "Ended" || meta.Status == "Canceled" {
			continue
		}

		checked++

		changed, err := s.matchSvc.RefreshSeriesMetadata(item, meta)
		if err != nil {
			slog.Warn("metadata-refresh: failed to refresh",
				"item_id", item.ID, "title", item.Title, "error", err)
			continue
		}

		if changed {
			// Auto-create SeasonMonitor rows for new seasons when MonitorNewSeasons is enabled.
			if item.MonitorNewSeasons {
				monitors, _ := s.store.ListSeasonMonitorsByMediaItem(item.ID)
				monitoredSet := make(map[int]bool, len(monitors))
				for _, m := range monitors {
					monitoredSet[m.SeasonNumber] = true
				}
				episodes, _ := s.store.ListEpisodesByMediaItem(item.ID)
				seenSeasons := make(map[int]bool)
				for _, ep := range episodes {
					sn := ep.SeasonNumber
					if !seenSeasons[sn] && !monitoredSet[sn] {
						_ = s.store.CreateSeasonMonitor(&store.SeasonMonitor{
							MediaItemID:  item.ID,
							SeasonNumber: sn,
							Monitored:    true,
						})
						seenSeasons[sn] = true
					}
				}
			}

			// Resolve orphan downloads: fill in episode_id for single-episode downloads
			// that were created before the episode existed in the database (e.g. via season
			// search when metadata provider didn't list the episode yet).
			s.resolveOrphanDownloads(item.ID)

			updated++
			_ = s.syncSvc.RecalcMediaItemStatus(item.ID)
			s.bus.Publish(eventbus.MetadataRefreshed, eventbus.MediaItemPayload{
				MediaItemID: item.ID,
				LibraryID:   item.LibraryID,
				Title:       item.Title,
			})
		}

		// Rate-limit API calls: brief pause between items.
		time.Sleep(500 * time.Millisecond)
	}

	if checked > 0 {
		slog.Debug("metadata-refresh: cycle complete", "checked", checked, "updated", updated)
	}
}

// resolveOrphanDownloads fills in EpisodeID for active downloads that were created
// before the episode existed in the database. This happens when a user downloads via
// season search before the metadata provider lists the episode, then later refreshes
// metadata to a provider that knows about the episode.
func (s *Service) resolveOrphanDownloads(itemID uint) {
	downloads, err := s.store.ListDownloads(&itemID, nil)
	if err != nil {
		return
	}
	for i := range downloads {
		dl := &downloads[i]
		if dl.EpisodeID != nil {
			continue
		}
		// Skip terminal downloads — no point resolving episode_id on finished work.
		// Note: different from store.ActiveDownloadStatuses which includes "completed".
		if dl.Status == "completed" || dl.Status == "failed" {
			continue
		}
		parsed := fileparse.ParseTorrentSeasonEpisode(dl.Title)
		if parsed.Season == nil || parsed.Episode == nil {
			continue // can't determine episode, or it's a genuine season pack
		}
		ep, err := s.store.GetEpisodeByNumber(itemID, *parsed.Season, *parsed.Episode)
		if err != nil {
			continue // episode still not in DB
		}
		dl.EpisodeID = &ep.ID
		if dl.SeasonNumber == nil {
			sn := *parsed.Season
			dl.SeasonNumber = &sn
		}
		if err := s.store.UpdateDownload(dl); err != nil {
			slog.Warn("metadata-refresh: failed to resolve orphan download",
				"download_id", dl.ID, "title", dl.Title, "error", err)
			continue
		}
		slog.Info("metadata-refresh: resolved orphan download",
			"download_id", dl.ID, "title", dl.Title, "episode_id", ep.ID,
			"season", *parsed.Season, "episode", *parsed.Episode)
	}
}

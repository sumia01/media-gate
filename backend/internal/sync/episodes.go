package sync

import (
	"fmt"
	"sort"

	"github.com/sumia01/media-gate/internal/store"
)

// EpisodeSummary is a single episode enriched with file-presence and download status.
type EpisodeSummary struct {
	Episode        store.Episode
	HasFile        bool
	DownloadStatus string // empty means no active download
}

// SeasonSummary groups episodes by season number with aggregate counts.
type SeasonSummary struct {
	SeasonNumber      int
	TotalEpisodes     int
	AvailableEpisodes int
	Monitored         bool
	Episodes          []EpisodeSummary
}

// AssembleEpisodes builds a season-grouped, enriched view of episodes for a
// media item including file presence, season monitor state, and download status
// with priority resolution (episode > season > item level).
func (s *Service) AssembleEpisodes(itemID uint) ([]SeasonSummary, error) {
	episodes, err := s.store.ListEpisodesByMediaItem(itemID)
	if err != nil {
		return nil, err
	}

	files, err := s.store.ListMediaFilesByMediaItem(itemID)
	if err != nil {
		return nil, err
	}

	monitors, err := s.store.ListSeasonMonitorsByMediaItem(itemID)
	if err != nil {
		return nil, err
	}

	downloads, err := s.store.ListDownloads(&itemID, nil)
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

	// Build download status lookups.
	// Priority: downloading > pending > downloaded > importing > seeding (completed/failed ignored).
	dlStatusPriority := map[string]int{
		"downloading": 5,
		"pending":     4,
		"downloaded":  3,
		"importing":   2,
		"seeding":     1,
	}
	episodeDownloadStatus := make(map[uint]string)
	seasonDownloadStatus := make(map[int]string)
	var itemDownloadStatus string
	for _, dl := range downloads {
		pri := dlStatusPriority[dl.Status]
		if pri == 0 {
			continue // skip completed/failed
		}
		if dl.EpisodeID != nil {
			if cur, ok := episodeDownloadStatus[*dl.EpisodeID]; !ok || pri > dlStatusPriority[cur] {
				episodeDownloadStatus[*dl.EpisodeID] = dl.Status
			}
		} else if dl.SeasonNumber != nil {
			sn := *dl.SeasonNumber
			if cur, ok := seasonDownloadStatus[sn]; !ok || pri > dlStatusPriority[cur] {
				seasonDownloadStatus[sn] = dl.Status
			}
		} else {
			if dlStatusPriority[itemDownloadStatus] < pri {
				itemDownloadStatus = dl.Status
			}
		}
	}

	// Group episodes by season.
	seasonMap := make(map[int][]EpisodeSummary)
	for _, ep := range episodes {
		key := fmt.Sprintf("S%dE%d", ep.SeasonNumber, ep.EpisodeNumber)
		summary := EpisodeSummary{
			Episode: ep,
			HasFile: fileLookup[key],
		}

		// Resolve download status: episode-level > season-level > item-level.
		if status, ok := episodeDownloadStatus[ep.ID]; ok {
			summary.DownloadStatus = status
		} else if status, ok := seasonDownloadStatus[ep.SeasonNumber]; ok {
			summary.DownloadStatus = status
		} else if itemDownloadStatus != "" {
			summary.DownloadStatus = itemDownloadStatus
		}

		seasonMap[ep.SeasonNumber] = append(seasonMap[ep.SeasonNumber], summary)
	}

	// Build sorted season summaries.
	seasons := make([]SeasonSummary, 0, len(seasonMap))
	for sn, eps := range seasonMap {
		available := 0
		for _, ep := range eps {
			if ep.HasFile {
				available++
			}
		}
		monitored, ok := monitorLookup[sn]
		if !ok {
			monitored = false // explicit: no row = not monitored
		}
		seasons = append(seasons, SeasonSummary{
			SeasonNumber:      sn,
			TotalEpisodes:     len(eps),
			AvailableEpisodes: available,
			Monitored:         monitored,
			Episodes:          eps,
		})
	}

	sort.Slice(seasons, func(i, j int) bool {
		return seasons[i].SeasonNumber < seasons[j].SeasonNumber
	})

	return seasons, nil
}

// SeasonMonitorInput represents a season monitor create/update request.
type SeasonMonitorInput struct {
	SeasonNumber int
	Monitored    bool
}

// UpsertSeasonMonitors creates or updates season monitors for a media item.
func (s *Service) UpsertSeasonMonitors(itemID uint, monitors []SeasonMonitorInput) error {
	existing, err := s.store.ListSeasonMonitorsByMediaItem(itemID)
	if err != nil {
		return err
	}
	lookup := make(map[int]*store.SeasonMonitor, len(existing))
	for i := range existing {
		lookup[existing[i].SeasonNumber] = &existing[i]
	}
	for _, sm := range monitors {
		if mon, ok := lookup[sm.SeasonNumber]; ok {
			mon.Monitored = sm.Monitored
			if err := s.store.UpdateSeasonMonitor(mon); err != nil {
				return err
			}
		} else {
			if err := s.store.CreateSeasonMonitor(&store.SeasonMonitor{
				MediaItemID:  itemID,
				SeasonNumber: sm.SeasonNumber,
				Monitored:    sm.Monitored,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

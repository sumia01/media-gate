package sync

import (
	"fmt"
	"sort"

	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
)

// EpisodeSummary is a single episode enriched with file-presence and download status.
type EpisodeSummary struct {
	Episode        store.Episode
	HasFile        bool
	Monitored      bool
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

	// Build episode monitor lookup: "S{season}E{episode}" → monitored
	epMonitors, _ := s.store.ListEpisodeMonitorsByMediaItem(itemID)
	epMonitorLookup := make(map[string]bool, len(epMonitors))
	for _, em := range epMonitors {
		key := fmt.Sprintf("S%dE%d", em.SeasonNumber, em.EpisodeNumber)
		epMonitorLookup[key] = em.Monitored
	}

	// Build download status lookups with title parsing to distinguish
	// single-episode downloads from actual season packs.
	epDL, epKeyDL, seasonDL, itemDL := resolveDownloadStatuses(downloads)

	// Group episodes by season.
	seasonMap := make(map[int][]EpisodeSummary)
	for _, ep := range episodes {
		key := fmt.Sprintf("S%dE%d", ep.SeasonNumber, ep.EpisodeNumber)
		summary := EpisodeSummary{
			Episode: ep,
			HasFile: fileLookup[key],
		}

		// Resolve monitored: episode-level override > season-level > false.
		if epMon, ok := epMonitorLookup[key]; ok {
			summary.Monitored = epMon
		} else if seasonMon, ok := monitorLookup[ep.SeasonNumber]; ok {
			summary.Monitored = seasonMon
		}

		// Resolve download status: episode-id > episode-key > season-level > item-level.
		if status, ok := epDL[ep.ID]; ok {
			summary.DownloadStatus = status
		} else if status, ok := epKeyDL[key]; ok {
			summary.DownloadStatus = status
		} else if status, ok := seasonDL[ep.SeasonNumber]; ok {
			summary.DownloadStatus = status
		} else if itemDL != "" {
			summary.DownloadStatus = itemDL
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

// dlStatusPriority defines the priority of download statuses for resolution.
// Higher value wins. Completed/failed are excluded (priority 0).
var dlStatusPriority = map[string]int{
	"downloading": 5,
	"pending":     4,
	"downloaded":  3,
	"importing":   2,
	"seeding":     1,
}

// resolveDownloadStatuses buckets downloads into four tiers:
//   - episodeStatus:    keyed by episode DB ID (downloads with EpisodeID set)
//   - episodeKeyStatus: keyed by "S{n}E{n}" (single-episode downloads without EpisodeID,
//     detected via title parsing)
//   - seasonStatus:     keyed by season number (true season packs only)
//   - itemStatus:       fallback for downloads with neither EpisodeID nor SeasonNumber
//
// Within each tier the highest-priority status wins.
func resolveDownloadStatuses(downloads []store.Download) (
	episodeStatus map[uint]string,
	episodeKeyStatus map[string]string,
	seasonStatus map[int]string,
	itemStatus string,
) {
	episodeStatus = make(map[uint]string)
	episodeKeyStatus = make(map[string]string)
	seasonStatus = make(map[int]string)

	for _, dl := range downloads {
		pri := dlStatusPriority[dl.Status]
		if pri == 0 {
			continue // skip completed/failed
		}
		if dl.EpisodeID != nil {
			if cur, ok := episodeStatus[*dl.EpisodeID]; !ok || pri > dlStatusPriority[cur] {
				episodeStatus[*dl.EpisodeID] = dl.Status
			}
		} else if dl.SeasonNumber != nil {
			// Parse title to distinguish single-episode downloads from actual season packs.
			// Downloads created via season search may lack episode_id even for single episodes.
			parsed := fileparse.ParseTorrentSeasonEpisode(dl.Title)
			if parsed.Episode != nil && parsed.EpisodeEnd == nil {
				// Single-episode download — scope status to that episode only.
				key := fmt.Sprintf("S%dE%d", *dl.SeasonNumber, *parsed.Episode)
				if cur, ok := episodeKeyStatus[key]; !ok || pri > dlStatusPriority[cur] {
					episodeKeyStatus[key] = dl.Status
				}
			} else {
				// True season pack (or episode range) — applies to entire season.
				sn := *dl.SeasonNumber
				if cur, ok := seasonStatus[sn]; !ok || pri > dlStatusPriority[cur] {
					seasonStatus[sn] = dl.Status
				}
			}
		} else {
			if dlStatusPriority[itemStatus] < pri {
				itemStatus = dl.Status
			}
		}
	}
	return
}

// SeasonMonitorInput represents a season monitor create/update request.
type SeasonMonitorInput struct {
	SeasonNumber int
	Monitored    bool
}

// UpsertSeasonMonitors creates or updates season monitors for a media item.
// When a season's monitored status changes, episode-level overrides for that
// season are cleared so episodes inherit the new season-level setting.
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
		// Clear episode-level overrides — episodes now inherit from the season setting.
		_ = s.store.DeleteEpisodeMonitorsBySeason(itemID, sm.SeasonNumber)
	}
	return nil
}

// EpisodeMonitorInput represents an episode monitor create/update request.
type EpisodeMonitorInput struct {
	SeasonNumber  int
	EpisodeNumber int
	Monitored     bool
}

// UpsertEpisodeMonitors creates or updates episode monitors for a media item.
func (s *Service) UpsertEpisodeMonitors(itemID uint, monitors []EpisodeMonitorInput) error {
	for _, em := range monitors {
		if err := s.store.UpsertEpisodeMonitor(&store.EpisodeMonitor{
			MediaItemID:   itemID,
			SeasonNumber:  em.SeasonNumber,
			EpisodeNumber: em.EpisodeNumber,
			Monitored:     em.Monitored,
		}); err != nil {
			return err
		}
	}
	return nil
}

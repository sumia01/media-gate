package sync

import "fmt"

// TimelineEpisodeSummary adds series identity to the shared episode summary.
type TimelineEpisodeSummary struct {
	EpisodeSummary
	SeriesTitle string
	PosterPath  string
}

func (s *Service) AssembleEpisodeTimeline(from, to string) ([]TimelineEpisodeSummary, error) {
	rows, err := s.store.ListTimelineEpisodes(from, to)
	if err != nil {
		return nil, err
	}
	items := make([]TimelineEpisodeSummary, len(rows))
	byItem := make(map[uint][]int)
	for i, row := range rows {
		items[i] = TimelineEpisodeSummary{
			EpisodeSummary: EpisodeSummary{Episode: row.Episode, HasFile: row.HasFile, Monitored: row.Monitored},
			SeriesTitle:    row.SeriesTitle,
			PosterPath:     row.PosterPath,
		}
		byItem[row.MediaItemID] = append(byItem[row.MediaItemID], i)
	}
	// Fetch downloads once per series present in this window, never its episode catalog.
	for itemID, indices := range byItem {
		downloads, err := s.store.ListDownloads(&itemID, nil)
		if err != nil {
			return nil, err
		}
		epDL, epKeyDL, seasonDL, itemDL := resolveDownloadStatuses(downloads)
		for _, i := range indices {
			ep := &items[i]
			key := fmt.Sprintf("S%dE%d", ep.Episode.SeasonNumber, ep.Episode.EpisodeNumber)
			if status, ok := epDL[ep.Episode.ID]; ok {
				ep.DownloadStatus = status
			} else if status, ok := epKeyDL[key]; ok {
				ep.DownloadStatus = status
			} else if status, ok := seasonDL[ep.Episode.SeasonNumber]; ok {
				ep.DownloadStatus = status
			} else {
				ep.DownloadStatus = itemDL
			}
		}
	}
	return items, nil
}

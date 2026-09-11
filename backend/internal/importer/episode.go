package importer

import (
	"path/filepath"

	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/store"
)

// singleEpisodeTarget is only a fallback for one unnumbered video. Applying a
// selected episode to every file of a pack/range would fabricate file coverage.
func (s *Service) singleEpisodeTarget(item *store.MediaItem, dl *store.Download, files []qbittorrent.TorrentFile) (*store.Episode, error) {
	if item.MediaType != "series" || dl.EpisodeID == nil {
		return nil, nil
	}
	parsed := fileparse.ParseTorrentSeasonEpisode(dl.Title)
	if parsed.IsSeasonPack() || (parsed.EpisodeEnd != nil && parsed.Episode != nil && *parsed.EpisodeEnd != *parsed.Episode) {
		return nil, nil
	}
	videoCount := 0
	var videoName string
	for _, file := range files {
		if fileparse.IsVideoFile(file.Name) && !fileparse.IsSampleFile(file.Name) && !fileparse.IsJunkFile(file.Name) {
			videoCount++
			videoName = file.Name
		}
	}
	if videoCount != 1 || fileparse.Parse(filepath.Base(videoName)).EpisodeNumber != nil {
		return nil, nil
	}
	episodes, err := s.store.ListEpisodesByMediaItem(item.ID)
	if err != nil {
		return nil, err
	}
	for _, episode := range episodes {
		if episode.ID == *dl.EpisodeID && episode.MediaItemID == item.ID {
			return &episode, nil
		}
	}
	return nil, nil
}

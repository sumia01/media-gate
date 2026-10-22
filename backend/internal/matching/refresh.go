package matching

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/sumia01/media-gate/internal/store"
)

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

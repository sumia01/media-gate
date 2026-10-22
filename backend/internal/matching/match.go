package matching

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/ratelimit"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

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

type matchActor struct {
	userID    uint
	component string
}

type matchVersion struct {
	item     *store.MediaItem
	metadata *store.MediaMetadata
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

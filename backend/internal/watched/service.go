package watched

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

var (
	ErrForbidden         = errors.New("watched item belongs to another user")
	ErrInvalidIdentity   = errors.New("invalid watched identity")
	ErrInvalidMode       = errors.New("invalid watched list mode")
	ErrMediaItemMismatch = errors.New("media item does not match watched identity")
)

const (
	modeGlobal  = "global"
	modePerUser = "per_user"
)

type ActivityPublisher interface {
	Publish(eventbus.EventType, any)
}

type Service struct {
	store      store.Store
	publisher  ActivityPublisher
	mutationMu sync.Mutex
}

func NewService(st store.Store, publisher ActivityPublisher) *Service {
	return &Service{store: st, publisher: publisher}
}

func (s *Service) SetActivityPublisher(publisher ActivityPublisher) {
	s.publisher = publisher
}

func (s *Service) List(actorUserID uint) ([]store.WatchedItem, error) {
	mode, err := watchedListMode(s.store)
	if err != nil {
		return nil, err
	}
	if mode == modePerUser {
		if actorUserID == 0 {
			return nil, store.ErrActivityActorNotFound
		}
		return s.store.ListWatchedItemsByUser(actorUserID)
	}
	return s.store.ListWatchedItems()
}

func (s *Service) Check(actorUserID uint, source, mediaType string, externalID int) (*store.WatchedItem, error) {
	if err := validateIdentity(source, mediaType, externalID); err != nil {
		return nil, err
	}
	mode, err := watchedListMode(s.store)
	if err != nil {
		return nil, err
	}
	var lookupUser *uint
	if mode == modePerUser {
		if actorUserID == 0 {
			return nil, store.ErrActivityActorNotFound
		}
		lookupUser = &actorUserID
	}
	return s.store.GetWatchedBySourceExternal(lookupUser, source, mediaType, externalID)
}

func (s *Service) Create(actorUserID uint, candidate *store.WatchedItem) (*store.WatchedItem, error) {
	if candidate == nil {
		return nil, ErrMediaItemMismatch
	}
	if err := validateIdentity(candidate.Source, candidate.MediaType, candidate.ExternalID); err != nil {
		return nil, err
	}

	// watched_list_mode makes uniqueness conditional, so serialize the exact
	// check-and-mutate within this single-binary process as well as using a DB tx.
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	item := *candidate
	item.ID = 0
	item.UserID = actorUserID
	item.WatchedAt = time.Now().UTC()

	var publishedMediaItemID uint
	err := s.store.WithTx(func(tx store.Store) error {
		if err := validateActor(tx, actorUserID); err != nil {
			return err
		}
		mode, err := watchedListMode(tx)
		if err != nil {
			return err
		}

		var lookupUser *uint
		if mode == modePerUser {
			lookupUser = &actorUserID
		}
		if _, err := tx.GetWatchedBySourceExternal(lookupUser, item.Source, item.MediaType, item.ExternalID); err == nil {
			return store.ErrDuplicate
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		linked, err := resolveMediaItem(tx, &item)
		if err != nil {
			return err
		}
		if linked == nil {
			item.MediaItemID = nil
		} else {
			linkedID := linked.ID
			item.MediaItemID = &linkedID
		}

		if err := tx.CreateWatchedItem(&item); err != nil {
			return err
		}
		if linked == nil {
			return nil
		}

		visibility := activityVisibility(mode)
		if err := appendActivity(tx, linked, actorUserID, store.MediaActivityActionWatchedMarked, visibility); err != nil {
			return err
		}
		if visibility == store.MediaActivityVisibilityShared {
			publishedMediaItemID = linked.ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publishSharedActivity(publishedMediaItemID)
	return &item, nil
}

func (s *Service) Delete(actorUserID, watchedItemID uint) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	var publishedMediaItemID uint
	err := s.store.WithTx(func(tx store.Store) error {
		if err := validateActor(tx, actorUserID); err != nil {
			return err
		}
		mode, err := watchedListMode(tx)
		if err != nil {
			return err
		}

		watchedItem, err := tx.GetWatchedItem(watchedItemID)
		if err != nil {
			return err
		}
		if mode == modePerUser && watchedItem.UserID != actorUserID {
			return ErrForbidden
		}

		items := []store.WatchedItem{*watchedItem}
		if mode == modeGlobal {
			allItems, err := tx.ListWatchedItems()
			if err != nil {
				return err
			}
			for i := range allItems {
				item := &allItems[i]
				if item.ID != watchedItem.ID && sameIdentity(item, watchedItem) {
					items = append(items, *item)
				}
			}
		}

		linkedItems := make(map[uint]*store.MediaItem)
		for i := range items {
			linked, err := exactLinkedMediaItem(tx, &items[i])
			if err != nil {
				return err
			}
			if linked != nil {
				linkedItems[linked.ID] = linked
			}
		}
		for i := range items {
			if err := tx.DeleteWatchedItem(items[i].ID); err != nil {
				return err
			}
		}
		if len(linkedItems) != 1 {
			return nil
		}
		var linked *store.MediaItem
		for _, item := range linkedItems {
			linked = item
		}

		visibility := activityVisibility(mode)
		if err := appendActivity(tx, linked, actorUserID, store.MediaActivityActionWatchedUnmarked, visibility); err != nil {
			return err
		}
		if visibility == store.MediaActivityVisibilityShared {
			publishedMediaItemID = linked.ID
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publishSharedActivity(publishedMediaItemID)
	return nil
}

func validateActor(tx store.Store, actorUserID uint) error {
	if actorUserID == 0 {
		return store.ErrActivityActorNotFound
	}
	if _, err := tx.GetUser(actorUserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ErrActivityActorNotFound
		}
		return err
	}
	return nil
}

func watchedListMode(tx store.Store) (string, error) {
	setting, err := tx.GetSetting(settings.KeyWatchedListMode)
	if errors.Is(err, store.ErrNotFound) {
		return modeGlobal, nil
	}
	if err != nil {
		return "", err
	}
	if setting == nil {
		return "", ErrInvalidMode
	}
	switch setting.Value {
	case modeGlobal, modePerUser:
		return setting.Value, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidMode, setting.Value)
	}
}

func validateIdentity(source, mediaType string, externalID int) error {
	if (source != "tmdb" && source != "tvdb") ||
		(mediaType != "movie" && mediaType != "series") || externalID <= 0 {
		return ErrInvalidIdentity
	}
	return nil
}

func sameIdentity(left, right *store.WatchedItem) bool {
	return left.Source == right.Source && left.MediaType == right.MediaType && left.ExternalID == right.ExternalID
}

func exactLinkedMediaItem(tx store.Store, watchedItem *store.WatchedItem) (*store.MediaItem, error) {
	if watchedItem.MediaItemID == nil {
		return nil, nil
	}
	item, err := tx.GetMediaItem(*watchedItem.MediaItemID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	metadata, err := tx.GetMediaMetadataByMediaItem(item.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if metadata.Source != watchedItem.Source || metadata.ExternalID != watchedItem.ExternalID || item.MediaType != watchedItem.MediaType {
		return nil, nil
	}
	return item, nil
}

func resolveMediaItem(tx store.Store, watchedItem *store.WatchedItem) (*store.MediaItem, error) {
	if watchedItem.MediaItemID != nil {
		item, err := tx.GetMediaItem(*watchedItem.MediaItemID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrMediaItemMismatch
			}
			return nil, err
		}
		metadata, err := tx.GetMediaMetadataByMediaItem(item.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrMediaItemMismatch
			}
			return nil, err
		}
		if metadata.Source != watchedItem.Source || metadata.ExternalID != watchedItem.ExternalID || item.MediaType != watchedItem.MediaType {
			return nil, ErrMediaItemMismatch
		}
		return item, nil
	}

	metadata, err := tx.ListMediaMetadataExternalIDs()
	if err != nil {
		return nil, err
	}
	var mediaItemID uint
	matches := 0
	for _, match := range metadata {
		if match.Source == watchedItem.Source && match.MediaType == watchedItem.MediaType && match.ExternalID == watchedItem.ExternalID {
			mediaItemID = match.MediaItemID
			matches++
			if matches > 1 {
				return nil, nil
			}
		}
	}
	if matches == 0 {
		return nil, nil
	}
	item, err := tx.GetMediaItem(mediaItemID)
	if err != nil {
		return nil, err
	}
	if item.MediaType != watchedItem.MediaType {
		return nil, fmt.Errorf("%w: media type changed during lookup", ErrMediaItemMismatch)
	}
	return item, nil
}

func appendActivity(tx store.Store, item *store.MediaItem, actorUserID uint, action, visibility string) error {
	operationID, err := store.NewMediaActivityOperationID()
	if err != nil {
		return err
	}
	activity, err := store.NewUserMediaActivity(item, actorUserID, action, operationID, visibility, store.MediaActivityDetails{
		Target: &store.MediaActivityTarget{Scope: store.MediaActivityScopeMedia},
	})
	if err != nil {
		return err
	}
	return tx.AppendMediaActivity(activity)
}

func activityVisibility(mode string) string {
	if mode == modePerUser {
		return store.MediaActivityVisibilityActorOnly
	}
	return store.MediaActivityVisibilityShared
}

func (s *Service) publishSharedActivity(mediaItemID uint) {
	if mediaItemID != 0 && s.publisher != nil {
		s.publisher.Publish(eventbus.MediaActivityAdded, eventbus.MediaActivityPayload{MediaItemID: mediaItemID})
	}
}

package plexrefresh

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/plex"
	"github.com/sumia01/media-gate/internal/store"
)

// Service listens for ImportCompleted events and triggers Plex library scans.
type Service struct {
	provider *plex.Provider
	store    store.Store
	logger   *slog.Logger
}

// NewService creates a new Plex refresh service.
func NewService(provider *plex.Provider, s store.Store, logger *slog.Logger) *Service {
	return &Service{
		provider: provider,
		store:    s,
		logger:   logger,
	}
}

// HandleImportCompleted is the event handler triggered when an import completes.
// It looks up the Plex section mapping for the library and triggers a refresh.
func (s *Service) HandleImportCompleted(e eventbus.Event) {
	payload, ok := e.Payload.(eventbus.ImportPayload)
	if !ok {
		return
	}

	item, err := s.store.GetMediaItem(payload.MediaItemID)
	if err != nil {
		s.logger.Warn("plex refresh: failed to get media item", "mediaItemId", payload.MediaItemID, "error", err)
		return
	}

	// Look up the plex section mapping for this library.
	key := fmt.Sprintf("plex:mapping:%d", item.LibraryID)
	setting, err := s.store.GetSetting(key)
	if err != nil || setting.Value == "" {
		// No mapping configured — skip silently.
		return
	}
	sectionID := setting.Value

	go s.refreshWithRetry(sectionID, item.LibraryID)
}

// refreshWithRetry triggers a Plex refresh with up to 3 retries using exponential backoff.
func (s *Service) refreshWithRetry(sectionID string, libraryID uint) {
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			time.Sleep(backoff)
		}

		client, err := s.provider.Client()
		if err != nil {
			s.logger.Warn("plex refresh: failed to get client", "attempt", attempt, "error", err)
			continue
		}

		if err := client.RefreshSection(sectionID); err != nil {
			s.logger.Warn("plex refresh: failed", "sectionId", sectionID, "libraryId", libraryID, "attempt", attempt, "error", err)
			continue
		}

		s.logger.Info("plex refresh triggered", "sectionId", sectionID, "libraryId", libraryID)
		return
	}

	s.logger.Error("plex refresh: all retries exhausted", "sectionId", sectionID, "libraryId", libraryID)
}

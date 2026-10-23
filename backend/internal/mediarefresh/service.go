package mediarefresh

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/store"
)

// debounceWindow coalesces a burst of filesystem changes affecting the same
// library (e.g. deleting a download whose release folder also drops several
// subtitle files) into a single media-server library scan.
const debounceWindow = 3 * time.Second

// Service listens for library content changes — imports and deletions — and
// triggers a mapped media-server library scan.
type Service struct {
	config Config
	store  store.Store
	logger *slog.Logger

	debounce time.Duration
	mu       sync.Mutex
	timers   map[uint]*time.Timer // libraryID -> pending debounced refresh
}

// Config supplies each server's mapping namespace and current refresh policy.
type Config struct {
	Name          string
	MappingPrefix string
	Enabled       func() bool
	Refresh       func(string) error
}

// NewService shares debounce/event handling across independently configured servers.
func NewService(s store.Store, logger *slog.Logger, config Config) *Service {
	return &Service{
		config:   config,
		store:    s,
		logger:   logger.With("server", config.Name),
		debounce: debounceWindow,
		timers:   make(map[uint]*time.Timer),
	}
}

// HandleImportCompleted refreshes the mapped library after files are imported.
func (s *Service) HandleImportCompleted(e eventbus.Event) {
	payload, ok := e.Payload.(eventbus.ImportPayload)
	if !ok {
		return
	}
	s.refreshForMediaItem(payload.MediaItemID)
}

// HandleMediaItemDeleted refreshes the mapped library after a media item and its
// files are removed from disk.
func (s *Service) HandleMediaItemDeleted(e eventbus.Event) {
	payload, ok := e.Payload.(eventbus.MediaItemPayload)
	if !ok {
		return
	}
	// The media item is already gone from the DB by the time this fires, so use
	// the library id carried on the payload rather than looking the item up.
	s.scheduleRefresh(payload.LibraryID)
}

// HandleSubtitleDeleted refreshes the mapped library after a subtitle file is
// removed from disk.
func (s *Service) HandleSubtitleDeleted(e eventbus.Event) {
	payload, ok := e.Payload.(eventbus.SubtitlePayload)
	if !ok {
		return
	}
	s.refreshForMediaItem(payload.MediaItemID)
}

// HandleDownloadDeleted refreshes the mapped library after a download's imported
// files are removed from disk.
func (s *Service) HandleDownloadDeleted(e eventbus.Event) {
	payload, ok := e.Payload.(eventbus.DownloadPayload)
	if !ok {
		return
	}
	s.refreshForMediaItem(payload.MediaItemID)
}

// refreshForMediaItem resolves the owning library for a media item and schedules
// a section refresh for it.
func (s *Service) refreshForMediaItem(mediaItemID uint) {
	if !s.config.Enabled() {
		return
	}
	item, err := s.store.GetMediaItem(mediaItemID)
	if err != nil {
		s.logger.Warn("media server refresh: failed to get media item", "mediaItemId", mediaItemID, "error", err)
		return
	}
	s.scheduleRefresh(item.LibraryID)
}

// scheduleRefresh debounces refresh requests per library so a burst of file
// changes results in a single scan. The mapping lookup and the
// scan itself run when the timer fires, off the event-dispatch goroutine.
func (s *Service) scheduleRefresh(libraryID uint) {
	if libraryID == 0 || !s.config.Enabled() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if t := s.timers[libraryID]; t != nil {
		t.Stop()
	}

	var t *time.Timer
	t = time.AfterFunc(s.debounce, func() {
		s.mu.Lock()
		// A newer event may have replaced this timer after it fired but before
		// we acquired the lock; if so, let the newer one win and bail out.
		if s.timers[libraryID] != t {
			s.mu.Unlock()
			return
		}
		delete(s.timers, libraryID)
		s.mu.Unlock()

		s.refreshLibrary(libraryID)
	})
	s.timers[libraryID] = t
}

// refreshLibrary looks up the server section mapped to a library and triggers a
// scan. It is a no-op when no mapping is configured.
func (s *Service) refreshLibrary(libraryID uint) {
	if !s.config.Enabled() {
		return
	}
	key := fmt.Sprintf("%s%d", s.config.MappingPrefix, libraryID)
	setting, err := s.store.GetSetting(key)
	if err != nil || setting.Value == "" {
		// No mapping configured — skip silently.
		return
	}
	s.refreshWithRetry(setting.Value, libraryID)
}

// refreshWithRetry triggers a scan with up to 3 retries using exponential backoff.
func (s *Service) refreshWithRetry(sectionID string, libraryID uint) {
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			time.Sleep(backoff)
		}

		// Recheck after debounce/backoff: disabling also suppresses pending work.
		if !s.config.Enabled() {
			return
		}

		if err := s.config.Refresh(sectionID); err != nil {
			s.logger.Warn("media server refresh: failed", "sectionId", sectionID, "libraryId", libraryID, "attempt", attempt)
			continue
		}

		s.logger.Info("media server refresh triggered", "sectionId", sectionID, "libraryId", libraryID)
		return
	}

	s.logger.Error("media server refresh: all retries exhausted", "sectionId", sectionID, "libraryId", libraryID)
}

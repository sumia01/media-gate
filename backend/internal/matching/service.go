package matching

import (
	"errors"
	"net/http"
	"sync"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const (
	autoMatchThreshold = 0.8
	tmdbPosterBase     = "https://image.tmdb.org/t/p/w500"
)

var (
	ErrNoRequestedScope      = errors.New("select at least one season or episode")
	ErrStaleMatchCandidate   = errors.New("stale match candidate")
	ErrStaleRefreshCandidate = errors.New("stale metadata refresh candidate")
)

// StatusRecalculator recalculates a media item's status based on current files
// and episodes. Implemented by sync.Service; defined here to avoid circular imports.
type StatusRecalculator interface {
	RecalcMediaItemStatus(itemID uint) error
}

type Service struct {
	store        store.Store
	settings     *settings.Service
	posterDir    string
	httpClient   *http.Client
	statusRecalc StatusRecalculator
	bus          *eventbus.Bus
	posterMu     *sync.Mutex
	requestMu    *sync.Mutex

	tmdbMu     sync.Mutex
	tmdbKey    string
	tmdbCached *tmdb.Client

	tvdbMu     sync.Mutex
	tvdbKey    string
	tvdbCached *tvdb.Client
}

func NewService(s store.Store, set *settings.Service, posterDir string, httpClient *http.Client) *Service {
	return &Service{
		store:      s,
		settings:   set,
		posterDir:  posterDir,
		httpClient: httpClient,
		posterMu:   &sync.Mutex{},
		requestMu:  &sync.Mutex{},
	}
}

// SetStatusRecalculator injects the status recalculator after construction
// to break the init cycle between matching and sync packages.
func (s *Service) SetStatusRecalculator(r StatusRecalculator) {
	s.statusRecalc = r
}

// SetBus injects the event bus for publishing media item events.
func (s *Service) SetBus(b *eventbus.Bus) {
	s.bus = b
}

// WithStore returns a shallow copy of the Service that uses the given store.
// Useful for running operations inside a database transaction.
func (s *Service) WithStore(st store.Store) *Service {
	return &Service{
		store:        st,
		settings:     s.settings,
		posterDir:    s.posterDir,
		httpClient:   s.httpClient,
		statusRecalc: s.statusRecalc,
		bus:          s.bus,
		posterMu:     s.posterMu,
		requestMu:    s.requestMu,
		tmdbKey:      s.tmdbKey,
		tmdbCached:   s.tmdbCached,
		tvdbKey:      s.tvdbKey,
		tvdbCached:   s.tvdbCached,
	}
}

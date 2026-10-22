package matching

import (
	"fmt"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/settings"
)

func (s *Service) resolveAPIKey(source string) (string, error) {
	switch source {
	case "tmdb":
		return s.settings.Get(settings.KeyTMDBApiKey)
	case "tvdb":
		return s.settings.Get(settings.KeyTVDBApiKey)
	default:
		return "", fmt.Errorf("unknown source: %s", source)
	}
}

// TMDBClient returns a cached TMDB client configured with the current API key,
// or nil if no key is configured.
func (s *Service) TMDBClient() *tmdb.Client {
	apiKey, err := s.settings.Get(settings.KeyTMDBApiKey)
	if err != nil || apiKey == "" {
		return nil
	}
	return s.cachedTMDB(apiKey)
}

// TVDBClient returns a cached TVDB client configured with the current API key,
// or nil if no key is configured.
func (s *Service) TVDBClient() *tvdb.Client {
	apiKey, err := s.settings.Get(settings.KeyTVDBApiKey)
	if err != nil || apiKey == "" {
		return nil
	}
	return s.cachedTVDB(apiKey)
}

// cachedTMDB returns a cached TMDB client for the given key, re-creating if the key changed.
func (s *Service) cachedTMDB(apiKey string) *tmdb.Client {
	s.tmdbMu.Lock()
	defer s.tmdbMu.Unlock()
	if s.tmdbCached == nil || s.tmdbKey != apiKey {
		s.tmdbKey = apiKey
		s.tmdbCached = tmdb.NewClient(apiKey, s.httpClient)
	}
	return s.tmdbCached
}

// cachedTVDB returns a cached TVDB client for the given key, re-creating if the key changed.
func (s *Service) cachedTVDB(apiKey string) *tvdb.Client {
	s.tvdbMu.Lock()
	defer s.tvdbMu.Unlock()
	if s.tvdbCached == nil || s.tvdbKey != apiKey {
		s.tvdbKey = apiKey
		s.tvdbCached = tvdb.NewClient(apiKey, s.httpClient)
	}
	return s.tvdbCached
}

func (s *Service) rateLimitKey(source string) string {
	if source == "tvdb" {
		return settings.KeyTVDBRateLimit
	}
	return settings.KeyTMDBRateLimit
}

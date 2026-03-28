package settings

import (
	"fmt"
	"strings"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/store"
)

const (
	KeyTMDBApiKey           = "tmdb_api_key"
	KeyTVDBApiKey           = "tvdb_api_key"
	KeyMetadataPrimarySource = "metadata_primary_source"
	KeyTMDBRateLimit        = "tmdb_rate_limit"
	KeyTVDBRateLimit        = "tvdb_rate_limit"
)

var sensitiveKeys = map[string]bool{
	KeyTMDBApiKey: true,
	KeyTVDBApiKey: true,
}

type KeyValue struct {
	Key   string
	Value string
}

type Service struct {
	store store.Store
}

func NewService(s store.Store) *Service {
	return &Service{store: s}
}

func (s *Service) List() ([]store.Setting, error) {
	settings, err := s.store.ListSettings()
	if err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}
	for i := range settings {
		if settings[i].Sensitive {
			settings[i].Value = maskValue(settings[i].Value)
		}
	}
	return settings, nil
}

func (s *Service) Update(items []KeyValue) error {
	for _, item := range items {
		setting := &store.Setting{
			Key:       item.Key,
			Value:     item.Value,
			Sensitive: sensitiveKeys[item.Key],
		}
		if err := s.store.SetSetting(setting); err != nil {
			return fmt.Errorf("saving setting %q: %w", item.Key, err)
		}
	}
	return nil
}

func (s *Service) Get(key string) (string, error) {
	setting, err := s.store.GetSetting(key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (s *Service) GetWithDefault(key, defaultValue string) string {
	val, err := s.Get(key)
	if err != nil {
		return defaultValue
	}
	return val
}

func (s *Service) TestTMDB(apiKey *string) (bool, string, error) {
	key, err := s.resolveKey(apiKey, KeyTMDBApiKey)
	if err != nil {
		return false, "TMDB API key not configured", nil
	}
	client := tmdb.NewClient(key)
	if err := client.TestConnection(); err != nil {
		return false, fmt.Sprintf("Connection failed: %v", err), nil
	}
	return true, "Connection successful", nil
}

func (s *Service) TestTVDB(apiKey *string) (bool, string, error) {
	key, err := s.resolveKey(apiKey, KeyTVDBApiKey)
	if err != nil {
		return false, "TVDB API key not configured", nil
	}
	client := tvdb.NewClient(key)
	if err := client.TestConnection(); err != nil {
		return false, fmt.Sprintf("Connection failed: %v", err), nil
	}
	return true, "Connection successful", nil
}

// resolveKey returns the provided key if non-empty, otherwise falls back to the saved value.
func (s *Service) resolveKey(provided *string, settingKey string) (string, error) {
	if provided != nil && strings.TrimSpace(*provided) != "" {
		return *provided, nil
	}
	return s.Get(settingKey)
}

func maskValue(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

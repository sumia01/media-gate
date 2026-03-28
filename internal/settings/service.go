package settings

import (
	"fmt"
	"strings"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/store"
)

const (
	KeyTMDBApiKey = "tmdb_api_key"
	KeyTVDBApiKey = "tvdb_api_key"
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

func (s *Service) TestTMDB(apiKey string) (bool, string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return false, "TMDB API key is empty", nil
	}
	client := tmdb.NewClient(apiKey)
	if err := client.TestConnection(); err != nil {
		return false, fmt.Sprintf("Connection failed: %v", err), nil
	}
	return true, "Connection successful", nil
}

func (s *Service) TestTVDB(apiKey string) (bool, string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return false, "TVDB API key is empty", nil
	}
	client := tvdb.NewClient(apiKey)
	if err := client.TestConnection(); err != nil {
		return false, fmt.Sprintf("Connection failed: %v", err), nil
	}
	return true, "Connection successful", nil
}

func maskValue(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

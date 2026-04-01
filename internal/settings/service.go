package settings

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/store"
)

const (
	KeyTMDBApiKey            = "tmdb_api_key"
	KeyTVDBApiKey            = "tvdb_api_key"
	KeyMetadataPrimarySource = "metadata_primary_source"
	KeyTMDBRateLimit         = "tmdb_rate_limit"
	KeyTVDBRateLimit         = "tvdb_rate_limit"
	KeyQBitURL               = "qbit_url"
	KeyQBitUsername           = "qbit_username"
	KeyQBitPassword           = "qbit_password"
	KeyQBitDownloadPath       = "qbit_download_path"
	KeyQBitCategory           = "qbit_category"
	KeyMonitorSeasonPackPref  = "monitor_season_pack_preference"
)

var sensitiveKeys = map[string]bool{
	KeyTMDBApiKey:   true,
	KeyTVDBApiKey:   true,
	KeyQBitPassword: true,
}

type KeyValue struct {
	Key   string
	Value string
}

var (
	ErrDownloadPathConflict = errors.New("download path is already used by a library")
	ErrDownloadPathOutside  = errors.New("download path must be within the configured base path")
)

type Service struct {
	store    store.Store
	basePath string
}

func NewService(s store.Store, basePath string) *Service {
	return &Service{store: s, basePath: filepath.Clean(basePath)}
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
		if item.Key == KeyQBitDownloadPath {
			if err := s.validateDownloadPath(item.Value); err != nil {
				return err
			}
		}
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

func (s *Service) TestQBit(urlVal, username, password *string) (bool, string, error) {
	u, err := s.resolveKey(urlVal, KeyQBitURL)
	if err != nil {
		return false, "qBittorrent URL not configured", nil
	}
	user, err := s.resolveKey(username, KeyQBitUsername)
	if err != nil {
		return false, "qBittorrent username not configured", nil
	}
	pass, err := s.resolveKey(password, KeyQBitPassword)
	if err != nil {
		return false, "qBittorrent password not configured", nil
	}
	client := qbittorrent.NewClient(u, user, pass)
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

// validateDownloadPath ensures the path is within basePath and not used by any library.
func (s *Service) validateDownloadPath(path string) error {
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, s.basePath+string(filepath.Separator)) && clean != s.basePath {
		return ErrDownloadPathOutside
	}
	libs, err := s.store.ListLibraries()
	if err != nil {
		return fmt.Errorf("checking library paths: %w", err)
	}
	for _, lib := range libs {
		if filepath.Clean(lib.Path) == clean {
			return ErrDownloadPathConflict
		}
	}
	return nil
}

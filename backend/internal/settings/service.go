package settings

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sumia01/media-gate/internal/crypto"
	"github.com/sumia01/media-gate/internal/integration/discord"
	"github.com/sumia01/media-gate/internal/integration/flaresolverr"
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
	KeyQBitSavePath           = "qbit_save_path"
	KeyQBitCategory           = "qbit_category"
	KeyMonitorSeasonPackPref  = "monitor_season_pack_preference"

	KeyFlareSolverrURL     = "flaresolverr_url"
	KeyDiscordWebhookURL = "discord_webhook_url"

	KeyWorkerMonitorInterval         = "worker_monitor_interval"
	KeyWorkerDownloadInterval        = "worker_download_interval"
	KeyWorkerImporterInterval        = "worker_importer_interval"
	KeyWorkerMetadataRefreshInterval = "worker_metadata_refresh_interval"

	KeyGlobalExcludeTags = "global_exclude_tags"

	KeyWatchedListMode = "watched_list_mode"

	KeyLibraryBasePath     = "library_basepath"
	KeyOnboardingStep      = "onboarding_step"
	KeyOnboardingCompleted = "onboarding_completed"
)

var sensitiveKeys = map[string]bool{
	KeyTMDBApiKey:        true,
	KeyTVDBApiKey:        true,
	KeyQBitPassword:      true,
	KeyDiscordWebhookURL: true,
}

func isSensitiveKey(key string) bool {
	if sensitiveKeys[key] {
		return true
	}
	return strings.HasPrefix(key, "indexer:")
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
	store        store.Store
	basePath     string
	envFallbacks map[string]string
	cipher       *crypto.Cipher
	subscribers  []chan string
	mu           sync.Mutex
}

func NewService(s store.Store, basePath string, envFallbacks map[string]string, secretKey string) *Service {
	fb := make(map[string]string, len(envFallbacks))
	for k, v := range envFallbacks {
		if v != "" {
			fb[k] = v
		}
	}
	svc := &Service{store: s, basePath: filepath.Clean(basePath), envFallbacks: fb}

	if secretKey != "" {
		key := crypto.DeriveKey(secretKey)
		c, err := crypto.NewCipher(key)
		if err != nil {
			slog.Error("failed to initialize encryption cipher", "error", err)
		} else {
			svc.cipher = c
		}
	} else {
		slog.Warn("MEDIAGATE_SECRET_KEY not set; sensitive settings stored in plaintext")
	}

	return svc
}

// Subscribe returns a channel that receives the key name whenever a setting is updated.
func (s *Service) Subscribe() <-chan string {
	ch := make(chan string, 16)
	s.mu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.mu.Unlock()
	return ch
}

func (s *Service) notify(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- key:
		default:
		}
	}
}

func (s *Service) List() ([]store.Setting, error) {
	settings, err := s.store.ListSettings()
	if err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}
	filtered := settings[:0]
	for i := range settings {
		// Indexer secrets are internal — not shown on the settings page.
		if strings.HasPrefix(settings[i].Key, "indexer:") {
			continue
		}
		if settings[i].Sensitive && s.cipher != nil {
			if decrypted, err := s.cipher.Decrypt(settings[i].Value); err == nil {
				settings[i].Value = decrypted
			}
		}
		if settings[i].Sensitive {
			settings[i].Value = maskValue(settings[i].Value)
		}
		filtered = append(filtered, settings[i])
	}
	return filtered, nil
}

func (s *Service) Update(items []KeyValue) error {
	for _, item := range items {
		if item.Key == KeyQBitDownloadPath {
			if err := s.validateDownloadPath(item.Value); err != nil {
				return err
			}
		}
		if (item.Key == KeyFlareSolverrURL || item.Key == KeyQBitURL || item.Key == KeyDiscordWebhookURL) && item.Value != "" {
			if err := validateURL(item.Value); err != nil {
				return fmt.Errorf("invalid URL for %s: %w", item.Key, err)
			}
		}
		// Clearing the Discord webhook URL deletes the row so List() won't
		// return a masked "****" placeholder for an empty value.
		if item.Key == KeyDiscordWebhookURL && item.Value == "" {
			if err := s.store.DeleteSetting(item.Key); err != nil {
				return fmt.Errorf("deleting setting %q: %w", item.Key, err)
			}
			s.notify(item.Key)
			continue
		}
		sensitive := isSensitiveKey(item.Key)
		value := item.Value
		if sensitive && s.cipher != nil {
			encrypted, err := s.cipher.Encrypt(value)
			if err != nil {
				return fmt.Errorf("encrypting setting %q: %w", item.Key, err)
			}
			value = encrypted
		}
		setting := &store.Setting{
			Key:       item.Key,
			Value:     value,
			Sensitive: sensitive,
		}
		if err := s.store.SetSetting(setting); err != nil {
			return fmt.Errorf("saving setting %q: %w", item.Key, err)
		}
		s.notify(item.Key)
	}
	return nil
}

func (s *Service) Get(key string) (string, error) {
	setting, err := s.store.GetSetting(key)
	if err == nil {
		if setting.Sensitive && s.cipher != nil {
			decrypted, err := s.cipher.Decrypt(setting.Value)
			if err != nil {
				return "", fmt.Errorf("decrypting setting %q: %w", key, err)
			}
			return decrypted, nil
		}
		return setting.Value, nil
	}
	if v, ok := s.envFallbacks[key]; ok {
		return v, nil
	}
	return "", err
}

// HasEnvFallback reports whether a setting key has a non-empty environment variable fallback.
func (s *Service) HasEnvFallback(key string) bool {
	_, ok := s.envFallbacks[key]
	return ok
}

// BasePath returns the resolved library base path (DB setting, then env fallback).
func (s *Service) BasePath() string {
	val, err := s.Get(KeyLibraryBasePath)
	if err != nil || val == "" {
		return s.basePath
	}
	return filepath.Clean(val)
}

func (s *Service) GetWithDefault(key, defaultValue string) string {
	val, err := s.Get(key)
	if err != nil {
		return defaultValue
	}
	return val
}

func (s *Service) GetDurationWithDefault(key string, defaultVal time.Duration) time.Duration {
	val, err := s.Get(key)
	if err != nil {
		return defaultVal
	}
	seconds, err := strconv.Atoi(val)
	if err != nil || seconds <= 0 {
		return defaultVal
	}
	return time.Duration(seconds) * time.Second
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

func (s *Service) TestFlareSolverr(urlVal *string) (bool, string, error) {
	u, err := s.resolveKey(urlVal, KeyFlareSolverrURL)
	if err != nil {
		return false, "FlareSolverr URL not configured", nil
	}

	return flaresolverr.TestConnection(u)
}

func (s *Service) TestDiscord(urlVal *string) (bool, string, error) {
	u, err := s.resolveKey(urlVal, KeyDiscordWebhookURL)
	if err != nil {
		return false, "Discord webhook URL not configured", nil
	}

	return discord.TestConnection(u)
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

// --- Indexer secret helpers ---

func indexerSecretKey(indexerID uint, fieldName string) string {
	return fmt.Sprintf("indexer:%d:%s", indexerID, fieldName)
}

// SetIndexerSecret stores a single indexer credential in the settings table.
func (s *Service) SetIndexerSecret(indexerID uint, fieldName, value string) error {
	return s.Update([]KeyValue{{
		Key:   indexerSecretKey(indexerID, fieldName),
		Value: value,
	}})
}

// GetIndexerSecrets returns all credentials for an indexer as a map of field name → plaintext value.
func (s *Service) GetIndexerSecrets(indexerID uint) (map[string]string, error) {
	prefix := fmt.Sprintf("indexer:%d:", indexerID)
	rows, err := s.store.ListSettingsByPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("listing indexer secrets: %w", err)
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		fieldName := strings.TrimPrefix(row.Key, prefix)
		val := row.Value
		if row.Sensitive && s.cipher != nil {
			decrypted, err := s.cipher.Decrypt(val)
			if err != nil {
				return nil, fmt.Errorf("decrypting %s: %w", row.Key, err)
			}
			val = decrypted
		}
		result[fieldName] = val
	}
	return result, nil
}

// DeleteIndexerSecrets removes all credentials for an indexer.
func (s *Service) DeleteIndexerSecrets(indexerID uint) error {
	prefix := fmt.Sprintf("indexer:%d:", indexerID)
	return s.store.DeleteSettingsByPrefix(prefix)
}

// MigrateEncryption encrypts any existing plaintext sensitive settings.
// It is idempotent — values already encrypted are skipped.
func (s *Service) MigrateEncryption() error {
	if s.cipher == nil {
		return nil
	}
	settings, err := s.store.ListSettings()
	if err != nil {
		return fmt.Errorf("listing settings for encryption migration: %w", err)
	}
	for i := range settings {
		if !settings[i].Sensitive {
			continue
		}
		if crypto.IsEncrypted(settings[i].Value) {
			continue
		}
		encrypted, err := s.cipher.Encrypt(settings[i].Value)
		if err != nil {
			return fmt.Errorf("encrypting %s: %w", settings[i].Key, err)
		}
		settings[i].Value = encrypted
		if err := s.store.SetSetting(&settings[i]); err != nil {
			return fmt.Errorf("saving encrypted %s: %w", settings[i].Key, err)
		}
		slog.Info("encrypted sensitive setting", "key", settings[i].Key)
	}
	return nil
}

// validateDownloadPath ensures the path is within basePath and not used by any library.
func (s *Service) validateDownloadPath(path string) error {
	base := s.BasePath()
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, base+string(filepath.Separator)) && clean != base {
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

// validateURL checks that a URL is well-formed with an http or https scheme.
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL must have a host")
	}
	return nil
}

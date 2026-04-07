package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// settingsStubStore implements store.Store with just enough for settings tests.
type settingsStubStore struct {
	libraries []store.Library
	settings  map[string]*store.Setting
}

func newSettingsStubStore() *settingsStubStore {
	return &settingsStubStore{settings: make(map[string]*store.Setting)}
}

func (s *settingsStubStore) Close() error                              { return nil }
func (s *settingsStubStore) Ping() error                               { return nil }
func (s *settingsStubStore) CreateLibrary(lib *store.Library) error     { return nil }
func (s *settingsStubStore) ListLibraries() ([]store.Library, error)    { return s.libraries, nil }
func (s *settingsStubStore) GetLibrary(uint) (*store.Library, error)    { return nil, store.ErrNotFound }
func (s *settingsStubStore) UpdateLibrary(*store.Library) error         { return nil }
func (s *settingsStubStore) DeleteLibrary(uint) error                   { return nil }

func (s *settingsStubStore) CreateMediaItem(*store.MediaItem) error                     { return nil }
func (s *settingsStubStore) GetMediaItem(uint) (*store.MediaItem, error)                { return nil, nil }
func (s *settingsStubStore) UpdateMediaItem(*store.MediaItem) error                     { return nil }
func (s *settingsStubStore) DeleteMediaItem(uint) error                                 { return nil }
func (s *settingsStubStore) ListMediaItemsByLibrary(uint) ([]store.MediaItem, error)    { return nil, nil }
func (s *settingsStubStore) ListDiskMediaItemsByLibrary(uint) ([]store.MediaItem, error) { return nil, nil }
func (s *settingsStubStore) ListNewMediaItemsByLibrary(uint) ([]store.MediaItem, error) { return nil, nil }
func (s *settingsStubStore) CountMediaItemsByLibrary(uint) (int64, error)               { return 0, nil }
func (s *settingsStubStore) MediaItemExistsByExternalID(uint, string, int) (bool, error) {
	return false, nil
}
func (s *settingsStubStore) ListMonitoredMediaItems() ([]store.MediaItem, error) { return nil, nil }
func (s *settingsStubStore) ListRecentMediaItems(int) ([]store.MediaItem, error) { return nil, nil }

func (s *settingsStubStore) CreateMediaMetadata(*store.MediaMetadata) error { return nil }
func (s *settingsStubStore) GetMediaMetadataByMediaItem(uint) (*store.MediaMetadata, error) {
	return nil, nil
}
func (s *settingsStubStore) UpdateMediaMetadata(*store.MediaMetadata) error    { return nil }
func (s *settingsStubStore) DeleteMediaMetadataByMediaItem(uint) error         { return nil }
func (s *settingsStubStore) ListMediaMetadataByMediaItemIDs([]uint) ([]store.MediaMetadata, error) {
	return nil, nil
}

func (s *settingsStubStore) CreateMediaProfile(*store.MediaProfile) error              { return nil }
func (s *settingsStubStore) GetMediaProfile(uint) (*store.MediaProfile, error)         { return nil, nil }
func (s *settingsStubStore) ListMediaProfiles() ([]store.MediaProfile, error)          { return nil, nil }
func (s *settingsStubStore) UpdateMediaProfile(*store.MediaProfile) error              { return nil }
func (s *settingsStubStore) DeleteMediaProfile(uint) error                             { return nil }

func (s *settingsStubStore) CreateMediaFile(*store.MediaFile) error                    { return nil }
func (s *settingsStubStore) GetMediaFile(uint) (*store.MediaFile, error)               { return nil, nil }
func (s *settingsStubStore) UpdateMediaFile(*store.MediaFile) error                    { return nil }
func (s *settingsStubStore) ListMediaFilesByMediaItem(uint) ([]store.MediaFile, error) { return nil, nil }
func (s *settingsStubStore) ListMediaFilesByLibrary(uint) ([]store.MediaFile, error)   { return nil, nil }
func (s *settingsStubStore) DeleteMediaFile(uint) error                                { return nil }
func (s *settingsStubStore) DeleteMediaFilesByPaths([]string) error                    { return nil }

func (s *settingsStubStore) CreateSeasonMonitor(*store.SeasonMonitor) error { return nil }
func (s *settingsStubStore) ListSeasonMonitorsByMediaItem(uint) ([]store.SeasonMonitor, error) {
	return nil, nil
}
func (s *settingsStubStore) UpdateSeasonMonitor(*store.SeasonMonitor) error { return nil }

func (s *settingsStubStore) ListEpisodeMonitorsByMediaItem(uint) ([]store.EpisodeMonitor, error) {
	return nil, nil
}
func (s *settingsStubStore) UpsertEpisodeMonitor(*store.EpisodeMonitor) error { return nil }
func (s *settingsStubStore) DeleteEpisodeMonitorsBySeason(uint, int) error    { return nil }
func (s *settingsStubStore) DeleteEpisodeMonitorsByMediaItem(uint) error      { return nil }

func (s *settingsStubStore) CreateEpisode(*store.Episode) error                    { return nil }
func (s *settingsStubStore) ListEpisodesByMediaItem(uint) ([]store.Episode, error) { return nil, nil }
func (s *settingsStubStore) DeleteEpisodesByMediaItem(uint) error                  { return nil }

func (s *settingsStubStore) GetSetting(key string) (*store.Setting, error) {
	if v, ok := s.settings[key]; ok {
		return v, nil
	}
	return nil, store.ErrNotFound
}
func (s *settingsStubStore) SetSetting(setting *store.Setting) error {
	s.settings[setting.Key] = setting
	return nil
}
func (s *settingsStubStore) ListSettings() ([]store.Setting, error) {
	result := make([]store.Setting, 0, len(s.settings))
	for _, v := range s.settings {
		result = append(result, *v)
	}
	return result, nil
}
func (s *settingsStubStore) DeleteSetting(key string) error {
	delete(s.settings, key)
	return nil
}
func (s *settingsStubStore) DeleteSettingsByPrefix(prefix string) error {
	for k := range s.settings {
		if strings.HasPrefix(k, prefix) {
			delete(s.settings, k)
		}
	}
	return nil
}
func (s *settingsStubStore) ListSettingsByPrefix(prefix string) ([]store.Setting, error) {
	var result []store.Setting
	for k, v := range s.settings {
		if strings.HasPrefix(k, prefix) {
			result = append(result, *v)
		}
	}
	return result, nil
}

func (s *settingsStubStore) CreateJobRecord(*store.JobRecord) error        { return nil }
func (s *settingsStubStore) ListJobRecords(int) ([]store.JobRecord, error) { return nil, nil }
func (s *settingsStubStore) DeleteOldJobRecords(int) error                 { return nil }
func (s *settingsStubStore) MaxJobRecordID() (uint, error)                 { return 0, nil }

func (s *settingsStubStore) CreateIndexer(*store.Indexer) error      { return nil }
func (s *settingsStubStore) GetIndexer(uint) (*store.Indexer, error) { return nil, nil }
func (s *settingsStubStore) ListIndexers() ([]store.Indexer, error)  { return nil, nil }
func (s *settingsStubStore) UpdateIndexer(*store.Indexer) error      { return nil }
func (s *settingsStubStore) DeleteIndexer(uint) error                { return nil }

func (s *settingsStubStore) CreateDownload(*store.Download) error                   { return nil }
func (s *settingsStubStore) GetDownload(uint) (*store.Download, error)              { return nil, nil }
func (s *settingsStubStore) UpdateDownload(*store.Download) error                   { return nil }
func (s *settingsStubStore) ListDownloads(*uint, *string) ([]store.Download, error) { return nil, nil }
func (s *settingsStubStore) DeleteDownload(uint) error                              { return nil }
func (s *settingsStubStore) WithTx(fn func(store.Store) error) error                { return fn(s) }

func (s *settingsStubStore) CreateUser(*store.User) error                               { return nil }
func (s *settingsStubStore) GetUser(uint) (*store.User, error)                          { return nil, nil }
func (s *settingsStubStore) GetUserByEmail(string) (*store.User, error)                 { return nil, store.ErrNotFound }
func (s *settingsStubStore) ListUsers() ([]store.User, error)                           { return nil, nil }
func (s *settingsStubStore) UpdateUser(*store.User) error                               { return nil }
func (s *settingsStubStore) DeleteUser(uint) error                                      { return nil }
func (s *settingsStubStore) CountUsers() (int64, error)                                 { return 0, nil }
func (s *settingsStubStore) CreateRefreshToken(*store.RefreshToken) error               { return nil }
func (s *settingsStubStore) GetRefreshTokenByToken(string) (*store.RefreshToken, error) { return nil, store.ErrNotFound }
func (s *settingsStubStore) DeleteRefreshToken(string) error                            { return nil }
func (s *settingsStubStore) DeleteRefreshTokensByUser(uint) error                       { return nil }
func (s *settingsStubStore) DeleteExpiredRefreshTokens() error                          { return nil }
func (s *settingsStubStore) CreateWatchedItem(*store.WatchedItem) error                 { return nil }
func (s *settingsStubStore) DeleteWatchedItem(uint) error                              { return nil }
func (s *settingsStubStore) ListWatchedItems() ([]store.WatchedItem, error)            { return nil, nil }
func (s *settingsStubStore) ListWatchedItemsByUser(uint) ([]store.WatchedItem, error)  { return nil, nil }
func (s *settingsStubStore) GetWatchedBySourceExternal(*uint, string, int) (*store.WatchedItem, error) { return nil, store.ErrNotFound }

// --- Existing path validation tests ---

func TestValidateDownloadPath_RejectsTraversal(t *testing.T) {
	basePath := t.TempDir()
	st := newSettingsStubStore()
	svc := NewService(st, basePath, nil, "")

	tests := []struct {
		name string
		path string
	}{
		{"parent directory with ../", basePath + "/../etc"},
		{"parent from subdirectory", basePath + "/subdir/../../etc"},
		{"absolute path outside base", "/etc/passwd"},
		{"root path", "/"},
		{"double dot only", ".."},
		{"relative traversal", "../../../etc/shadow"},
		{"base path prefix trick", basePath + "evil"},
		{"base path prefix with dash", basePath + "-escape/data"},
		{"trailing slash traversal", basePath + "/../"},
		{"dot segments in middle", basePath + "/./../../etc"},
		{"double separator traversal", basePath + "//../../etc"},
		{"empty string", ""},
		{"single dot", "."},
		{"whitespace only", "   "},
		{"tab characters", "\t\t"},
		{"basePath parent", filepath.Dir(basePath)},
		{"basePath parent with trailing sep", filepath.Dir(basePath) + "/"},
		{"basePath truncated name", basePath[:len(basePath)-1]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.validateDownloadPath(tt.path)
			if err != ErrDownloadPathOutside {
				t.Errorf("validateDownloadPath(%q) = %v, want ErrDownloadPathOutside", tt.path, err)
			}
		})
	}
}

func TestValidateDownloadPath_AcceptsValidPaths(t *testing.T) {
	basePath := t.TempDir()
	st := newSettingsStubStore()
	svc := NewService(st, basePath, nil, "")

	tests := []struct {
		name string
		path string
	}{
		{"exact base path", basePath},
		{"subdirectory", basePath + "/downloads"},
		{"nested subdirectory", basePath + "/downloads/complete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.validateDownloadPath(tt.path)
			if err != nil {
				t.Errorf("validateDownloadPath(%q) = %v, want nil", tt.path, err)
			}
		})
	}
}

func TestValidateDownloadPath_RejectsConflictWithLibrary(t *testing.T) {
	basePath := t.TempDir()
	st := newSettingsStubStore()
	st.libraries = []store.Library{
		{ID: 1, Name: "Movies", Path: basePath + "/movies", MediaType: "movie"},
	}
	svc := NewService(st, basePath, nil, "")

	err := svc.validateDownloadPath(basePath + "/movies")
	if err != ErrDownloadPathConflict {
		t.Errorf("validateDownloadPath(library path) = %v, want ErrDownloadPathConflict", err)
	}
}

func TestUpdate_RejectsDownloadPathOutsideBase(t *testing.T) {
	basePath := t.TempDir()
	st := newSettingsStubStore()
	svc := NewService(st, basePath, nil, "")

	attacks := []struct {
		name string
		path string
	}{
		{"traversal via ../", basePath + "/../outside"},
		{"absolute escape", "/tmp/evil"},
		{"prefix trick", basePath + "-evil"},
	}

	for _, tt := range attacks {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Update([]KeyValue{{Key: KeyQBitDownloadPath, Value: tt.path}})
			if err != ErrDownloadPathOutside {
				t.Errorf("Update(download_path=%q) = %v, want ErrDownloadPathOutside", tt.path, err)
			}
		})
	}
}

func TestUpdate_AcceptsDownloadPathInsideBase(t *testing.T) {
	basePath := t.TempDir()
	st := newSettingsStubStore()
	svc := NewService(st, basePath, nil, "")

	err := svc.Update([]KeyValue{{Key: KeyQBitDownloadPath, Value: basePath + "/downloads"}})
	if err != nil {
		t.Errorf("Update(download_path=valid) = %v, want nil", err)
	}
}

// --- Encryption tests ---

func TestEncryptDecryptRoundTrip(t *testing.T) {
	st := newSettingsStubStore()
	svc := NewService(st, "/mnt", nil, "test-secret-key")

	err := svc.Update([]KeyValue{{Key: KeyTMDBApiKey, Value: "my-tmdb-key-1234"}})
	if err != nil {
		t.Fatalf("Update() = %v", err)
	}

	// Raw store value should be encrypted
	raw := st.settings[KeyTMDBApiKey]
	if raw == nil {
		t.Fatal("setting not stored")
	}
	if !strings.HasPrefix(raw.Value, "enc:") {
		t.Errorf("raw value should be encrypted, got %q", raw.Value)
	}
	if !raw.Sensitive {
		t.Error("setting should be marked sensitive")
	}

	// Get should return plaintext
	val, err := svc.Get(KeyTMDBApiKey)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if val != "my-tmdb-key-1234" {
		t.Errorf("Get() = %q, want %q", val, "my-tmdb-key-1234")
	}
}

func TestPlaintextModeWhenNoKey(t *testing.T) {
	st := newSettingsStubStore()
	svc := NewService(st, "/mnt", nil, "")

	err := svc.Update([]KeyValue{{Key: KeyTMDBApiKey, Value: "plain-key"}})
	if err != nil {
		t.Fatalf("Update() = %v", err)
	}

	raw := st.settings[KeyTMDBApiKey]
	if raw.Value != "plain-key" {
		t.Errorf("raw value = %q, want %q (no encryption)", raw.Value, "plain-key")
	}

	val, err := svc.Get(KeyTMDBApiKey)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if val != "plain-key" {
		t.Errorf("Get() = %q, want %q", val, "plain-key")
	}
}

func TestMigrateEncryption(t *testing.T) {
	st := newSettingsStubStore()
	// Pre-populate with plaintext sensitive settings
	st.settings[KeyTMDBApiKey] = &store.Setting{Key: KeyTMDBApiKey, Value: "plain-tmdb", Sensitive: true}
	st.settings[KeyQBitPassword] = &store.Setting{Key: KeyQBitPassword, Value: "plain-pass", Sensitive: true}
	st.settings["qbit_url"] = &store.Setting{Key: "qbit_url", Value: "http://localhost", Sensitive: false}

	svc := NewService(st, "/mnt", nil, "migrate-key")

	err := svc.MigrateEncryption()
	if err != nil {
		t.Fatalf("MigrateEncryption() = %v", err)
	}

	// Sensitive settings should now be encrypted
	if !strings.HasPrefix(st.settings[KeyTMDBApiKey].Value, "enc:") {
		t.Error("tmdb key should be encrypted after migration")
	}
	if !strings.HasPrefix(st.settings[KeyQBitPassword].Value, "enc:") {
		t.Error("qbit password should be encrypted after migration")
	}
	// Non-sensitive should remain plaintext
	if st.settings["qbit_url"].Value != "http://localhost" {
		t.Error("non-sensitive setting should remain unchanged")
	}

	// Verify they still decrypt correctly
	val, err := svc.Get(KeyTMDBApiKey)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if val != "plain-tmdb" {
		t.Errorf("Get() = %q, want %q", val, "plain-tmdb")
	}
}

func TestIndexerSecretRoundTrip(t *testing.T) {
	st := newSettingsStubStore()
	svc := NewService(st, "/mnt", nil, "test-key")

	err := svc.SetIndexerSecret(5, "password", "s3cret")
	if err != nil {
		t.Fatalf("SetIndexerSecret() = %v", err)
	}

	secrets, err := svc.GetIndexerSecrets(5)
	if err != nil {
		t.Fatalf("GetIndexerSecrets() = %v", err)
	}
	if secrets["password"] != "s3cret" {
		t.Errorf("password = %q, want %q", secrets["password"], "s3cret")
	}
}

func TestDeleteIndexerSecrets(t *testing.T) {
	st := newSettingsStubStore()
	svc := NewService(st, "/mnt", nil, "test-key")

	_ = svc.SetIndexerSecret(5, "password", "s3cret")
	_ = svc.SetIndexerSecret(5, "2facode", "123456")

	err := svc.DeleteIndexerSecrets(5)
	if err != nil {
		t.Fatalf("DeleteIndexerSecrets() = %v", err)
	}

	secrets, err := svc.GetIndexerSecrets(5)
	if err != nil {
		t.Fatalf("GetIndexerSecrets() = %v", err)
	}
	if len(secrets) != 0 {
		t.Errorf("expected no secrets after delete, got %v", secrets)
	}
}

func TestListExcludesIndexerSecrets(t *testing.T) {
	st := newSettingsStubStore()
	svc := NewService(st, "/mnt", nil, "test-key")

	// Store a regular setting and an indexer secret
	_ = svc.Update([]KeyValue{{Key: KeyQBitURL, Value: "http://localhost:8080"}})
	_ = svc.SetIndexerSecret(5, "password", "s3cret")

	listed, err := svc.List()
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	for _, s := range listed {
		if strings.HasPrefix(s.Key, "indexer:") {
			t.Errorf("List() should not include indexer secrets, found %q", s.Key)
		}
	}

	// Verify the regular setting is present
	found := false
	for _, s := range listed {
		if s.Key == KeyQBitURL {
			found = true
		}
	}
	if !found {
		t.Error("List() should include regular settings")
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{KeyTMDBApiKey, true},
		{KeyTVDBApiKey, true},
		{KeyQBitPassword, true},
		{KeyQBitURL, false},
		{"indexer:5:password", true},
		{"indexer:10:2facode", true},
		{"random_key", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := isSensitiveKey(tt.key); got != tt.want {
				t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

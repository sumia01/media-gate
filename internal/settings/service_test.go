package settings

import (
	"path/filepath"
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
func (s *settingsStubStore) ListSettings() ([]store.Setting, error) { return nil, nil }
func (s *settingsStubStore) DeleteSetting(string) error             { return nil }

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

func TestValidateDownloadPath_RejectsTraversal(t *testing.T) {
	basePath := t.TempDir()
	st := newSettingsStubStore()
	svc := NewService(st, basePath, nil)

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
	svc := NewService(st, basePath, nil)

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
	svc := NewService(st, basePath, nil)

	err := svc.validateDownloadPath(basePath + "/movies")
	if err != ErrDownloadPathConflict {
		t.Errorf("validateDownloadPath(library path) = %v, want ErrDownloadPathConflict", err)
	}
}

func TestUpdate_RejectsDownloadPathOutsideBase(t *testing.T) {
	basePath := t.TempDir()
	st := newSettingsStubStore()
	svc := NewService(st, basePath, nil)

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
	svc := NewService(st, basePath, nil)

	err := svc.Update([]KeyValue{{Key: KeyQBitDownloadPath, Value: basePath + "/downloads"}})
	if err != nil {
		t.Errorf("Update(download_path=valid) = %v, want nil", err)
	}
}

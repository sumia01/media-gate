package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// stubStore is a minimal store implementation for testing path validation.
// Only CreateLibrary and UpdateLibrary are needed; the rest are stubs.
type stubStore struct {
	created *store.Library
	updated *store.Library
}

func (s *stubStore) Close() error                              { return nil }
func (s *stubStore) Ping() error                               { return nil }
func (s *stubStore) CreateLibrary(lib *store.Library) error     { s.created = lib; return nil }
func (s *stubStore) ListLibraries() ([]store.Library, error)    { return nil, nil }
func (s *stubStore) GetLibrary(uint) (*store.Library, error)    { return nil, store.ErrNotFound }
func (s *stubStore) UpdateLibrary(lib *store.Library) error     { s.updated = lib; return nil }
func (s *stubStore) DeleteLibrary(uint) error                   { return nil }

func (s *stubStore) CreateMediaItem(*store.MediaItem) error                     { return nil }
func (s *stubStore) GetMediaItem(uint) (*store.MediaItem, error)                { return nil, nil }
func (s *stubStore) UpdateMediaItem(*store.MediaItem) error                     { return nil }
func (s *stubStore) DeleteMediaItem(uint) error                                 { return nil }
func (s *stubStore) ListMediaItemsByLibrary(uint) ([]store.MediaItem, error)    { return nil, nil }
func (s *stubStore) ListDiskMediaItemsByLibrary(uint) ([]store.MediaItem, error) { return nil, nil }
func (s *stubStore) ListNewMediaItemsByLibrary(uint) ([]store.MediaItem, error) { return nil, nil }
func (s *stubStore) CountMediaItemsByLibrary(uint) (int64, error)               { return 0, nil }
func (s *stubStore) MediaItemExistsByExternalID(uint, string, int) (bool, error) { return false, nil }
func (s *stubStore) ListMonitoredMediaItems() ([]store.MediaItem, error)         { return nil, nil }
func (s *stubStore) ListRecentMediaItems(int) ([]store.MediaItem, error)         { return nil, nil }

func (s *stubStore) CreateMediaMetadata(*store.MediaMetadata) error                { return nil }
func (s *stubStore) GetMediaMetadataByMediaItem(uint) (*store.MediaMetadata, error) { return nil, nil }
func (s *stubStore) UpdateMediaMetadata(*store.MediaMetadata) error                { return nil }
func (s *stubStore) DeleteMediaMetadataByMediaItem(uint) error                     { return nil }
func (s *stubStore) ListMediaMetadataByMediaItemIDs([]uint) ([]store.MediaMetadata, error) {
	return nil, nil
}

func (s *stubStore) CreateMediaProfile(*store.MediaProfile) error              { return nil }
func (s *stubStore) GetMediaProfile(uint) (*store.MediaProfile, error)         { return nil, nil }
func (s *stubStore) ListMediaProfiles() ([]store.MediaProfile, error)          { return nil, nil }
func (s *stubStore) UpdateMediaProfile(*store.MediaProfile) error              { return nil }
func (s *stubStore) DeleteMediaProfile(uint) error                             { return nil }

func (s *stubStore) CreateMediaFile(*store.MediaFile) error                    { return nil }
func (s *stubStore) GetMediaFile(uint) (*store.MediaFile, error)               { return nil, nil }
func (s *stubStore) UpdateMediaFile(*store.MediaFile) error                    { return nil }
func (s *stubStore) ListMediaFilesByMediaItem(uint) ([]store.MediaFile, error) { return nil, nil }
func (s *stubStore) ListMediaFilesByLibrary(uint) ([]store.MediaFile, error)   { return nil, nil }
func (s *stubStore) DeleteMediaFile(uint) error                                { return nil }
func (s *stubStore) DeleteMediaFilesByPaths([]string) error                    { return nil }

func (s *stubStore) CreateSeasonMonitor(*store.SeasonMonitor) error                    { return nil }
func (s *stubStore) ListSeasonMonitorsByMediaItem(uint) ([]store.SeasonMonitor, error) { return nil, nil }
func (s *stubStore) UpdateSeasonMonitor(*store.SeasonMonitor) error                    { return nil }

func (s *stubStore) CreateEpisode(*store.Episode) error                    { return nil }
func (s *stubStore) ListEpisodesByMediaItem(uint) ([]store.Episode, error) { return nil, nil }
func (s *stubStore) DeleteEpisodesByMediaItem(uint) error                  { return nil }

func (s *stubStore) DeleteEpisodeMonitorsByMediaItem(uint) error                        { return nil }
func (s *stubStore) DeleteEpisodeMonitorsBySeason(uint, int) error                      { return nil }
func (s *stubStore) ListEpisodeMonitorsByMediaItem(uint) ([]store.EpisodeMonitor, error) { return nil, nil }
func (s *stubStore) UpsertEpisodeMonitor(*store.EpisodeMonitor) error                   { return nil }

func (s *stubStore) GetSetting(string) (*store.Setting, error)              { return nil, store.ErrNotFound }
func (s *stubStore) SetSetting(*store.Setting) error                        { return nil }
func (s *stubStore) ListSettings() ([]store.Setting, error)                 { return nil, nil }
func (s *stubStore) DeleteSetting(string) error                             { return nil }
func (s *stubStore) DeleteSettingsByPrefix(string) error                    { return nil }
func (s *stubStore) ListSettingsByPrefix(string) ([]store.Setting, error)   { return nil, nil }

func (s *stubStore) CreateJobRecord(*store.JobRecord) error        { return nil }
func (s *stubStore) ListJobRecords(int) ([]store.JobRecord, error) { return nil, nil }
func (s *stubStore) DeleteOldJobRecords(int) error                 { return nil }
func (s *stubStore) MaxJobRecordID() (uint, error)                 { return 0, nil }

func (s *stubStore) CreateIndexer(*store.Indexer) error              { return nil }
func (s *stubStore) GetIndexer(uint) (*store.Indexer, error)         { return nil, nil }
func (s *stubStore) ListIndexers() ([]store.Indexer, error)          { return nil, nil }
func (s *stubStore) UpdateIndexer(*store.Indexer) error              { return nil }
func (s *stubStore) DeleteIndexer(uint) error                        { return nil }

func (s *stubStore) CreateDownload(*store.Download) error                       { return nil }
func (s *stubStore) GetDownload(uint) (*store.Download, error)                  { return nil, nil }
func (s *stubStore) UpdateDownload(*store.Download) error                       { return nil }
func (s *stubStore) ListDownloads(*uint, *string) ([]store.Download, error)     { return nil, nil }
func (s *stubStore) DeleteDownload(uint) error                                  { return nil }
func (s *stubStore) WithTx(fn func(store.Store) error) error                    { return fn(s) }

func (s *stubStore) CreateUser(*store.User) error                               { return nil }
func (s *stubStore) GetUser(uint) (*store.User, error)                          { return nil, nil }
func (s *stubStore) GetUserByEmail(string) (*store.User, error)                 { return nil, store.ErrNotFound }
func (s *stubStore) ListUsers() ([]store.User, error)                           { return nil, nil }
func (s *stubStore) UpdateUser(*store.User) error                               { return nil }
func (s *stubStore) DeleteUser(uint) error                                      { return nil }
func (s *stubStore) CountUsers() (int64, error)                                 { return 0, nil }
func (s *stubStore) CreateRefreshToken(*store.RefreshToken) error               { return nil }
func (s *stubStore) GetRefreshTokenByToken(string) (*store.RefreshToken, error) { return nil, store.ErrNotFound }
func (s *stubStore) DeleteRefreshToken(string) error                            { return nil }
func (s *stubStore) DeleteRefreshTokensByUser(uint) error                       { return nil }
func (s *stubStore) DeleteExpiredRefreshTokens() error                          { return nil }
func (s *stubStore) CreateWatchedItem(*store.WatchedItem) error                 { return nil }
func (s *stubStore) DeleteWatchedItem(uint) error                              { return nil }
func (s *stubStore) ListWatchedItems() ([]store.WatchedItem, error)            { return nil, nil }
func (s *stubStore) ListWatchedItemsByUser(uint) ([]store.WatchedItem, error)  { return nil, nil }
func (s *stubStore) GetWatchedBySourceExternal(*uint, string, int) (*store.WatchedItem, error) { return nil, store.ErrNotFound }
func (s *stubStore) ClearWatchedMediaItemID(uint) error                                      { return nil }

// staticBasePath implements BasePathProvider for testing.
type staticBasePath string

func (s staticBasePath) BasePath() string { return string(s) }

// newTestService creates a library service with a temp dir as basePath.
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	basePath := t.TempDir()
	svc := NewService(&stubStore{}, staticBasePath(basePath), nil)
	return svc, basePath
}

func TestValidatePath_RejectsTraversal(t *testing.T) {
	svc, basePath := newTestService(t)

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
			err := svc.validatePath(tt.path)
			if err != ErrPathOutsideBase {
				t.Errorf("validatePath(%q) = %v, want ErrPathOutsideBase", tt.path, err)
			}
		})
	}
}

func TestValidatePath_AcceptsValidPaths(t *testing.T) {
	svc, basePath := newTestService(t)

	tests := []struct {
		name string
		path string
	}{
		{"exact base path", basePath},
		{"subdirectory", basePath + "/movies"},
		{"nested subdirectory", basePath + "/movies/action"},
		{"path with spaces", basePath + "/my movies"},
		{"path with dots in name", basePath + "/my.library"},
		{"deeply nested", basePath + "/a/b/c/d/e"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.validatePath(tt.path)
			if err != nil {
				t.Errorf("validatePath(%q) = %v, want nil", tt.path, err)
			}
		})
	}
}

func TestCreate_RejectsPathOutsideBase(t *testing.T) {
	svc, basePath := newTestService(t)

	attacks := []struct {
		name string
		path string
	}{
		{"traversal via ../", basePath + "/../outside"},
		{"absolute escape", "/tmp/evil"},
		{"prefix trick", basePath + "-evil"},
		{"deep traversal", basePath + "/a/b/../../../../etc"},
	}

	for _, tt := range attacks {
		t.Run(tt.name, func(t *testing.T) {
			lib := &store.Library{Name: "test", Path: tt.path, MediaType: "movie"}
			err := svc.Create(lib)
			if err != ErrPathOutsideBase {
				t.Errorf("Create with path %q: got %v, want ErrPathOutsideBase", tt.path, err)
			}
		})
	}
}

func TestUpdate_RejectsPathOutsideBase(t *testing.T) {
	svc, basePath := newTestService(t)

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
			lib := &store.Library{Name: "test", Path: tt.path, MediaType: "movie"}
			err := svc.Update(lib)
			if err != ErrPathOutsideBase {
				t.Errorf("Update with path %q: got %v, want ErrPathOutsideBase", tt.path, err)
			}
		})
	}
}

func TestBrowse_RejectsPathOutsideBase(t *testing.T) {
	svc, basePath := newTestService(t)

	attacks := []struct {
		name string
		path string
	}{
		{"traversal via ../", basePath + "/../"},
		{"absolute escape", "/etc"},
		{"prefix trick", basePath + "-evil"},
		{"deep traversal", basePath + "/a/../../../etc"},
	}

	for _, tt := range attacks {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.Browse(tt.path)
			if err != ErrPathOutsideBase {
				t.Errorf("Browse with path %q: got %v, want ErrPathOutsideBase", tt.path, err)
			}
		})
	}
}

func TestBrowse_DefaultsToBasePathWhenEmpty(t *testing.T) {
	svc, basePath := newTestService(t)

	// Create a subdirectory so Browse has something to return
	subdir := filepath.Join(basePath, "testlib")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	browsedPath, entries, err := svc.Browse("")
	if err != nil {
		t.Fatalf("Browse(\"\") = %v", err)
	}
	if browsedPath != basePath {
		t.Errorf("browsedPath = %q, want %q", browsedPath, basePath)
	}
	if len(entries) == 0 {
		t.Error("expected at least one directory entry")
	}
}

func TestCreate_AcceptsValidPathInsideBase(t *testing.T) {
	svc, basePath := newTestService(t)

	lib := &store.Library{Name: "Movies", Path: basePath + "/movies", MediaType: "movie"}
	err := svc.Create(lib)
	if err != nil {
		t.Errorf("Create with valid path: got %v, want nil", err)
	}
}

// TestTraversalDoesNotCreateFolderOutsideBase is the integration-style test:
// it verifies that no directory is created outside basePath even if an attacker
// tries path traversal via the public API (Create, Update, Browse).
func TestTraversalDoesNotCreateFolderOutsideBase(t *testing.T) {
	basePath := t.TempDir()
	outsideDir := t.TempDir() // a separate temp dir simulating "outside"

	svc := NewService(&stubStore{}, staticBasePath(basePath), nil)

	// Craft paths that attempt to land inside outsideDir
	// Since both are under the OS temp dir, we can build a relative traversal.
	rel, err := filepath.Rel(basePath, outsideDir)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	attackPath := filepath.Join(basePath, rel, "pwned")

	// Attempt Create
	lib := &store.Library{Name: "evil", Path: attackPath, MediaType: "movie"}
	if err := svc.Create(lib); err != ErrPathOutsideBase {
		t.Errorf("Create(attackPath) = %v, want ErrPathOutsideBase", err)
	}

	// Attempt Update
	if err := svc.Update(lib); err != ErrPathOutsideBase {
		t.Errorf("Update(attackPath) = %v, want ErrPathOutsideBase", err)
	}

	// Attempt Browse
	if _, _, err := svc.Browse(attackPath); err != ErrPathOutsideBase {
		t.Errorf("Browse(attackPath) = %v, want ErrPathOutsideBase", err)
	}

	// Verify the "pwned" directory was never created
	pwnedPath := filepath.Join(outsideDir, "pwned")
	if _, err := os.Stat(pwnedPath); !os.IsNotExist(err) {
		t.Errorf("directory %q should not exist, but it does (or stat error: %v)", pwnedPath, err)
	}
}

// TestSymlinkTraversal verifies that if a symlink inside basePath points outside,
// the string-based validation still allows it (documenting current behavior).
// This test documents the known limitation — symlink resolution is not performed.
func TestSymlinkTraversal_DocumentedLimitation(t *testing.T) {
	basePath := t.TempDir()
	outsideDir := t.TempDir()

	// Create a symlink inside basePath that points to outsideDir
	symlinkPath := filepath.Join(basePath, "escape")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Skip("cannot create symlinks on this OS")
	}

	svc := NewService(&stubStore{}, staticBasePath(basePath), nil)

	// The string-based check passes because the path starts with basePath
	err := svc.validatePath(symlinkPath)
	if err != nil {
		t.Fatalf("validatePath(symlink) = %v; current implementation accepts symlinks (known limitation)", err)
	}

	// NOTE: This test documents that symlink-based escapes are NOT caught
	// by the current string-prefix validation. If EvalSymlinks is added
	// in the future, this test should be updated to expect ErrPathOutsideBase.
}

// TestDocumentedLimitations_NotRealTraversals documents edge cases that pass
// validation but are NOT real security issues on Linux:
//
//   - trailing ".." that resolves back INTO basePath (not an escape)
//   - backslash is a legal filename char on Linux, not a path separator
//   - unicode look-alike slashes are literal filename chars, not separators
//   - null bytes in Go strings are literal; Linux rejects them with ENOENT
//
// These tests exist so we are aware of the behavior. If validation is
// tightened in the future (e.g. rejecting non-printable chars), update
// the expectations accordingly.
func TestDocumentedLimitations_NotRealTraversals(t *testing.T) {
	svc, basePath := newTestService(t)

	tests := []struct {
		name string
		path string
		why  string
	}{
		{
			"trailing dot-dot resolves to basePath",
			basePath + "/movies/..",
			"filepath.Clean resolves .. back to basePath itself, which is valid",
		},
		{
			"backslash is literal on Linux",
			basePath + "/sub\\..\\..\\etc",
			"on Linux \\ is a legal filename character, not a separator",
		},
		{
			"unicode fraction slash U+2044",
			basePath + "/sub\u2044..\u2044..\u2044etc",
			"OS treats unicode look-alike slashes as literal filename chars",
		},
		{
			"unicode division slash U+2215",
			basePath + "/sub\u2215..\u2215etc",
			"OS treats unicode look-alike slashes as literal filename chars",
		},
		{
			"null byte mid-path",
			basePath + "/sub\x00/../etc",
			"Go strings can contain \\x00; filepath.Clean keeps it; Linux ENOENT on null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.validatePath(tt.path)
			if err != nil {
				t.Errorf("validatePath(%q) = %v, expected nil (not a real traversal: %s)", tt.path, err, tt.why)
			}
		})
	}
}

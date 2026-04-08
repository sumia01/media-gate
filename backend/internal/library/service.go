package library

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/store"
)

// ScanEntry represents a single file or directory found during a library scan.
type ScanEntry struct {
	Name        string
	Path        string
	IsDirectory bool
	Size        int64
	ModifiedAt  time.Time
}

var ErrPathOutsideBase = errors.New("path must be within the configured base path")
var ErrPathIsDownloadDir = errors.New("path is reserved for downloads")

// SettingsGetter provides read access to settings without importing the settings package.
type SettingsGetter interface {
	Get(key string) (string, error)
}

// BasePathProvider provides the resolved library base path.
type BasePathProvider interface {
	BasePath() string
}

type Service struct {
	store        store.Store
	basePathProv BasePathProvider
	settings     SettingsGetter
}

func NewService(s store.Store, bp BasePathProvider, sg SettingsGetter) *Service {
	return &Service{store: s, basePathProv: bp, settings: sg}
}

// BasePath returns the current resolved base path for libraries.
func (s *Service) BasePath() string {
	return s.basePathProv.BasePath()
}

// validatePath checks that the given path is inside basePath after cleaning.
func (s *Service) validatePath(p string) error {
	base := s.BasePath()
	clean := filepath.Clean(p)
	if !strings.HasPrefix(clean, base+string(filepath.Separator)) && clean != base {
		return ErrPathOutsideBase
	}
	return nil
}

func (s *Service) Create(lib *store.Library) error {
	if err := s.validatePath(lib.Path); err != nil {
		return err
	}
	if err := s.checkNotDownloadPath(lib.Path); err != nil {
		return err
	}
	if err := s.store.CreateLibrary(lib); err != nil {
		return err
	}
	slog.Info("library: created", "library_id", lib.ID, "name", lib.Name, "path", lib.Path, "media_type", lib.MediaType)
	return nil
}

func (s *Service) List() ([]store.Library, error) {
	return s.store.ListLibraries()
}

func (s *Service) Get(id uint) (*store.Library, error) {
	return s.store.GetLibrary(id)
}

func (s *Service) Update(lib *store.Library) error {
	if err := s.validatePath(lib.Path); err != nil {
		return err
	}
	if err := s.checkNotDownloadPath(lib.Path); err != nil {
		return err
	}
	if err := s.store.UpdateLibrary(lib); err != nil {
		return err
	}
	slog.Info("library: updated", "library_id", lib.ID, "name", lib.Name, "path", lib.Path)
	return nil
}

func (s *Service) Delete(id uint) error {
	if err := s.store.DeleteLibrary(id); err != nil {
		return err
	}
	slog.Info("library: deleted", "library_id", id)
	return nil
}

// Scan reads the immediate children of the library's path and returns them as ScanEntry values.
func (s *Service) Scan(lib *store.Library) ([]ScanEntry, error) {
	entries, err := os.ReadDir(lib.Path)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", lib.Path, err)
	}

	result := make([]ScanEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, ScanEntry{
			Name:        e.Name(),
			Path:        e.Name(),
			IsDirectory: e.IsDir(),
			Size:        info.Size(),
			ModifiedAt:  info.ModTime(),
		})
	}
	return result, nil
}

// Browse lists only subdirectories within the given path.
// If path is empty, the configured basePath is used.
// Returns the cleaned absolute path and directory entries.
func (s *Service) Browse(path string) (string, []ScanEntry, error) {
	if path == "" {
		path = s.BasePath()
	}
	path = filepath.Clean(path)

	if err := s.validatePath(path); err != nil {
		return "", nil, err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading directory %s: %w", path, err)
	}

	result := make([]ScanEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, ScanEntry{
			Name:        e.Name(),
			Path:        filepath.Join(path, e.Name()),
			IsDirectory: true,
			Size:        info.Size(),
			ModifiedAt:  info.ModTime(),
		})
	}
	return path, result, nil
}

// checkNotDownloadPath rejects the path if it matches the configured download directory.
func (s *Service) checkNotDownloadPath(p string) error {
	if s.settings == nil {
		return nil
	}
	dlPath, err := s.settings.Get("qbit_download_path")
	if err != nil {
		// No download path configured — no conflict possible.
		return nil
	}
	if filepath.Clean(p) == filepath.Clean(dlPath) {
		return ErrPathIsDownloadDir
	}
	return nil
}

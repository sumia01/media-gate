package library

import (
	"errors"
	"fmt"
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

type Service struct {
	store    store.Store
	basePath string
}

func NewService(s store.Store, basePath string) *Service {
	return &Service{store: s, basePath: filepath.Clean(basePath)}
}

// BasePath returns the configured base path for libraries.
func (s *Service) BasePath() string {
	return s.basePath
}

// validatePath checks that the given path is inside basePath after cleaning.
func (s *Service) validatePath(p string) error {
	clean := filepath.Clean(p)
	if !strings.HasPrefix(clean, s.basePath+string(filepath.Separator)) && clean != s.basePath {
		return ErrPathOutsideBase
	}
	return nil
}

func (s *Service) Create(lib *store.Library) error {
	if err := s.validatePath(lib.Path); err != nil {
		return err
	}
	return s.store.CreateLibrary(lib)
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
	return s.store.UpdateLibrary(lib)
}

func (s *Service) Delete(id uint) error {
	return s.store.DeleteLibrary(id)
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
		path = s.basePath
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

package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateLibrary(lib *store.Library) error {
	return s.db.Create(lib).Error
}

func (s *SQLiteStore) ListLibraries() ([]store.Library, error) {
	var libs []store.Library
	if err := s.db.Find(&libs).Error; err != nil {
		return nil, err
	}
	return libs, nil
}

func (s *SQLiteStore) GetLibrary(id uint) (*store.Library, error) {
	return getByID[store.Library](s.db, id)
}

func (s *SQLiteStore) UpdateLibrary(lib *store.Library) error {
	return save(s.db, lib)
}

func (s *SQLiteStore) DeleteLibrary(id uint) error {
	return deleteByID(s.db, &store.Library{}, id)
}

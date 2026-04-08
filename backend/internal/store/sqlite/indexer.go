package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateIndexer(indexer *store.Indexer) error {
	return s.db.Create(indexer).Error
}

func (s *SQLiteStore) GetIndexer(id uint) (*store.Indexer, error) {
	return getByID[store.Indexer](s.db, id)
}

func (s *SQLiteStore) ListIndexers() ([]store.Indexer, error) {
	var indexers []store.Indexer
	if err := s.db.Order("priority DESC, name ASC").Find(&indexers).Error; err != nil {
		return nil, err
	}
	return indexers, nil
}

func (s *SQLiteStore) UpdateIndexer(indexer *store.Indexer) error {
	return save(s.db, indexer)
}

func (s *SQLiteStore) DeleteIndexer(id uint) error {
	return deleteByID(s.db, &store.Indexer{}, id)
}

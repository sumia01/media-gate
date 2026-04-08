package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateMediaProfile(profile *store.MediaProfile) error {
	return s.db.Create(profile).Error
}

func (s *SQLiteStore) GetMediaProfile(id uint) (*store.MediaProfile, error) {
	return getByID[store.MediaProfile](s.db, id)
}

func (s *SQLiteStore) ListMediaProfiles() ([]store.MediaProfile, error) {
	var profiles []store.MediaProfile
	if err := s.db.Order("name ASC").Find(&profiles).Error; err != nil {
		return nil, err
	}
	return profiles, nil
}

func (s *SQLiteStore) UpdateMediaProfile(profile *store.MediaProfile) error {
	return save(s.db, profile)
}

func (s *SQLiteStore) DeleteMediaProfile(id uint) error {
	return deleteByID(s.db, &store.MediaProfile{}, id)
}

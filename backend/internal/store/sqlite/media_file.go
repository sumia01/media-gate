package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateMediaFile(file *store.MediaFile) error {
	return s.db.Create(file).Error
}

func (s *SQLiteStore) GetMediaFile(id uint) (*store.MediaFile, error) {
	return getByID[store.MediaFile](s.db, id)
}

func (s *SQLiteStore) UpdateMediaFile(file *store.MediaFile) error {
	return save(s.db, file)
}

func (s *SQLiteStore) ListMediaFilesByMediaItem(mediaItemID uint) ([]store.MediaFile, error) {
	var files []store.MediaFile
	if err := s.db.Where("media_item_id = ?", mediaItemID).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (s *SQLiteStore) ListMediaFilesByLibrary(libraryID uint) ([]store.MediaFile, error) {
	var files []store.MediaFile
	if err := s.db.
		Joins("JOIN media_items ON media_items.id = media_files.media_item_id").
		Where("media_items.library_id = ?", libraryID).
		Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (s *SQLiteStore) DeleteMediaFile(id uint) error {
	return deleteByID(s.db, &store.MediaFile{}, id)
}

func (s *SQLiteStore) DeleteMediaFilesByPaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return s.db.Where("path IN ?", paths).Delete(&store.MediaFile{}).Error
}

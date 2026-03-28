package store

import (
	"errors"
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ Store = (*SQLiteStore)(nil)

type SQLiteStore struct {
	db *gorm.DB
}

func NewSQLite(dbPath string) (*SQLiteStore, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	if err := db.AutoMigrate(
		&Library{},
		&MediaItem{},
		&MediaMetadata{},
		&QualityProfile{},
		&MediaFile{},
		&SeasonMonitor{},
		&Setting{},
		&JobRecord{},
	); err != nil {
		return nil, fmt.Errorf("auto-migrating database: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Ping() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func (s *SQLiteStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// --- Library ---

func (s *SQLiteStore) CreateLibrary(lib *Library) error {
	return s.db.Create(lib).Error
}

func (s *SQLiteStore) ListLibraries() ([]Library, error) {
	var libs []Library
	if err := s.db.Find(&libs).Error; err != nil {
		return nil, err
	}
	return libs, nil
}

func (s *SQLiteStore) GetLibrary(id uint) (*Library, error) {
	var lib Library
	if err := s.db.First(&lib, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &lib, nil
}

func (s *SQLiteStore) UpdateLibrary(lib *Library) error {
	result := s.db.Save(lib)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteLibrary(id uint) error {
	result := s.db.Delete(&Library{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- MediaItem ---

func (s *SQLiteStore) CreateMediaItem(item *MediaItem) error {
	return s.db.Create(item).Error
}

func (s *SQLiteStore) GetMediaItem(id uint) (*MediaItem, error) {
	var item MediaItem
	if err := s.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) UpdateMediaItem(item *MediaItem) error {
	result := s.db.Save(item)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteMediaItem(id uint) error {
	result := s.db.Delete(&MediaItem{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListMediaItemsByLibrary(libraryID uint) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("library_id = ?", libraryID).Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListDiskMediaItemsByLibrary(libraryID uint) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("library_id = ? AND source = ?", libraryID, "disk").Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) ListNewMediaItemsByLibrary(libraryID uint) ([]MediaItem, error) {
	var items []MediaItem
	if err := s.db.Where("library_id = ? AND status = ?", libraryID, "new").Order("title ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (s *SQLiteStore) DeleteMediaItemsByLibrary(libraryID uint) error {
	return s.db.Where("library_id = ?", libraryID).Delete(&MediaItem{}).Error
}

func (s *SQLiteStore) CountMediaItemsByLibrary(libraryID uint) (int64, error) {
	var count int64
	if err := s.db.Model(&MediaItem{}).Where("library_id = ?", libraryID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStore) MediaItemExistsByExternalID(libraryID uint, source string, externalID int) (bool, error) {
	var count int64
	err := s.db.Model(&MediaMetadata{}).
		Joins("JOIN media_items ON media_items.id = media_metadata.media_item_id").
		Where("media_items.library_id = ? AND media_metadata.source = ? AND media_metadata.external_id = ?", libraryID, source, externalID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// --- MediaMetadata ---

func (s *SQLiteStore) CreateMediaMetadata(meta *MediaMetadata) error {
	return s.db.Create(meta).Error
}

func (s *SQLiteStore) GetMediaMetadataByMediaItem(mediaItemID uint) (*MediaMetadata, error) {
	var meta MediaMetadata
	if err := s.db.Where("media_item_id = ?", mediaItemID).First(&meta).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &meta, nil
}

func (s *SQLiteStore) UpdateMediaMetadata(meta *MediaMetadata) error {
	result := s.db.Save(meta)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteMediaMetadataByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&MediaMetadata{}).Error
}

func (s *SQLiteStore) ListMediaMetadataByMediaItemIDs(ids []uint) ([]MediaMetadata, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var metas []MediaMetadata
	if err := s.db.Where("media_item_id IN ?", ids).Find(&metas).Error; err != nil {
		return nil, err
	}
	return metas, nil
}

// --- QualityProfile ---

func (s *SQLiteStore) CreateQualityProfile(profile *QualityProfile) error {
	return s.db.Create(profile).Error
}

func (s *SQLiteStore) GetQualityProfile(id uint) (*QualityProfile, error) {
	var profile QualityProfile
	if err := s.db.First(&profile, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &profile, nil
}

func (s *SQLiteStore) ListQualityProfiles() ([]QualityProfile, error) {
	var profiles []QualityProfile
	if err := s.db.Order("name ASC").Find(&profiles).Error; err != nil {
		return nil, err
	}
	return profiles, nil
}

func (s *SQLiteStore) UpdateQualityProfile(profile *QualityProfile) error {
	result := s.db.Save(profile)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteQualityProfile(id uint) error {
	result := s.db.Delete(&QualityProfile{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- MediaFile ---

func (s *SQLiteStore) CreateMediaFile(file *MediaFile) error {
	return s.db.Create(file).Error
}

func (s *SQLiteStore) GetMediaFile(id uint) (*MediaFile, error) {
	var file MediaFile
	if err := s.db.First(&file, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &file, nil
}

func (s *SQLiteStore) ListMediaFilesByMediaItem(mediaItemID uint) ([]MediaFile, error) {
	var files []MediaFile
	if err := s.db.Where("media_item_id = ?", mediaItemID).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (s *SQLiteStore) ListMediaFilesByLibrary(libraryID uint) ([]MediaFile, error) {
	var files []MediaFile
	if err := s.db.
		Joins("JOIN media_items ON media_items.id = media_files.media_item_id").
		Where("media_items.library_id = ?", libraryID).
		Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (s *SQLiteStore) DeleteMediaFile(id uint) error {
	result := s.db.Delete(&MediaFile{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteMediaFilesByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&MediaFile{}).Error
}

func (s *SQLiteStore) DeleteMediaFilesByPaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return s.db.Where("path IN ?", paths).Delete(&MediaFile{}).Error
}

func (s *SQLiteStore) MediaFileExistsByPath(path string) bool {
	var count int64
	s.db.Model(&MediaFile{}).Where("path = ?", path).Count(&count)
	return count > 0
}

// --- SeasonMonitor ---

func (s *SQLiteStore) CreateSeasonMonitor(monitor *SeasonMonitor) error {
	return s.db.Create(monitor).Error
}

func (s *SQLiteStore) ListSeasonMonitorsByMediaItem(mediaItemID uint) ([]SeasonMonitor, error) {
	var monitors []SeasonMonitor
	if err := s.db.Where("media_item_id = ?", mediaItemID).Order("season_number ASC").Find(&monitors).Error; err != nil {
		return nil, err
	}
	return monitors, nil
}

func (s *SQLiteStore) UpdateSeasonMonitor(monitor *SeasonMonitor) error {
	result := s.db.Save(monitor)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteSeasonMonitorsByMediaItem(mediaItemID uint) error {
	return s.db.Where("media_item_id = ?", mediaItemID).Delete(&SeasonMonitor{}).Error
}

// --- Settings ---

func (s *SQLiteStore) GetSetting(key string) (*Setting, error) {
	var setting Setting
	if err := s.db.First(&setting, "key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &setting, nil
}

func (s *SQLiteStore) SetSetting(setting *Setting) error {
	return s.db.Save(setting).Error
}

func (s *SQLiteStore) ListSettings() ([]Setting, error) {
	var settings []Setting
	if err := s.db.Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *SQLiteStore) DeleteSetting(key string) error {
	result := s.db.Delete(&Setting{}, "key = ?", key)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- JobRecords ---

func (s *SQLiteStore) CreateJobRecord(record *JobRecord) error {
	return s.db.Create(record).Error
}

func (s *SQLiteStore) ListJobRecords(limit int) ([]JobRecord, error) {
	var records []JobRecord
	if err := s.db.Order("completed_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (s *SQLiteStore) DeleteOldJobRecords(keep int) error {
	return s.db.Exec(
		"DELETE FROM job_records WHERE id NOT IN (SELECT id FROM job_records ORDER BY completed_at DESC LIMIT ?)",
		keep,
	).Error
}

func (s *SQLiteStore) MaxJobRecordID() (uint, error) {
	var maxID *uint
	if err := s.db.Model(&JobRecord{}).Select("MAX(id)").Scan(&maxID).Error; err != nil {
		return 0, err
	}
	if maxID == nil {
		return 0, nil
	}
	return *maxID, nil
}

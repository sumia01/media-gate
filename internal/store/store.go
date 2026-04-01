package store

import (
	"errors"
	"io"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("record not found")

// Store defines the data access interface.
// Implementations must be safe for concurrent use.
type Store interface {
	io.Closer
	// Ping verifies the database connection is alive.
	Ping() error

	CreateLibrary(lib *Library) error
	ListLibraries() ([]Library, error)
	GetLibrary(id uint) (*Library, error)
	UpdateLibrary(lib *Library) error
	DeleteLibrary(id uint) error

	CreateMediaItem(item *MediaItem) error
	GetMediaItem(id uint) (*MediaItem, error)
	UpdateMediaItem(item *MediaItem) error
	DeleteMediaItem(id uint) error
	ListMediaItemsByLibrary(libraryID uint) ([]MediaItem, error)
	ListDiskMediaItemsByLibrary(libraryID uint) ([]MediaItem, error)
	ListNewMediaItemsByLibrary(libraryID uint) ([]MediaItem, error)
	CountMediaItemsByLibrary(libraryID uint) (int64, error)
	MediaItemExistsByExternalID(libraryID uint, source string, externalID int) (bool, error)
	ListMonitoredMediaItems() ([]MediaItem, error)

	CreateMediaMetadata(meta *MediaMetadata) error
	GetMediaMetadataByMediaItem(mediaItemID uint) (*MediaMetadata, error)
	UpdateMediaMetadata(meta *MediaMetadata) error
	DeleteMediaMetadataByMediaItem(mediaItemID uint) error
	ListMediaMetadataByMediaItemIDs(ids []uint) ([]MediaMetadata, error)

	// MediaProfile CRUD
	CreateMediaProfile(profile *MediaProfile) error
	GetMediaProfile(id uint) (*MediaProfile, error)
	ListMediaProfiles() ([]MediaProfile, error)
	UpdateMediaProfile(profile *MediaProfile) error
	DeleteMediaProfile(id uint) error

	// MediaFile CRUD
	CreateMediaFile(file *MediaFile) error
	GetMediaFile(id uint) (*MediaFile, error)
	UpdateMediaFile(file *MediaFile) error
	ListMediaFilesByMediaItem(mediaItemID uint) ([]MediaFile, error)
	ListMediaFilesByLibrary(libraryID uint) ([]MediaFile, error)
	DeleteMediaFile(id uint) error
	DeleteMediaFilesByPaths(paths []string) error

	// SeasonMonitor CRUD
	CreateSeasonMonitor(monitor *SeasonMonitor) error
	ListSeasonMonitorsByMediaItem(mediaItemID uint) ([]SeasonMonitor, error)
	UpdateSeasonMonitor(monitor *SeasonMonitor) error

	// Episode CRUD
	CreateEpisode(episode *Episode) error
	ListEpisodesByMediaItem(mediaItemID uint) ([]Episode, error)
	DeleteEpisodesByMediaItem(mediaItemID uint) error

	GetSetting(key string) (*Setting, error)
	SetSetting(setting *Setting) error
	ListSettings() ([]Setting, error)
	DeleteSetting(key string) error

	CreateJobRecord(record *JobRecord) error
	ListJobRecords(limit int) ([]JobRecord, error)
	DeleteOldJobRecords(keep int) error
	MaxJobRecordID() (uint, error)

	// Indexer CRUD
	CreateIndexer(indexer *Indexer) error
	GetIndexer(id uint) (*Indexer, error)
	ListIndexers() ([]Indexer, error)
	UpdateIndexer(indexer *Indexer) error
	DeleteIndexer(id uint) error

	// Download CRUD
	CreateDownload(download *Download) error
	GetDownload(id uint) (*Download, error)
	UpdateDownload(download *Download) error
	ListDownloads(mediaItemID *uint, status *string) ([]Download, error)
	DeleteDownload(id uint) error
}

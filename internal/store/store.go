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
	DeleteMediaItemsByLibrary(libraryID uint) error
	DeleteMediaItemsByPaths(libraryID uint, paths []string) error
	CountMediaItemsByLibrary(libraryID uint) (int64, error)
	MediaItemExistsByExternalID(libraryID uint, source string, externalID int) (bool, error)

	CreateMediaMetadata(meta *MediaMetadata) error
	GetMediaMetadataByMediaItem(mediaItemID uint) (*MediaMetadata, error)
	UpdateMediaMetadata(meta *MediaMetadata) error
	DeleteMediaMetadataByMediaItem(mediaItemID uint) error
	ListMediaMetadataByMediaItemIDs(ids []uint) ([]MediaMetadata, error)

	GetSetting(key string) (*Setting, error)
	SetSetting(setting *Setting) error
	ListSettings() ([]Setting, error)
	DeleteSetting(key string) error

	CreateJobRecord(record *JobRecord) error
	ListJobRecords(limit int) ([]JobRecord, error)
	DeleteOldJobRecords(keep int) error
	MaxJobRecordID() (uint, error)
}

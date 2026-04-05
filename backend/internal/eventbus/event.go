package eventbus

import "time"

// EventType identifies the kind of event.
type EventType string

// Download lifecycle events.
const (
	DownloadCreated      EventType = "download.created"
	DownloadSentToClient EventType = "download.sent_to_client"
	DownloadFailed       EventType = "download.failed"
	DownloadCompleted    EventType = "download.completed"
	ImportStarted        EventType = "download.import_started"
	ImportCompleted      EventType = "download.import_completed"
	ImportFailed         EventType = "download.import_failed"
	SeedingCompleted     EventType = "download.seeding_completed"
)

// Library workflow events.
const (
	LibrarySyncStarted    EventType = "library.sync_started"
	LibrarySyncCompleted  EventType = "library.sync_completed"
	LibrarySyncFailed     EventType = "library.sync_failed"
	LibraryMatchStarted   EventType = "library.match_started"
	LibraryMatchProgress  EventType = "library.match_progress"
	LibraryMatchCompleted EventType = "library.match_completed"
	LibraryMatchFailed    EventType = "library.match_failed"
)

// Media item events.
const (
	MediaItemSynced    EventType = "media.item_synced"
	MediaItemRemoved   EventType = "media.item_removed"
	MediaItemMatched   EventType = "media.item_matched"
	MediaItemDeleteReq EventType = "media.item_delete_requested"
	MediaItemDeleted   EventType = "media.item_deleted"
	ResyncCompleted    EventType = "media.resync_completed"
)

// Monitor worker events.
const (
	MonitorGrabbed EventType = "monitor.grabbed"
)

// Event is a single occurrence published on the bus.
type Event struct {
	Type      EventType `json:"type"`
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

// --- Typed payloads ---

// DownloadPayload carries download lifecycle event data.
type DownloadPayload struct {
	DownloadID  uint   `json:"downloadId"`
	MediaItemID uint   `json:"mediaItemId"`
	Title       string `json:"title"`
	Hash        string `json:"hash,omitempty"`
	Status      string `json:"status,omitempty"`
}

// ImportPayload carries import event data.
type ImportPayload struct {
	DownloadID  uint `json:"downloadId"`
	MediaItemID uint `json:"mediaItemId"`
	FilesCount  int  `json:"filesCount,omitempty"`
}

// LibrarySyncPayload carries library sync event data.
type LibrarySyncPayload struct {
	LibraryID   uint   `json:"libraryId"`
	LibraryName string `json:"libraryName"`
	Added       int    `json:"added,omitempty"`
	Removed     int    `json:"removed,omitempty"`
}

// LibraryMatchPayload carries library match event data.
type LibraryMatchPayload struct {
	LibraryID   uint   `json:"libraryId"`
	LibraryName string `json:"libraryName"`
	Current     int    `json:"current,omitempty"`
	Total       int    `json:"total,omitempty"`
}

// MediaItemPayload carries media item event data.
type MediaItemPayload struct {
	MediaItemID uint   `json:"mediaItemId"`
	LibraryID   uint   `json:"libraryId"`
	Title       string `json:"title,omitempty"`
	PosterPath  string `json:"posterPath,omitempty"`
}

// MediaItemDeletePayload carries data needed for async delete cleanup.
type MediaItemDeletePayload struct {
	MediaItemID   uint     `json:"mediaItemId"`
	LibraryID     uint     `json:"libraryId"`
	TorrentHashes []string `json:"torrentHashes,omitempty"`
	FilePaths     []string `json:"filePaths,omitempty"`
	PosterPath    string   `json:"posterPath,omitempty"`
	LibraryPath   string   `json:"libraryPath,omitempty"`
}

// ResyncPayload carries resync event data.
type ResyncPayload struct {
	MediaItemID uint `json:"mediaItemId"`
	Updated     int  `json:"updated,omitempty"`
	Added       int  `json:"added,omitempty"`
	Removed     int  `json:"removed,omitempty"`
}

// MonitorPayload carries monitor worker event data.
type MonitorPayload struct {
	MediaItemID uint   `json:"mediaItemId"`
	Title       string `json:"title"`
	ResultTitle string `json:"resultTitle"`
}

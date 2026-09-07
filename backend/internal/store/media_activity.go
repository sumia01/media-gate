package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	MediaActivityActorUser   = "user"
	MediaActivityActorSystem = "system"

	MediaActivityVisibilityShared    = "shared"
	MediaActivityVisibilityActorOnly = "actor_only"

	MediaActivityActionRequestMade            = "request.made"
	MediaActivityActionMonitoringChanged      = "monitoring.changed"
	MediaActivityActionSettingsChanged        = "media.settings_changed"
	MediaActivityActionDownloadQueued         = "download.queued"
	MediaActivityActionDownloadStatusChanged  = "download.status_changed"
	MediaActivityActionDownloadRemovalRequest = "download.removal_requested"
	MediaActivityActionDownloadRemoved        = "download.removed"
	MediaActivityActionMatchChanged           = "media.match_changed"
	MediaActivityActionUnmatched              = "media.unmatched"
	MediaActivityActionMetadataChanged        = "metadata.changed"
	MediaActivityActionResyncRequested        = "media.resync_requested"
	MediaActivityActionResyncCompleted        = "media.resync_completed"
	MediaActivityActionRemovalRequested       = "media.removal_requested"
	MediaActivityActionSubtitleDownloaded     = "subtitle.downloaded"
	MediaActivityActionSubtitleRemoved        = "subtitle.removed"
	MediaActivityActionWatchedMarked          = "watched.marked"
	MediaActivityActionWatchedUnmarked        = "watched.unmarked"

	MediaActivityScopeMedia         = "media"
	MediaActivityScopeWholeSeries   = "whole_series"
	MediaActivityScopeFutureSeasons = "future_seasons"
	MediaActivityScopeSeason        = "season"
	MediaActivityScopeEpisode       = "episode"
	MediaActivityScopeUnknown       = "unknown"

	MediaActivityDetailsVersion  = 1
	MediaActivityMaxDetailsBytes = 32 * 1024
	MediaActivityMaxDetails      = 50
	MediaActivityMaxTitleBytes   = 512
)

var knownMediaActivityActions = map[string]struct{}{
	MediaActivityActionRequestMade:            {},
	MediaActivityActionMonitoringChanged:      {},
	MediaActivityActionSettingsChanged:        {},
	MediaActivityActionDownloadQueued:         {},
	MediaActivityActionDownloadStatusChanged:  {},
	MediaActivityActionDownloadRemovalRequest: {},
	MediaActivityActionDownloadRemoved:        {},
	MediaActivityActionMatchChanged:           {},
	MediaActivityActionUnmatched:              {},
	MediaActivityActionMetadataChanged:        {},
	MediaActivityActionResyncRequested:        {},
	MediaActivityActionResyncCompleted:        {},
	MediaActivityActionRemovalRequested:       {},
	MediaActivityActionSubtitleDownloaded:     {},
	MediaActivityActionSubtitleRemoved:        {},
	MediaActivityActionWatchedMarked:          {},
	MediaActivityActionWatchedUnmarked:        {},
}

// MediaActivity is append-only application history. Domain state and requester
// attribution remain independent sources of truth.
type MediaActivity struct {
	ID             uint `gorm:"primarykey"`
	MediaItemID    uint `gorm:"not null;index;constraint:OnDelete:CASCADE"`
	RecordedAt     time.Time
	ActorKind      string
	ActorUserID    *uint
	ActorComponent string
	Action         string
	OperationID    string
	Visibility     string
	MediaTitle     string
	DetailsVersion int
	Details        string
}

func (MediaActivity) TableName() string { return "media_activity" }

// MediaActivityAttribution adds the actor's current display-name projection.
// User fields become empty after account deletion.
type MediaActivityAttribution struct {
	MediaActivity
	FirstName string `gorm:"->;-:migration"`
	LastName  string `gorm:"->;-:migration"`
	Email     string `gorm:"->;-:migration"`
}

// MediaActivityTarget records natural media scope at event time. Object fields
// are safe snapshots for downloads, files, or subtitles and never contain URLs,
// hashes, or paths.
type MediaActivityTarget struct {
	Scope         string `json:"scope"`
	SeasonNumber  *int   `json:"seasonNumber,omitempty"`
	EpisodeNumber *int   `json:"episodeNumber,omitempty"`
	ObjectID      *uint  `json:"objectId,omitempty"`
	Title         string `json:"title,omitempty"`
}

type MediaActivityMonitoringChange struct {
	Target          MediaActivityTarget `json:"target"`
	Before          *bool               `json:"before,omitempty"`
	After           *bool               `json:"after,omitempty"`
	EffectiveBefore bool                `json:"effectiveBefore"`
	EffectiveAfter  bool                `json:"effectiveAfter"`
}

type MediaActivityFieldChange struct {
	Field  string               `json:"field"`
	Before *string              `json:"before,omitempty"`
	After  *string              `json:"after,omitempty"`
	Target *MediaActivityTarget `json:"target,omitempty"`
}

type MediaActivityProviderIdentity struct {
	Source     string `json:"source"`
	ExternalID int    `json:"externalId"`
	Title      string `json:"title,omitempty"`
}

// MediaActivityDetails is the version-1 payload shared by the initial action
// set. Only action-relevant fields are populated.
type MediaActivityDetails struct {
	Reason                    string                          `json:"reason,omitempty"`
	Target                    *MediaActivityTarget            `json:"target,omitempty"`
	Targets                   []MediaActivityTarget           `json:"targets,omitempty"`
	MonitoringChanges         []MediaActivityMonitoringChange `json:"monitoringChanges,omitempty"`
	FieldChanges              []MediaActivityFieldChange      `json:"fieldChanges,omitempty"`
	MediaAdded                *bool                           `json:"mediaAdded,omitempty"`
	Automatic                 *bool                           `json:"automatic,omitempty"`
	ReleaseName               string                          `json:"releaseName,omitempty"`
	IndexerName               string                          `json:"indexerName,omitempty"`
	DownloadID                *uint                           `json:"downloadId,omitempty"`
	OldStatus                 string                          `json:"oldStatus,omitempty"`
	NewStatus                 string                          `json:"newStatus,omitempty"`
	OldProvider               *MediaActivityProviderIdentity  `json:"oldProvider,omitempty"`
	NewProvider               *MediaActivityProviderIdentity  `json:"newProvider,omitempty"`
	Language                  string                          `json:"language,omitempty"`
	Provider                  string                          `json:"provider,omitempty"`
	FileName                  string                          `json:"fileName,omitempty"`
	CleanupOutcome            string                          `json:"cleanupOutcome,omitempty"`
	RequestedDeleteFiles      *bool                           `json:"requestedDeleteFiles,omitempty"`
	RecordRemoved             *bool                           `json:"recordRemoved,omitempty"`
	TorrentCleanupOutcome     string                          `json:"torrentCleanupOutcome,omitempty"`
	FileCleanupOutcome        string                          `json:"fileCleanupOutcome,omitempty"`
	PhysicalFilesRemoved      int                             `json:"physicalFilesRemoved,omitempty"`
	PhysicalFilesFailed       int                             `json:"physicalFilesFailed,omitempty"`
	DatabaseRecordsRemoved    int                             `json:"databaseRecordsRemoved,omitempty"`
	Added                     int                             `json:"added,omitempty"`
	Updated                   int                             `json:"updated,omitempty"`
	Removed                   int                             `json:"removed,omitempty"`
	Total                     int                             `json:"total,omitempty"`
	Truncated                 bool                            `json:"truncated,omitempty"`
	Partial                   bool                            `json:"partial,omitempty"`
	FailedProviderWindows     int                             `json:"failedProviderWindows,omitempty"`
	SuccessfulProviderWindows int                             `json:"successfulProviderWindows,omitempty"`
}

// NewMediaActivityOperationID returns a short random correlation key. It is not
// a deduplication key and may be shared by multiple facts from one command.
func NewMediaActivityOperationID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating activity operation id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func NewUserMediaActivity(item *MediaItem, userID uint, action, operationID, visibility string, details MediaActivityDetails) (*MediaActivity, error) {
	return newMediaActivity(item, MediaActivityActorUser, &userID, "", action, operationID, visibility, details)
}

func NewSystemMediaActivity(item *MediaItem, component, action, operationID string, details MediaActivityDetails) (*MediaActivity, error) {
	return newMediaActivity(item, MediaActivityActorSystem, nil, component, action, operationID, MediaActivityVisibilityShared, details)
}

func newMediaActivity(item *MediaItem, actorKind string, userID *uint, component, action, operationID, visibility string, details MediaActivityDetails) (*MediaActivity, error) {
	if item == nil {
		return nil, ErrInvalidMediaActivity
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding details", ErrInvalidMediaActivity)
	}
	activity := &MediaActivity{
		MediaItemID: item.ID, ActorKind: actorKind, ActorUserID: userID,
		ActorComponent: component, Action: action, OperationID: operationID,
		Visibility: visibility, MediaTitle: BoundMediaActivityText(item.Title, MediaActivityMaxTitleBytes),
		DetailsVersion: MediaActivityDetailsVersion, Details: string(payload),
	}
	if err := ValidateMediaActivity(activity); err != nil {
		return nil, err
	}
	return activity, nil
}

// BoundMediaActivityText returns valid UTF-8 text no larger than maxBytes.
// Activity details use it for safe event-time titles and release names.
func BoundMediaActivityText(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(value, "") {
		if b.Len()+len(string(r)) > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func ValidateMediaActivity(activity *MediaActivity) error {
	if activity == nil || activity.MediaItemID == 0 || strings.TrimSpace(activity.MediaTitle) == "" || len(activity.MediaTitle) > MediaActivityMaxTitleBytes ||
		strings.TrimSpace(activity.OperationID) == "" || len(activity.OperationID) > 64 || activity.DetailsVersion != MediaActivityDetailsVersion ||
		len(activity.Details) == 0 || len(activity.Details) > MediaActivityMaxDetailsBytes || !json.Valid([]byte(activity.Details)) {
		return ErrInvalidMediaActivity
	}
	if _, ok := knownMediaActivityActions[activity.Action]; !ok {
		return ErrInvalidMediaActivity
	}
	switch activity.ActorKind {
	case MediaActivityActorUser:
		if activity.ActorUserID == nil || *activity.ActorUserID == 0 || activity.ActorComponent != "" {
			return ErrInvalidMediaActivity
		}
	case MediaActivityActorSystem:
		if activity.ActorUserID != nil || strings.TrimSpace(activity.ActorComponent) == "" || len(activity.ActorComponent) > 64 {
			return ErrInvalidMediaActivity
		}
	default:
		return ErrInvalidMediaActivity
	}
	if activity.Visibility != MediaActivityVisibilityShared && activity.Visibility != MediaActivityVisibilityActorOnly {
		return ErrInvalidMediaActivity
	}
	if activity.Visibility == MediaActivityVisibilityActorOnly && activity.ActorKind != MediaActivityActorUser {
		return ErrInvalidMediaActivity
	}
	var details MediaActivityDetails
	if err := json.Unmarshal([]byte(activity.Details), &details); err != nil {
		return errors.Join(ErrInvalidMediaActivity, err)
	}
	if len(details.Targets) > MediaActivityMaxDetails || len(details.MonitoringChanges) > MediaActivityMaxDetails || len(details.FieldChanges) > MediaActivityMaxDetails {
		return ErrInvalidMediaActivity
	}
	return nil
}

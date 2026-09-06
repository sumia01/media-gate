package store

import "time"

// MonitorDecision is the latest completed auto-monitor check, not a history.
type MonitorDecision struct {
	MediaItemID    uint      `gorm:"primaryKey;autoIncrement:false"`
	MediaItem      MediaItem `gorm:"constraint:OnDelete:CASCADE"`
	CheckedAt      time.Time
	InputUpdatedAt *time.Time // Copied from the input item; nil means legacy/unknown freshness.
	Outcome        string
	Summary        string
	Details        []MonitorDecisionDetail `gorm:"serializer:json"`
	Truncated      bool
}

// MonitorDecisionDetail contains only safe explanations, never indexer URLs or errors.
type MonitorDecisionDetail struct {
	SeasonNumber    *int   `json:"seasonNumber,omitempty"`
	EpisodeNumber   *int   `json:"episodeNumber,omitempty"`
	Outcome         string `json:"outcome"`
	Explanation     string `json:"explanation"`
	TotalResults    int    `json:"totalResults"`
	RejectedResults int    `json:"rejectedResults"`
	BlockedResults  int    `json:"blockedResults"`
	SelectedTitle   string `json:"selectedTitle,omitempty"`
	DownloadID      *uint  `json:"downloadId,omitempty"`
}

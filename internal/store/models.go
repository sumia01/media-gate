package store

import "time"

type Library struct {
	ID               uint   `gorm:"primarykey"`
	Name             string `gorm:"not null"`
	Path             string `gorm:"not null;uniqueIndex"`
	MediaType        string `gorm:"not null"`
	MediaProfileID *uint
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MediaItem struct {
	ID               uint   `gorm:"primarykey"`
	LibraryID        uint   `gorm:"not null;index"`
	Title            string `gorm:"not null"`
	MediaType        string `gorm:"not null"`
	Status           string `gorm:"not null;default:new"`
	Source           string `gorm:"not null;default:disk"`
	Year             *int
	MediaProfileID *uint
	MonitorNewSeasons bool  `gorm:"not null;default:false"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MediaMetadata struct {
	ID          uint    `gorm:"primarykey"`
	MediaItemID uint    `gorm:"not null;uniqueIndex"`
	Source      string  `gorm:"not null"`
	ExternalID  int     `gorm:"not null"`
	Title       string  `gorm:"not null"`
	Overview    string
	PosterPath  string
	Genres      string
	Credits     string
	Year        *int
	Rating      *float64
	Status      string
	Runtime     *int
	Seasons     *int
	Confidence  float64
	MatchedAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MediaProfile struct {
	ID          uint   `gorm:"primarykey"`
	Name        string `gorm:"not null;uniqueIndex"`
	Resolutions string `gorm:"not null"` // JSON array: ["2160p","1080p"]
	Languages   string `gorm:"not null"` // JSON array: ["hun","eng"]
	Sources     string                    // JSON array: ["webdl","webrip"]
	ExcludeTags string                    // JSON array: ["3d","cam"]
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MediaFile struct {
	ID            uint   `gorm:"primarykey"`
	MediaItemID   uint   `gorm:"not null;index"`
	Path          string `gorm:"not null;uniqueIndex"`
	FileName      string `gorm:"not null"`
	Size          int64
	Resolution    string
	SourceType    string
	SeasonNumber  *int
	EpisodeNumber *int
	AddedAt       time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SeasonMonitor struct {
	ID           uint `gorm:"primarykey"`
	MediaItemID  uint `gorm:"not null;uniqueIndex:idx_media_season"`
	SeasonNumber int  `gorm:"not null;uniqueIndex:idx_media_season"`
	Monitored    bool `gorm:"not null;default:true"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Episode struct {
	ID            uint   `gorm:"primarykey"`
	MediaItemID   uint   `gorm:"not null;index;uniqueIndex:idx_episode_unique"`
	SeasonNumber  int    `gorm:"not null;uniqueIndex:idx_episode_unique"`
	EpisodeNumber int    `gorm:"not null;uniqueIndex:idx_episode_unique"`
	Title         string
	Overview      string
	AirDate       string
	Runtime       *int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Setting struct {
	Key       string `gorm:"primarykey"`
	Value     string `gorm:"not null"`
	Sensitive bool   `gorm:"not null;default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type JobRecord struct {
	ID            uint   `gorm:"primarykey"`
	Type          string `gorm:"not null"`
	LibraryID     uint   `gorm:"not null;index"`
	LibraryName   string `gorm:"not null"`
	Status        string `gorm:"not null"`
	ResultMessage string
	Error         string
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
}

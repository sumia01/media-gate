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
	ID                uint   `gorm:"primarykey"`
	LibraryID         uint   `gorm:"not null;index;constraint:OnDelete:CASCADE"`
	Title             string `gorm:"not null"`
	MediaType        string `gorm:"not null"`
	Status           string `gorm:"not null;default:new"`
	Source           string `gorm:"not null;default:disk"`
	Year             *int
	MediaProfileID *uint
	Monitored              bool       `gorm:"not null;default:false"`
	MonitorNewSeasons      bool       `gorm:"not null;default:true"`
	MonitorSearchStartedAt *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type MediaMetadata struct {
	ID          uint    `gorm:"primarykey"`
	MediaItemID uint    `gorm:"not null;uniqueIndex;constraint:OnDelete:CASCADE"`
	Source      string  `gorm:"not null"`
	ExternalID  int     `gorm:"not null"`
	ImdbID      string
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
	ReleaseDate string // ISO "YYYY-MM-DD" from TMDB/TVDB
	TrailerURL  string
	Confidence  float64
	MatchedAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MediaProfile struct {
	ID           uint   `gorm:"primarykey"`
	Name         string `gorm:"not null;uniqueIndex"`
	Resolutions  string `gorm:"not null"`           // JSON array: ["2160p","1080p"]
	Languages    string `gorm:"not null"`           // JSON array: ["hun","eng"]
	LanguageMode string `gorm:"default:'or'"` // "and" or "or"
	Sources      string                             // JSON array: ["webdl","webrip"]
	ExcludeTags  string                             // JSON array: ["3d","cam"]
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type MediaFile struct {
	ID            uint   `gorm:"primarykey"`
	MediaItemID   uint   `gorm:"not null;index;constraint:OnDelete:CASCADE"`
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
	MediaItemID  uint `gorm:"not null;uniqueIndex:idx_media_season;constraint:OnDelete:CASCADE"`
	SeasonNumber int  `gorm:"not null;uniqueIndex:idx_media_season"`
	Monitored    bool `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type EpisodeMonitor struct {
	ID            uint `gorm:"primarykey"`
	MediaItemID   uint `gorm:"not null;uniqueIndex:idx_ep_monitor_unique;constraint:OnDelete:CASCADE"`
	SeasonNumber  int  `gorm:"not null;uniqueIndex:idx_ep_monitor_unique"`
	EpisodeNumber int  `gorm:"not null;uniqueIndex:idx_ep_monitor_unique"`
	Monitored     bool `gorm:"not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Episode struct {
	ID            uint   `gorm:"primarykey"`
	MediaItemID   uint   `gorm:"not null;index;uniqueIndex:idx_episode_unique;constraint:OnDelete:CASCADE"`
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

type Indexer struct {
	ID           uint    `gorm:"primarykey"`
	Name         string  `gorm:"not null"`
	DefinitionID string  `gorm:"not null"`
	Enabled      bool    `gorm:"not null;default:true"`
	Settings     string  `gorm:"not null;default:'{}'"` // JSON: {"username":"x","password":"y",...}
	Priority     int     `gorm:"not null;default:0"`
	SeedMinRatio float64 `gorm:"not null;default:0"` // 0 = no requirement
	SeedMinTime  int     `gorm:"not null;default:0"` // minutes, 0 = no requirement
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Download struct {
	ID                uint   `gorm:"primarykey"`
	MediaItemID       uint   `gorm:"not null;index;constraint:OnDelete:CASCADE"`
	EpisodeID         *uint  `gorm:"index;constraint:OnDelete:SET NULL"`
	SeasonNumber      *int
	IndexerID         uint   `gorm:"not null"`
	IndexerName       string `gorm:"not null"`
	Title             string `gorm:"not null"`
	DownloadURL       string `gorm:"not null"`
	DetailsURL        string
	Size              string
	ImdbID            string
	Status            string `gorm:"not null;default:pending"`
	ClientTorrentHash string
	SavePath          string
	SeedingRequired   bool `gorm:"not null;default:false"`
	LinkedToLibrary   bool `gorm:"not null;default:false"`
	RetryCount        int        `gorm:"not null;default:0"`
	NextRetryAt       *time.Time
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time

	// Populated by JOIN in ListDownloads, not a DB column.
	MediaItemTitle string `gorm:"-"`
}

type User struct {
	ID           uint   `gorm:"primarykey"`
	Email        string `gorm:"not null;uniqueIndex"`
	PasswordHash string `gorm:"not null"`
	FirstName    string
	LastName     string
	BirthYear    *int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RefreshToken struct {
	ID        uint      `gorm:"primarykey"`
	UserID    uint      `gorm:"not null;index;constraint:OnDelete:CASCADE"`
	Token     string    `gorm:"not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}

type Subtitle struct {
	ID               uint   `gorm:"primarykey"`
	MediaItemID      uint   `gorm:"not null;index;constraint:OnDelete:CASCADE"`
	MediaFileID      *uint  `gorm:"index;constraint:OnDelete:SET NULL"`
	SeasonNumber     *int
	EpisodeNumber    *int
	Language         string `gorm:"not null"`           // ISO 639-1 ("en", "hu")
	Provider         string `gorm:"not null"`           // "opensubtitles"
	ProviderFileID   string                             // opaque provider ID
	ReleaseName      string
	FileName         string `gorm:"not null"`
	FilePath         string `gorm:"not null;uniqueIndex"`
	Format           string                             // "srt", "ass", "sub"
	Score            int
	HearingImpaired  bool   `gorm:"not null;default:false"`
	ForeignPartsOnly bool   `gorm:"not null;default:false"`
	Source           string `gorm:"not null;default:manual"` // "manual" or "auto"
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type WatchedItem struct {
	ID          uint   `gorm:"primarykey"`
	UserID      uint   `gorm:"not null;index;uniqueIndex:idx_watched_user_source_ext;constraint:OnDelete:CASCADE"`
	Source      string `gorm:"not null;uniqueIndex:idx_watched_user_source_ext"` // "tmdb" or "tvdb"
	ExternalID  int    `gorm:"not null;uniqueIndex:idx_watched_user_source_ext"`
	ImdbID      string
	Title       string `gorm:"not null"`
	MediaType   string `gorm:"not null"` // "movie" or "series"
	Year        *int
	PosterPath  string
	MediaItemID *uint  `gorm:"index;constraint:OnDelete:SET NULL"`
	WatchedAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

package subtitle

import "context"

// SearchRequest describes what to search for across subtitle providers.
type SearchRequest struct {
	TMDbID        int
	IMDbID        string
	Title         string
	Year          *int
	MediaType     string // "movie" or "series"
	SeasonNumber  *int
	EpisodeNumber *int
	Languages     []string // ISO 639-1 codes
	FileHash      string   // provider-specific, optional
}

// SearchResult represents a single subtitle found by a provider.
type SearchResult struct {
	ProviderName     string
	ProviderFileID   string
	Language         string
	ReleaseName      string
	FileName         string
	HearingImpaired  bool
	ForeignPartsOnly bool
	DownloadCount    int
	HashMatch        bool
	Trusted          bool
	Score            int
}

// DownloadedFile holds the raw bytes of a downloaded subtitle file.
type DownloadedFile struct {
	FileName string
	Data     []byte
	Format   string // "srt", "ass", "sub", etc.
}

// Provider is the interface that subtitle providers implement.
type Provider interface {
	Name() string
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
	Download(ctx context.Context, providerFileID string) (*DownloadedFile, error)
}

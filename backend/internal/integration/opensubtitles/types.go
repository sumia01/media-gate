package opensubtitles

// SearchParams contains query parameters for the search endpoint.
type SearchParams struct {
	TMDbID        *int
	IMDbID        *int
	SeasonNumber  *int
	EpisodeNumber *int
	MovieHash     string
	Languages     string // comma-separated ISO 639-1 codes: "en,hu"
	Query         string
}

// SubtitleResult represents a single subtitle entry from the search response.
type SubtitleResult struct {
	ID         string             `json:"id"`
	Attributes SubtitleAttributes `json:"attributes"`
}

// SubtitleAttributes holds the details of a subtitle result.
type SubtitleAttributes struct {
	Language         string         `json:"language"`
	Release          string         `json:"release"`
	HearingImpaired  bool           `json:"hearing_impaired"`
	ForeignPartsOnly bool           `json:"foreign_parts_only"`
	DownloadCount    int            `json:"download_count"`
	Ratings          float64        `json:"ratings"`
	FromTrusted      bool           `json:"from_trusted"`
	MovieHashMatch   bool           `json:"moviehash_match"`
	Files            []SubtitleFile `json:"files"`
}

// SubtitleFile represents a downloadable file within a subtitle result.
type SubtitleFile struct {
	FileID   int    `json:"file_id"`
	FileName string `json:"file_name"`
}

// DownloadResult is the response from the download endpoint.
type DownloadResult struct {
	Link      string `json:"link"`
	FileName  string `json:"file_name"`
	Remaining int    `json:"remaining"`
}

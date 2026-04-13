package subtitle

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sumia01/media-gate/internal/integration/opensubtitles"
)

// OpenSubtitlesProvider adapts the opensubtitles.Client to the generic Provider interface.
type OpenSubtitlesProvider struct {
	provider *opensubtitles.Provider
}

// NewOpenSubtitlesProvider wraps an opensubtitles.Provider into a generic subtitle.Provider.
func NewOpenSubtitlesProvider(p *opensubtitles.Provider) *OpenSubtitlesProvider {
	return &OpenSubtitlesProvider{provider: p}
}

func (o *OpenSubtitlesProvider) Name() string { return "opensubtitles" }

func (o *OpenSubtitlesProvider) Search(_ context.Context, req SearchRequest) ([]SearchResult, error) {
	client, err := o.provider.Client()
	if err != nil {
		return nil, fmt.Errorf("opensubtitles client: %w", err)
	}

	params := opensubtitles.SearchParams{
		Languages: opensubtitles.FormatLanguages(req.Languages),
	}
	if req.TMDbID != 0 {
		params.TMDbID = &req.TMDbID
	}
	if req.IMDbID != "" {
		// OpenSubtitles expects numeric IMDB ID without "tt" prefix
		imdbStr := req.IMDbID
		if len(imdbStr) > 2 && imdbStr[:2] == "tt" {
			imdbStr = imdbStr[2:]
		}
		if id, err := strconv.Atoi(imdbStr); err == nil {
			params.IMDbID = &id
		}
	}
	if req.SeasonNumber != nil {
		params.SeasonNumber = req.SeasonNumber
	}
	if req.EpisodeNumber != nil {
		params.EpisodeNumber = req.EpisodeNumber
	}
	if req.FileHash != "" {
		params.MovieHash = req.FileHash
	}
	if req.Title != "" && req.TMDbID == 0 && req.IMDbID == "" {
		params.Query = req.Title
	}

	results, err := client.Search(params)
	if err != nil {
		return nil, err
	}

	var out []SearchResult
	for _, r := range results {
		if len(r.Attributes.Files) == 0 {
			continue
		}
		for _, f := range r.Attributes.Files {
			out = append(out, SearchResult{
				ProviderName:     "opensubtitles",
				ProviderFileID:   strconv.Itoa(f.FileID),
				Language:         r.Attributes.Language,
				ReleaseName:      r.Attributes.Release,
				FileName:         f.FileName,
				HearingImpaired:  r.Attributes.HearingImpaired,
				ForeignPartsOnly: r.Attributes.ForeignPartsOnly,
				DownloadCount:    r.Attributes.DownloadCount,
				HashMatch:        r.Attributes.MovieHashMatch,
				Trusted:          r.Attributes.FromTrusted,
			})
		}
	}
	return out, nil
}

func (o *OpenSubtitlesProvider) Download(_ context.Context, providerFileID string) (*DownloadedFile, error) {
	client, err := o.provider.Client()
	if err != nil {
		return nil, fmt.Errorf("opensubtitles client: %w", err)
	}

	fileID, err := strconv.Atoi(providerFileID)
	if err != nil {
		return nil, fmt.Errorf("invalid file ID %q: %w", providerFileID, err)
	}

	dlResult, err := client.Download(fileID)
	if err != nil {
		return nil, err
	}

	data, err := client.FetchFile(dlResult.Link)
	if err != nil {
		return nil, err
	}

	format := "srt"
	if name := dlResult.FileName; name != "" {
		if idx := lastDotIndex(name); idx >= 0 {
			format = name[idx+1:]
		}
	}

	return &DownloadedFile{
		FileName: dlResult.FileName,
		Data:     data,
		Format:   format,
	}, nil
}

func lastDotIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

package apiv1

import (
	"context"
	"errors"
	"net/http"

	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/subtitle"
)

func (h *Handlers) SearchSubtitles(ctx context.Context, req SearchSubtitlesRequestObject) (SearchSubtitlesResponseObject, error) {
	var seasonNumber, episodeNumber *int
	if req.Params.SeasonNumber != nil {
		seasonNumber = req.Params.SeasonNumber
	}
	if req.Params.EpisodeNumber != nil {
		episodeNumber = req.Params.EpisodeNumber
	}

	results, err := h.subtitleSvc.Search(ctx, uint(req.Params.MediaItemId), seasonNumber, episodeNumber)
	if err != nil {
		return SearchSubtitles400JSONResponse{Code: http.StatusBadRequest, Message: err.Error()}, nil
	}

	apiResults := make([]SubtitleSearchResult, len(results))
	for i, r := range results {
		apiResults[i] = subtitleSearchResultToAPI(r)
	}
	return SearchSubtitles200JSONResponse{Results: apiResults}, nil
}

func (h *Handlers) DownloadSubtitle(ctx context.Context, req DownloadSubtitleRequestObject) (DownloadSubtitleResponseObject, error) {
	body := req.Body
	if body == nil {
		return DownloadSubtitle400JSONResponse{Code: http.StatusBadRequest, Message: "request body required"}, nil
	}

	sub, err := h.subtitleSvc.Download(ctx,
		uint(body.MediaItemId),
		body.ProviderName,
		body.ProviderFileId,
		body.Language,
		body.SeasonNumber,
		body.EpisodeNumber,
		nil,
	)
	if err != nil {
		return DownloadSubtitle400JSONResponse{Code: http.StatusBadRequest, Message: err.Error()}, nil
	}

	return DownloadSubtitle201JSONResponse(subtitleToAPI(sub)), nil
}

func (h *Handlers) ListSubtitles(_ context.Context, req ListSubtitlesRequestObject) (ListSubtitlesResponseObject, error) {
	subs, err := h.subtitleSvc.List(uint(req.Params.MediaItemId))
	if err != nil {
		return nil, err
	}

	items := make([]Subtitle, len(subs))
	for i, s := range subs {
		items[i] = subtitleToAPI(&s)
	}
	return ListSubtitles200JSONResponse{Items: items}, nil
}

func (h *Handlers) DeleteSubtitle(_ context.Context, req DeleteSubtitleRequestObject) (DeleteSubtitleResponseObject, error) {
	if err := h.subtitleSvc.Delete(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteSubtitle404JSONResponse{Code: http.StatusNotFound, Message: "subtitle not found"}, nil
		}
		return nil, err
	}
	return DeleteSubtitle204Response{}, nil
}

func (h *Handlers) TestOpenSubtitlesConnection(_ context.Context, req TestOpenSubtitlesConnectionRequestObject) (TestOpenSubtitlesConnectionResponseObject, error) {
	body := req.Body
	var apiKey, username, password *string
	if body != nil {
		apiKey = body.ApiKey
		username = body.Username
		password = body.Password
	}

	success, message, _ := h.settings.TestOpenSubtitles(apiKey, username, password)
	return TestOpenSubtitlesConnection200JSONResponse{
		Success: success,
		Message: &message,
	}, nil
}

func subtitleToAPI(s *store.Subtitle) Subtitle {
	result := Subtitle{
		Id:          int(s.ID),
		MediaItemId: int(s.MediaItemID),
		Language:    s.Language,
		Provider:    s.Provider,
		FileName:    s.FileName,
		FilePath:    s.FilePath,
		Source:      SubtitleSource(s.Source),
		CreatedAt:   &s.CreatedAt,
	}
	if s.MediaFileID != nil {
		v := int(*s.MediaFileID)
		result.MediaFileId = &v
	}
	if s.SeasonNumber != nil {
		result.SeasonNumber = s.SeasonNumber
	}
	if s.EpisodeNumber != nil {
		result.EpisodeNumber = s.EpisodeNumber
	}
	if s.ProviderFileID != "" {
		result.ProviderFileId = &s.ProviderFileID
	}
	if s.ReleaseName != "" {
		result.ReleaseName = &s.ReleaseName
	}
	if s.Format != "" {
		result.Format = &s.Format
	}
	if s.Score != 0 {
		result.Score = &s.Score
	}
	if s.HearingImpaired {
		v := true
		result.HearingImpaired = &v
	}
	if s.ForeignPartsOnly {
		v := true
		result.ForeignPartsOnly = &v
	}
	return result
}

func subtitleSearchResultToAPI(r subtitle.SearchResult) SubtitleSearchResult {
	result := SubtitleSearchResult{
		ProviderName:   r.ProviderName,
		ProviderFileId: r.ProviderFileID,
		Language:       r.Language,
		Score:          r.Score,
	}
	if r.ReleaseName != "" {
		result.ReleaseName = &r.ReleaseName
	}
	if r.FileName != "" {
		result.FileName = &r.FileName
	}
	if r.HearingImpaired {
		v := true
		result.HearingImpaired = &v
	}
	if r.ForeignPartsOnly {
		v := true
		result.ForeignPartsOnly = &v
	}
	if r.DownloadCount > 0 {
		result.DownloadCount = &r.DownloadCount
	}
	if r.HashMatch {
		v := true
		result.HashMatch = &v
	}
	if r.Trusted {
		v := true
		result.Trusted = &v
	}
	return result
}

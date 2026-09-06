package apiv1

import (
	"context"
	"net/http"
	"time"
)

func (h *Handlers) GetEpisodeTimeline(_ context.Context, req GetEpisodeTimelineRequestObject) (GetEpisodeTimelineResponseObject, error) {
	from, fromErr := time.Parse(time.DateOnly, req.Params.From)
	to, toErr := time.Parse(time.DateOnly, req.Params.To)
	if fromErr != nil || toErr != nil || len(req.Params.From) != 10 || len(req.Params.To) != 10 ||
		!to.After(from) || to.After(from.AddDate(0, 0, 31)) {
		return GetEpisodeTimeline400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: "from and to must be YYYY-MM-DD dates defining a positive window of at most 31 days (to is exclusive)",
		}, nil
	}

	rows, err := h.syncSvc.AssembleEpisodeTimeline(req.Params.From, req.Params.To)
	if err != nil {
		return nil, err
	}
	items := make([]EpisodeTimelineItem, len(rows))
	for i, row := range rows {
		ep := row.Episode
		items[i] = EpisodeTimelineItem{
			SeriesTitle: row.SeriesTitle,
			Episode: Episode{
				Id: int64(ep.ID), MediaItemId: int64(ep.MediaItemID),
				SeasonNumber: ep.SeasonNumber, EpisodeNumber: ep.EpisodeNumber,
				AirDate: &ep.AirDate, Runtime: ep.Runtime,
				HasFile: &row.HasFile, Monitored: &row.Monitored,
			},
		}
		if ep.Title != "" {
			items[i].Episode.Title = &ep.Title
		}
		if ep.Overview != "" {
			items[i].Episode.Overview = &ep.Overview
		}
		if row.DownloadStatus != "" {
			status := EpisodeDownloadStatus(row.DownloadStatus)
			items[i].Episode.DownloadStatus = &status
		}
		if row.PosterPath != "" {
			items[i].PosterPath = &row.PosterPath
		}
	}
	return GetEpisodeTimeline200JSONResponse{Items: items}, nil
}

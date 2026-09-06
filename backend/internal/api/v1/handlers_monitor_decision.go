package apiv1

import (
	"context"
	"errors"

	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) GetMonitorDecision(_ context.Context, req GetMonitorDecisionRequestObject) (GetMonitorDecisionResponseObject, error) {
	if req.Id <= 0 {
		return GetMonitorDecision404JSONResponse{Code: 404, Message: "media item not found"}, nil
	}
	if _, err := h.store.GetMediaItem(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetMonitorDecision404JSONResponse{Code: 404, Message: "media item not found"}, nil
		}
		return nil, err
	}
	row, err := h.store.GetMonitorDecision(uint(req.Id))
	if errors.Is(err, store.ErrNotFound) || (err == nil && row == nil) {
		return GetMonitorDecision200JSONResponse{}, nil
	}
	if err != nil {
		return nil, err
	}
	decision := &MonitorDecision{
		CheckedAt: row.CheckedAt, Outcome: row.Outcome, Summary: row.Summary,
		InputUpdatedAt: row.InputUpdatedAt,
		Truncated:      row.Truncated, Details: make([]MonitorDecisionDetail, 0, len(row.Details)),
	}
	for _, d := range row.Details {
		detail := MonitorDecisionDetail{
			SeasonNumber: d.SeasonNumber, EpisodeNumber: d.EpisodeNumber,
			Outcome: d.Outcome, Explanation: d.Explanation, TotalResults: d.TotalResults,
			RejectedResults: d.RejectedResults, BlockedResults: d.BlockedResults,
		}
		if d.SelectedTitle != "" {
			detail.SelectedTitle = &d.SelectedTitle
		}
		if d.DownloadID != nil {
			id := int64(*d.DownloadID)
			detail.DownloadId = &id
		}
		decision.Details = append(decision.Details, detail)
	}
	return GetMonitorDecision200JSONResponse{Decision: decision}, nil
}

package apiv1

import (
	"context"

	"github.com/sumia01/media-gate/internal/settings"
)

func (h *Handlers) ListJobs(_ context.Context, _ ListJobsRequestObject) (ListJobsResponseObject, error) {
	jobs := h.queue.ListJobs()
	apiJobs := make([]Job, len(jobs))
	for i, j := range jobs {
		apiJobs[i] = jobToAPI(j)
	}
	return ListJobs200JSONResponse{Jobs: apiJobs}, nil
}

func (h *Handlers) ListSettings(_ context.Context, _ ListSettingsRequestObject) (ListSettingsResponseObject, error) {
	items, err := h.settings.List()
	if err != nil {
		return nil, err
	}
	apiSettings := make([]Setting, len(items))
	for i, s := range items {
		apiSettings[i] = settingToAPI(&s)
	}
	return ListSettings200JSONResponse{Settings: apiSettings}, nil
}

func (h *Handlers) UpdateSettings(_ context.Context, req UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	kvs := make([]settings.KeyValue, len(req.Body.Settings))
	for i, s := range req.Body.Settings {
		kvs[i] = settings.KeyValue{Key: s.Key, Value: s.Value}
	}
	if err := h.settings.Update(kvs); err != nil {
		return nil, err
	}

	items, err := h.settings.List()
	if err != nil {
		return nil, err
	}
	apiSettings := make([]Setting, len(items))
	for i, s := range items {
		apiSettings[i] = settingToAPI(&s)
	}
	return UpdateSettings200JSONResponse{Settings: apiSettings}, nil
}

func (h *Handlers) TestTmdbConnection(_ context.Context, req TestTmdbConnectionRequestObject) (TestTmdbConnectionResponseObject, error) {
	success, msg, err := h.settings.TestTMDB(req.Body.ApiKey)
	if err != nil {
		return nil, err
	}
	return TestTmdbConnection200JSONResponse{Success: success, Message: &msg}, nil
}

func (h *Handlers) TestTvdbConnection(_ context.Context, req TestTvdbConnectionRequestObject) (TestTvdbConnectionResponseObject, error) {
	success, msg, err := h.settings.TestTVDB(req.Body.ApiKey)
	if err != nil {
		return nil, err
	}
	return TestTvdbConnection200JSONResponse{Success: success, Message: &msg}, nil
}

func (h *Handlers) TestQbittorrentConnection(_ context.Context, req TestQbittorrentConnectionRequestObject) (TestQbittorrentConnectionResponseObject, error) {
	success, msg, err := h.settings.TestQBit(req.Body.Url, req.Body.Username, req.Body.Password)
	if err != nil {
		return nil, err
	}
	return TestQbittorrentConnection200JSONResponse{Success: success, Message: &msg}, nil
}

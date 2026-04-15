package apiv1

import "context"

func (h *Handlers) GetUpdateStatus(_ context.Context, _ GetUpdateStatusRequestObject) (GetUpdateStatusResponseObject, error) {
	if h.updaterSvc == nil {
		return GetUpdateStatus200JSONResponse{
			Enabled:        false,
			CurrentVersion: h.version,
		}, nil
	}

	resp := GetUpdateStatus200JSONResponse{
		Enabled:        true,
		CurrentVersion: h.updaterSvc.Version(),
	}

	if rel := h.updaterSvc.Latest(); rel != nil {
		avail := true
		resp.UpdateAvailable = &avail
		resp.LatestVersion = &rel.TagName
		resp.ReleaseNotes = &rel.Body
		pub := rel.PublishedAt.Format("2006-01-02T15:04:05Z")
		resp.PublishedAt = &pub
	}

	return resp, nil
}

func (h *Handlers) CheckForUpdate(_ context.Context, _ CheckForUpdateRequestObject) (CheckForUpdateResponseObject, error) {
	if h.updaterSvc == nil {
		avail := false
		return CheckForUpdate200JSONResponse{
			Enabled:        false,
			Available:      avail,
			CurrentVersion: h.version,
		}, nil
	}

	rel, err := h.updaterSvc.CheckNow()
	if err != nil {
		return nil, err
	}

	resp := CheckForUpdate200JSONResponse{
		Enabled:        true,
		Available:      rel != nil,
		CurrentVersion: h.updaterSvc.Version(),
	}

	if rel != nil {
		resp.LatestVersion = &rel.TagName
		resp.ReleaseNotes = &rel.Body
		pub := rel.PublishedAt.Format("2006-01-02T15:04:05Z")
		resp.PublishedAt = &pub
	}

	return resp, nil
}

func (h *Handlers) ApplyUpdate(_ context.Context, _ ApplyUpdateRequestObject) (ApplyUpdateResponseObject, error) {
	if h.updaterSvc == nil {
		return ApplyUpdate200JSONResponse{
			Success: false,
			Message: "Update feature is not available",
		}, nil
	}

	if err := h.updaterSvc.Apply(); err != nil {
		return ApplyUpdate200JSONResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	// Unreachable after successful Apply (process re-execs), but needed for compilation.
	return ApplyUpdate200JSONResponse{
		Success: true,
		Message: "Update applied",
	}, nil
}

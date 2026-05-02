package apiv1

import "context"

func (h *Handlers) ListWorkers(_ context.Context, _ ListWorkersRequestObject) (ListWorkersResponseObject, error) {
	statuses := h.workerRegistry.All()
	workers := make([]Worker, 0, len(statuses))
	for _, s := range statuses {
		w := Worker{
			Name:     s.Name,
			Running:  s.Running,
			Interval: s.Interval.String(),
		}
		if !s.LastRunAt.IsZero() {
			t := s.LastRunAt
			w.LastRunAt = &t
		}
		if !s.NextRunAt.IsZero() {
			t := s.NextRunAt
			w.NextRunAt = &t
		}
		workers = append(workers, w)
	}
	return ListWorkers200JSONResponse{Workers: workers}, nil
}

func (h *Handlers) RunWorker(_ context.Context, req RunWorkerRequestObject) (RunWorkerResponseObject, error) {
	if !h.workerRegistry.RunByName(req.Name) {
		return RunWorker404JSONResponse{Code: 404, Message: "worker not found: " + req.Name}, nil
	}
	return RunWorker204Response{}, nil
}

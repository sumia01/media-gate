package apiv1

import "context"

// Ensure Handlers implements the generated StrictServerInterface.
var _ StrictServerInterface = (*Handlers)(nil)

type Handlers struct{}

func NewHandlers() *Handlers {
	return &Handlers{}
}

func (h *Handlers) GetHealth(_ context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: "ok"}, nil
}

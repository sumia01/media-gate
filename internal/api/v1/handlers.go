package apiv1

import (
	"context"
	"errors"
	"net/http"

	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/store"
)

// Ensure Handlers implements the generated StrictServerInterface.
var _ StrictServerInterface = (*Handlers)(nil)

type Handlers struct {
	lib *library.Service
}

func NewHandlers(lib *library.Service) *Handlers {
	return &Handlers{lib: lib}
}

func (h *Handlers) GetHealth(_ context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: "ok"}, nil
}

func (h *Handlers) CreateLibrary(_ context.Context, req CreateLibraryRequestObject) (CreateLibraryResponseObject, error) {
	lib := &store.Library{
		Name:      req.Body.Name,
		Path:      req.Body.Path,
		MediaType: string(req.Body.MediaType),
	}

	if err := h.lib.Create(lib); err != nil {
		return CreateLibrary400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateLibrary201JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) ListLibraries(_ context.Context, _ ListLibrariesRequestObject) (ListLibrariesResponseObject, error) {
	libs, err := h.lib.List()
	if err != nil {
		return nil, err
	}

	result := make(ListLibraries200JSONResponse, len(libs))
	for i := range libs {
		result[i] = libraryToAPI(&libs[i])
	}
	return result, nil
}

func (h *Handlers) GetLibrary(_ context.Context, req GetLibraryRequestObject) (GetLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	return GetLibrary200JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) UpdateLibrary(_ context.Context, req UpdateLibraryRequestObject) (UpdateLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	lib.Name = req.Body.Name
	lib.Path = req.Body.Path
	lib.MediaType = string(req.Body.MediaType)

	if err := h.lib.Update(lib); err != nil {
		return nil, err
	}

	return UpdateLibrary200JSONResponse(libraryToAPI(lib)), nil
}

func (h *Handlers) DeleteLibrary(_ context.Context, req DeleteLibraryRequestObject) (DeleteLibraryResponseObject, error) {
	if err := h.lib.Delete(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	return DeleteLibrary204Response{}, nil
}

func (h *Handlers) ScanLibrary(_ context.Context, req ScanLibraryRequestObject) (ScanLibraryResponseObject, error) {
	lib, err := h.lib.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ScanLibrary404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "library not found",
			}, nil
		}
		return nil, err
	}

	entries, err := h.lib.Scan(lib)
	if err != nil {
		return ScanLibrary400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	apiEntries := make([]ScanEntry, len(entries))
	for i, e := range entries {
		apiEntries[i] = ScanEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDirectory,
			Size:        e.Size,
			ModifiedAt:  e.ModifiedAt,
		}
	}

	return ScanLibrary200JSONResponse{Entries: apiEntries}, nil
}

func (h *Handlers) BrowseFolder(_ context.Context, req BrowseFolderRequestObject) (BrowseFolderResponseObject, error) {
	var path string
	if req.Params.Path != nil {
		path = *req.Params.Path
	}

	browsedPath, entries, err := h.lib.Browse(path)
	if err != nil {
		return BrowseFolder400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	apiEntries := make([]ScanEntry, len(entries))
	for i, e := range entries {
		apiEntries[i] = ScanEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDirectory,
			Size:        e.Size,
			ModifiedAt:  e.ModifiedAt,
		}
	}

	return BrowseFolder200JSONResponse{
		Path:    browsedPath,
		Entries: apiEntries,
	}, nil
}

func libraryToAPI(lib *store.Library) Library {
	return Library{
		Id:        int64(lib.ID),
		Name:      lib.Name,
		Path:      lib.Path,
		MediaType: LibraryMediaType(lib.MediaType),
		CreatedAt: lib.CreatedAt,
		UpdatedAt: lib.UpdatedAt,
	}
}

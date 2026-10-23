package apiv1

import (
	"context"
	"fmt"

	"github.com/sumia01/media-gate/internal/integration/mediaserver"
	"github.com/sumia01/media-gate/internal/settings"
)

func (h *Handlers) TestJellyfinConnection(_ context.Context, request TestJellyfinConnectionRequestObject) (TestJellyfinConnectionResponseObject, error) {
	ok, msg, err := h.settings.TestJellyfin(request.Body.Url, request.Body.ApiKey)
	if err != nil {
		return nil, err
	}
	return TestJellyfinConnection200JSONResponse{Success: ok, Message: &msg}, nil
}

func (h *Handlers) ListJellyfinLibraries(_ context.Context, _ ListJellyfinLibrariesRequestObject) (ListJellyfinLibrariesResponseObject, error) {
	client, err := h.jellyfinProvider.Client()
	if err != nil {
		return ListJellyfinLibraries502Response{}, nil
	}
	libraries, err := client.ListLibraries()
	if err != nil {
		return ListJellyfinLibraries502Response{}, nil
	}
	result := make([]JellyfinLibrary, 0, len(libraries))
	for _, lib := range libraries {
		result = append(result, JellyfinLibrary{Id: lib.ID, Title: lib.Title, Type: JellyfinLibraryType(lib.Type), Locations: lib.Locations})
	}
	return ListJellyfinLibraries200JSONResponse{Libraries: result}, nil
}

func (h *Handlers) jellyfinMappings() ([]JellyfinMapping, error) {
	libs, err := h.store.ListLibraries()
	if err != nil {
		return nil, fmt.Errorf("listing libraries: %w", err)
	}
	var sections []mediaserver.Section
	if client, err := h.jellyfinProvider.Client(); err == nil {
		sections, _ = client.ListLibraries()
	}
	mappings := make([]JellyfinMapping, 0, len(libs))
	for _, lib := range libs {
		mapping := JellyfinMapping{LibraryId: int(lib.ID), LibraryName: lib.Name, LibraryPath: lib.Path, LibraryType: lib.MediaType}
		savedID, err := h.settings.Get(fmt.Sprintf("jellyfin:mapping:%d", lib.ID))
		if err == nil {
			// A saved empty mapping is intentional, not an auto-match request.
			mapping.JellyfinLibraryId = savedID
			if section := mediaserver.FindSection(sections, savedID); section != nil {
				mapping.JellyfinLibraryTitle = section.Title
			}
		} else if match := mediaserver.AutoMatch(lib.Path, lib.MediaType, sections); match != nil {
			mapping.JellyfinLibraryId = match.SectionID
			mapping.JellyfinLibraryTitle = match.SectionTitle
			mapping.AutoMatched = true
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

func (h *Handlers) ListJellyfinMappings(_ context.Context, _ ListJellyfinMappingsRequestObject) (ListJellyfinMappingsResponseObject, error) {
	mappings, err := h.jellyfinMappings()
	if err != nil {
		return nil, err
	}
	return ListJellyfinMappings200JSONResponse{Mappings: mappings}, nil
}

func (h *Handlers) UpdateJellyfinMappings(_ context.Context, request UpdateJellyfinMappingsRequestObject) (UpdateJellyfinMappingsResponseObject, error) {
	items := make([]settings.KeyValue, 0, len(request.Body.Mappings))
	for _, mapping := range request.Body.Mappings {
		items = append(items, settings.KeyValue{Key: fmt.Sprintf("jellyfin:mapping:%d", mapping.LibraryId), Value: mapping.JellyfinLibraryId})
	}
	if err := h.settings.Update(items); err != nil {
		return nil, err
	}
	mappings, err := h.jellyfinMappings()
	if err != nil {
		return nil, err
	}
	return UpdateJellyfinMappings200JSONResponse{Mappings: mappings}, nil
}

func (h *Handlers) RefreshJellyfinLibrary(_ context.Context, request RefreshJellyfinLibraryRequestObject) (RefreshJellyfinLibraryResponseObject, error) {
	if !h.settings.MediaServerRefreshEnabled(settings.KeyJellyfinRefreshEnabled) {
		return RefreshJellyfinLibrary409Response{}, nil
	}
	if err := h.jellyfinProvider.RefreshLibrary(request.LibraryId); err != nil {
		return RefreshJellyfinLibrary502Response{}, nil
	}
	return RefreshJellyfinLibrary204Response{}, nil
}

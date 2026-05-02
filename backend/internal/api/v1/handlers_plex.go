package apiv1

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sumia01/media-gate/internal/integration/plex"
	"github.com/sumia01/media-gate/internal/settings"
)

// TestPlexConnection tests the Plex Media Server connectivity.
func (h *Handlers) TestPlexConnection(_ context.Context, request TestPlexConnectionRequestObject) (TestPlexConnectionResponseObject, error) {
	ok, msg, err := h.settings.TestPlex(request.Body.Url, request.Body.Token)
	if err != nil {
		return nil, err
	}
	return TestPlexConnection200JSONResponse{Success: ok, Message: &msg}, nil
}

// ListPlexSections returns all library sections from the configured Plex server.
func (h *Handlers) ListPlexSections(_ context.Context, _ ListPlexSectionsRequestObject) (ListPlexSectionsResponseObject, error) {
	client, err := h.plexProvider.Client()
	if err != nil {
		return ListPlexSections502Response{}, nil
	}

	sections, err := client.ListSections()
	if err != nil {
		return ListPlexSections502Response{}, nil
	}

	apiSections := make([]PlexSection, 0, len(sections))
	for _, s := range sections {
		apiSections = append(apiSections, PlexSection{
			Id:        s.ID,
			Title:     s.Title,
			Type:      PlexSectionType(s.Type),
			Locations: s.Locations,
		})
	}

	return ListPlexSections200JSONResponse{Sections: apiSections}, nil
}

// ListPlexMappings returns the current library-to-Plex section mappings.
func (h *Handlers) ListPlexMappings(_ context.Context, _ ListPlexMappingsRequestObject) (ListPlexMappingsResponseObject, error) {
	libs, err := h.store.ListLibraries()
	if err != nil {
		return nil, fmt.Errorf("listing libraries: %w", err)
	}

	// Try to get Plex sections for auto-match info.
	var sections []plex.Section
	if client, err := h.plexProvider.Client(); err == nil {
		sections, _ = client.ListSections()
	}

	mappings := make([]PlexMapping, 0, len(libs))
	for _, lib := range libs {
		mapping := PlexMapping{
			LibraryId:   int(lib.ID),
			LibraryName: lib.Name,
			LibraryPath: lib.Path,
			LibraryType: lib.MediaType,
		}

		// Check for saved mapping.
		key := fmt.Sprintf("plex:mapping:%d", lib.ID)
		savedID, err := h.settings.Get(key)
		if err == nil && savedID != "" {
			mapping.PlexSectionId = savedID
			mapping.AutoMatched = false
			// Find section title.
			if s := plex.FindSection(sections, savedID); s != nil {
				mapping.PlexSectionTitle = s.Title
			}
		} else if len(sections) > 0 {
			// Auto-match.
			if match := plex.AutoMatch(lib.Path, lib.MediaType, sections); match != nil {
				mapping.PlexSectionId = match.SectionID
				mapping.PlexSectionTitle = match.SectionTitle
				mapping.AutoMatched = true
			}
		}

		mappings = append(mappings, mapping)
	}

	return ListPlexMappings200JSONResponse{Mappings: mappings}, nil
}

// UpdatePlexMappings saves library-to-Plex section mappings.
func (h *Handlers) UpdatePlexMappings(_ context.Context, request UpdatePlexMappingsRequestObject) (UpdatePlexMappingsResponseObject, error) {
	for _, m := range request.Body.Mappings {
		key := fmt.Sprintf("plex:mapping:%d", m.LibraryId)
		if err := h.settings.Update([]settings.KeyValue{{Key: key, Value: m.PlexSectionId}}); err != nil {
			return nil, fmt.Errorf("saving mapping for library %d: %w", m.LibraryId, err)
		}
	}

	// Return the updated mappings.
	libs, err := h.store.ListLibraries()
	if err != nil {
		return nil, fmt.Errorf("listing libraries: %w", err)
	}

	var sections []plex.Section
	if client, err := h.plexProvider.Client(); err == nil {
		sections, _ = client.ListSections()
	}

	mappings := make([]PlexMapping, 0, len(libs))
	for _, lib := range libs {
		mapping := PlexMapping{
			LibraryId:   int(lib.ID),
			LibraryName: lib.Name,
			LibraryPath: lib.Path,
			LibraryType: lib.MediaType,
		}

		key := fmt.Sprintf("plex:mapping:%d", lib.ID)
		savedID, err := h.settings.Get(key)
		if err == nil && savedID != "" {
			mapping.PlexSectionId = savedID
			mapping.AutoMatched = false
			if s := plex.FindSection(sections, savedID); s != nil {
				mapping.PlexSectionTitle = s.Title
			}
		}

		mappings = append(mappings, mapping)
	}

	return UpdatePlexMappings200JSONResponse{Mappings: mappings}, nil
}

// RefreshPlexSection triggers a Plex library scan on a specific section.
func (h *Handlers) RefreshPlexSection(_ context.Context, request RefreshPlexSectionRequestObject) (RefreshPlexSectionResponseObject, error) {
	client, err := h.plexProvider.Client()
	if err != nil {
		return RefreshPlexSection502Response{}, nil
	}

	if err := client.RefreshSection(request.SectionId); err != nil {
		return RefreshPlexSection502Response{}, nil
	}

	return RefreshPlexSection204Response{}, nil
}

// plexMappingKey returns the settings key for a library's Plex section mapping.
func plexMappingKey(libraryID uint) string {
	return "plex:mapping:" + strconv.FormatUint(uint64(libraryID), 10)
}

package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) ListIndexerDefinitions(_ context.Context, _ ListIndexerDefinitionsRequestObject) (ListIndexerDefinitionsResponseObject, error) {
	defs := h.indexerSvc.ListDefinitions()
	apiDefs := make([]IndexerDefinition, len(defs))
	for i, d := range defs {
		apiDefs[i] = definitionToAPI(&d)
	}
	return ListIndexerDefinitions200JSONResponse{Definitions: apiDefs}, nil
}

func (h *Handlers) GetIndexerDefinition(_ context.Context, req GetIndexerDefinitionRequestObject) (GetIndexerDefinitionResponseObject, error) {
	def, err := h.indexerSvc.GetDefinition(req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetIndexerDefinition404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer definition not found",
			}, nil
		}
		return nil, err
	}
	return GetIndexerDefinition200JSONResponse(definitionToAPI(def)), nil
}

func (h *Handlers) ListIndexers(_ context.Context, _ ListIndexersRequestObject) (ListIndexersResponseObject, error) {
	indexers, err := h.indexerSvc.List()
	if err != nil {
		return nil, err
	}
	apiIndexers := make([]Indexer, len(indexers))
	for i, idx := range indexers {
		apiIndexers[i] = indexerInfoToAPI(&idx)
	}
	return ListIndexers200JSONResponse{Indexers: apiIndexers}, nil
}

func (h *Handlers) CreateIndexer(_ context.Context, req CreateIndexerRequestObject) (CreateIndexerResponseObject, error) {
	settings := make(map[string]string)
	if req.Body.Settings != nil {
		settings = *req.Body.Settings
	}

	priority := 0
	if req.Body.Priority != nil {
		priority = *req.Body.Priority
	}

	var seedMinRatio float64
	if req.Body.SeedMinRatio != nil {
		seedMinRatio = float64(*req.Body.SeedMinRatio)
	}

	var seedMinTime int
	if req.Body.SeedMinTime != nil {
		seedMinTime = *req.Body.SeedMinTime
	}

	info, err := h.indexerSvc.Create(req.Body.Name, req.Body.DefinitionId, settings, priority, seedMinRatio, seedMinTime)
	if err != nil {
		return CreateIndexer400JSONResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		}, nil
	}

	return CreateIndexer201JSONResponse(indexerInfoToAPI(info)), nil
}

func (h *Handlers) GetIndexer(_ context.Context, req GetIndexerRequestObject) (GetIndexerResponseObject, error) {
	info, err := h.indexerSvc.Get(uint(req.Id))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetIndexer404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return GetIndexer200JSONResponse(indexerInfoToAPI(info)), nil
}

func (h *Handlers) UpdateIndexer(_ context.Context, req UpdateIndexerRequestObject) (UpdateIndexerResponseObject, error) {
	var settings map[string]string
	if req.Body.Settings != nil {
		settings = *req.Body.Settings
	}

	var seedMinRatio *float64
	if req.Body.SeedMinRatio != nil {
		v := float64(*req.Body.SeedMinRatio)
		seedMinRatio = &v
	}

	var seedMinTime *int
	if req.Body.SeedMinTime != nil {
		seedMinTime = req.Body.SeedMinTime
	}

	info, err := h.indexerSvc.Update(uint(req.Id), req.Body.Name, settings, req.Body.Enabled, req.Body.Priority, seedMinRatio, seedMinTime)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateIndexer404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return UpdateIndexer200JSONResponse(indexerInfoToAPI(info)), nil
}

func (h *Handlers) DeleteIndexer(_ context.Context, req DeleteIndexerRequestObject) (DeleteIndexerResponseObject, error) {
	if err := h.indexerSvc.Delete(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteIndexer404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return DeleteIndexer204Response{}, nil
}

func (h *Handlers) TestIndexerConnection(ctx context.Context, req TestIndexerConnectionRequestObject) (TestIndexerConnectionResponseObject, error) {
	var overrideSettings map[string]string
	if req.Body != nil && req.Body.Settings != nil {
		overrideSettings = *req.Body.Settings
	}

	success, msg, err := h.indexerSvc.TestConnection(uint(req.Id), overrideSettings)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TestIndexerConnection404JSONResponse{
				Code:    http.StatusNotFound,
				Message: "indexer not found",
			}, nil
		}
		return nil, err
	}
	return TestIndexerConnection200JSONResponse{Success: success, Message: &msg}, nil
}

func (h *Handlers) SearchIndexers(ctx context.Context, req SearchIndexersRequestObject) (SearchIndexersResponseObject, error) {
	params := indexer.SearchParams{}

	if req.Params.Query != nil {
		params.Query = *req.Params.Query
	}
	if req.Params.ImdbId != nil {
		params.ImdbID = *req.Params.ImdbId
	}
	if req.Params.Type != nil {
		params.Type = string(*req.Params.Type)
	}
	if req.Params.Season != nil {
		params.Season = *req.Params.Season
	}
	if req.Params.Episode != nil {
		params.Episode = *req.Params.Episode
	}
	if req.Params.Categories != nil {
		params.Categories = strings.Split(*req.Params.Categories, ",")
	}
	if req.Params.IndexerIds != nil {
		for _, idStr := range strings.Split(*req.Params.IndexerIds, ",") {
			id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 64)
			if err == nil {
				params.IndexerIDs = append(params.IndexerIDs, uint(id))
			}
		}
	}
	if req.Params.Limit != nil {
		params.Limit = *req.Params.Limit
	}

	results, err := h.indexerSvc.Search(ctx, params)
	if err != nil {
		return nil, err
	}

	// Resolve profile for annotation if profileId is provided
	var profile *store.MediaProfile
	var globalTags []string
	if req.Params.ProfileId != nil {
		p, err := h.store.GetMediaProfile(uint(*req.Params.ProfileId))
		if err != nil {
			return SearchIndexers200JSONResponse{Results: make([]TorrentResult, 0)}, nil
		}
		profile = p
		if raw, err := h.settings.Get(settings.KeyGlobalExcludeTags); err == nil && raw != "" {
			_ = json.Unmarshal([]byte(raw), &globalTags)
		}
	}

	apiResults := make([]TorrentResult, len(results))
	for i, r := range results {
		apiResults[i] = torrentResultToAPI(&r)
		if profile != nil {
			match := indexer.MatchesMediaProfile(&r, profile, globalTags...)
			apiResults[i].ProfileMatch = &match
		}
	}

	return SearchIndexers200JSONResponse{Results: apiResults}, nil
}

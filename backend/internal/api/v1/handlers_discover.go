package apiv1

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) GetRecentlyAdded(_ context.Context, _ GetRecentlyAddedRequestObject) (GetRecentlyAddedResponseObject, error) {
	items, err := h.store.ListRecentMediaItems(20)
	if err != nil {
		return nil, err
	}

	ids := make([]uint, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	metas, err := h.store.ListMediaMetadataByMediaItemIDs(ids)
	if err != nil {
		return nil, err
	}
	metaMap := make(map[uint]int, len(metas))
	for i, m := range metas {
		metaMap[m.MediaItemID] = i
	}

	apiItems := make([]MediaItem, len(items))
	for i := range items {
		var meta *store.MediaMetadata
		if idx, ok := metaMap[items[i].ID]; ok {
			meta = &metas[idx]
		}
		apiItems[i] = mediaItemToAPI(&items[i], meta)
	}

	return GetRecentlyAdded200JSONResponse{Items: apiItems}, nil
}

func (h *Handlers) GetTrending(_ context.Context, _ GetTrendingRequestObject) (GetTrendingResponseObject, error) {
	apiKey, err := h.settings.Get(settings.KeyTMDBApiKey)
	if err != nil || apiKey == "" {
		return GetTrending200JSONResponse{Items: []DiscoverItem{}}, nil
	}

	client := tmdb.NewClient(apiKey)
	results, err := client.TrendingAll("week")
	if err != nil {
		slog.Warn("discover: trending fetch failed", "error", err)
		return GetTrending200JSONResponse{Items: []DiscoverItem{}}, nil
	}

	items := make([]DiscoverItem, len(results))
	for i, r := range results {
		items[i] = trendingResultToDiscoverItem(r)
	}
	return GetTrending200JSONResponse{Items: items}, nil
}

func (h *Handlers) GetPopularMovies(_ context.Context, _ GetPopularMoviesRequestObject) (GetPopularMoviesResponseObject, error) {
	apiKey, err := h.settings.Get(settings.KeyTMDBApiKey)
	if err != nil || apiKey == "" {
		return GetPopularMovies200JSONResponse{Items: []DiscoverItem{}}, nil
	}

	client := tmdb.NewClient(apiKey)
	results, err := client.PopularMovies()
	if err != nil {
		slog.Warn("discover: popular movies fetch failed", "error", err)
		return GetPopularMovies200JSONResponse{Items: []DiscoverItem{}}, nil
	}

	items := make([]DiscoverItem, len(results))
	for i, r := range results {
		items[i] = movieResultToDiscoverItem(r)
	}
	return GetPopularMovies200JSONResponse{Items: items}, nil
}

func (h *Handlers) GetPopularSeries(_ context.Context, _ GetPopularSeriesRequestObject) (GetPopularSeriesResponseObject, error) {
	apiKey, err := h.settings.Get(settings.KeyTMDBApiKey)
	if err != nil || apiKey == "" {
		return GetPopularSeries200JSONResponse{Items: []DiscoverItem{}}, nil
	}

	client := tmdb.NewClient(apiKey)
	results, err := client.PopularTV()
	if err != nil {
		slog.Warn("discover: popular series fetch failed", "error", err)
		return GetPopularSeries200JSONResponse{Items: []DiscoverItem{}}, nil
	}

	items := make([]DiscoverItem, len(results))
	for i, r := range results {
		items[i] = tvResultToDiscoverItem(r)
	}
	return GetPopularSeries200JSONResponse{Items: items}, nil
}

const tmdbPosterW342 = "https://image.tmdb.org/t/p/w342"

func trendingResultToDiscoverItem(r tmdb.TrendingResult) DiscoverItem {
	d := DiscoverItem{
		Source:     DiscoverItemSourceTmdb,
		ExternalId: r.ID,
	}
	// Trending /all returns mixed movie+tv with different field names
	if r.MediaType == "movie" {
		d.Title = r.Title
		d.MediaType = DiscoverItemMediaTypeMovie
		if y := parseYear(r.ReleaseDate); y != nil {
			d.Year = y
		}
	} else {
		d.Title = r.Name
		d.MediaType = DiscoverItemMediaTypeSeries
		if y := parseYear(r.FirstAirDate); y != nil {
			d.Year = y
		}
	}
	if r.Overview != "" {
		d.Overview = &r.Overview
	}
	if r.PosterPath != "" {
		u := tmdbPosterW342 + r.PosterPath
		d.PosterUrl = &u
	}
	if r.VoteAverage > 0 {
		rating := float32(math.Round(r.VoteAverage*10) / 10)
		d.Rating = &rating
	}
	return d
}

func movieResultToDiscoverItem(r tmdb.MovieResult) DiscoverItem {
	d := DiscoverItem{
		Source:     DiscoverItemSourceTmdb,
		ExternalId: r.ID,
		Title:      r.Title,
		MediaType:  DiscoverItemMediaTypeMovie,
	}
	if r.Overview != "" {
		d.Overview = &r.Overview
	}
	if y := parseYear(r.ReleaseDate); y != nil {
		d.Year = y
	}
	if r.PosterPath != "" {
		u := tmdbPosterW342 + r.PosterPath
		d.PosterUrl = &u
	}
	if r.VoteAverage > 0 {
		rating := float32(math.Round(r.VoteAverage*10) / 10)
		d.Rating = &rating
	}
	return d
}

func tvResultToDiscoverItem(r tmdb.TVResult) DiscoverItem {
	d := DiscoverItem{
		Source:     DiscoverItemSourceTmdb,
		ExternalId: r.ID,
		Title:      r.Name,
		MediaType:  DiscoverItemMediaTypeSeries,
	}
	if r.Overview != "" {
		d.Overview = &r.Overview
	}
	if y := parseYear(r.FirstAirDate); y != nil {
		d.Year = y
	}
	if r.PosterPath != "" {
		u := tmdbPosterW342 + r.PosterPath
		d.PosterUrl = &u
	}
	if r.VoteAverage > 0 {
		rating := float32(math.Round(r.VoteAverage*10) / 10)
		d.Rating = &rating
	}
	return d
}

func parseYear(date string) *int {
	if len(date) < 4 {
		return nil
	}
	y, err := strconv.Atoi(strings.SplitN(date, "-", 2)[0])
	if err != nil {
		return nil
	}
	return &y
}

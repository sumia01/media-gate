package apiv1

import (
	"context"
	"log/slog"
	"math"

	"github.com/sumia01/media-gate/internal/dateutil"
	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) GetMediaExternalIds(_ context.Context, _ GetMediaExternalIdsRequestObject) (GetMediaExternalIdsResponseObject, error) {
	metas, err := h.store.ListMediaMetadataExternalIDs()
	if err != nil {
		return nil, err
	}
	items := make([]struct {
		ExternalId  int    `json:"externalId"`
		MediaItemId int    `json:"mediaItemId"`
		Source      string `json:"source"`
	}, len(metas))
	for i, m := range metas {
		items[i].Source = m.Source
		items[i].ExternalId = m.ExternalID
		items[i].MediaItemId = int(m.MediaItemID)
	}
	return GetMediaExternalIds200JSONResponse{Items: items}, nil
}

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

func (h *Handlers) GetTrending(_ context.Context, request GetTrendingRequestObject) (GetTrendingResponseObject, error) {
	page := 1
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	items, totalPages, err := h.fetchDiscover(func(c *tmdb.Client) ([]DiscoverItem, int, error) {
		results, tp, err := c.TrendingAll("week", page)
		if err != nil {
			return nil, 0, err
		}
		out := make([]DiscoverItem, len(results))
		for i, r := range results {
			if r.MediaType == "movie" {
				out[i] = toDiscoverItem(r.ID, r.Title, r.ReleaseDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeMovie)
			} else {
				out[i] = toDiscoverItem(r.ID, r.Name, r.FirstAirDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeSeries)
			}
		}
		return out, tp, nil
	})
	if err != nil {
		return nil, err
	}
	return GetTrending200JSONResponse{Items: items, Page: page, TotalPages: totalPages}, nil
}

func (h *Handlers) GetPopularMovies(_ context.Context, request GetPopularMoviesRequestObject) (GetPopularMoviesResponseObject, error) {
	page := 1
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	items, totalPages, err := h.fetchDiscover(func(c *tmdb.Client) ([]DiscoverItem, int, error) {
		results, tp, err := c.PopularMovies(page)
		if err != nil {
			return nil, 0, err
		}
		out := make([]DiscoverItem, len(results))
		for i, r := range results {
			out[i] = toDiscoverItem(r.ID, r.Title, r.ReleaseDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeMovie)
		}
		return out, tp, nil
	})
	if err != nil {
		return nil, err
	}
	return GetPopularMovies200JSONResponse{Items: items, Page: page, TotalPages: totalPages}, nil
}

func (h *Handlers) GetPopularSeries(_ context.Context, request GetPopularSeriesRequestObject) (GetPopularSeriesResponseObject, error) {
	page := 1
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	items, totalPages, err := h.fetchDiscover(func(c *tmdb.Client) ([]DiscoverItem, int, error) {
		results, tp, err := c.PopularTV(page)
		if err != nil {
			return nil, 0, err
		}
		out := make([]DiscoverItem, len(results))
		for i, r := range results {
			out[i] = toDiscoverItem(r.ID, r.Name, r.FirstAirDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeSeries)
		}
		return out, tp, nil
	})
	if err != nil {
		return nil, err
	}
	return GetPopularSeries200JSONResponse{Items: items, Page: page, TotalPages: totalPages}, nil
}

const tmdbPosterW342 = "https://image.tmdb.org/t/p/w342"

// fetchDiscover handles the common discover pattern: get a TMDB client from the
// matching service, call the fetch function, return empty slice on missing key or API error.
func (h *Handlers) fetchDiscover(fetch func(*tmdb.Client) ([]DiscoverItem, int, error)) ([]DiscoverItem, int, error) {
	client := h.matchSvc.TMDBClient()
	if client == nil {
		return []DiscoverItem{}, 0, nil
	}
	items, totalPages, err := fetch(client)
	if err != nil {
		slog.Warn("discover fetch failed", "error", err)
		return []DiscoverItem{}, 0, nil
	}
	return items, totalPages, nil
}

// toDiscoverItem builds a DiscoverItem from common TMDB result fields.
func toDiscoverItem(id int, title, date, overview, posterPath string, voteAvg float64, mediaType DiscoverItemMediaType) DiscoverItem {
	d := DiscoverItem{
		Source:     DiscoverItemSourceTmdb,
		ExternalId: id,
		Title:      title,
		MediaType:  mediaType,
	}
	if overview != "" {
		d.Overview = &overview
	}
	if len(date) >= 4 {
		d.Year = dateutil.ParseYear(date)
	}
	if posterPath != "" {
		u := tmdbPosterW342 + posterPath
		d.PosterUrl = &u
	}
	if voteAvg > 0 {
		rating := float32(math.Round(voteAvg*10) / 10)
		d.Rating = &rating
	}
	return d
}

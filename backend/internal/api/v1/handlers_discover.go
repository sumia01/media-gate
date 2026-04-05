package apiv1

import (
	"context"
	"log/slog"
	"math"

	"github.com/sumia01/media-gate/internal/dateutil"
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
	items, err := h.fetchDiscover(func(c *tmdb.Client) ([]DiscoverItem, error) {
		results, err := c.TrendingAll("week")
		if err != nil {
			return nil, err
		}
		out := make([]DiscoverItem, len(results))
		for i, r := range results {
			if r.MediaType == "movie" {
				out[i] = toDiscoverItem(r.ID, r.Title, r.ReleaseDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeMovie)
			} else {
				out[i] = toDiscoverItem(r.ID, r.Name, r.FirstAirDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeSeries)
			}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return GetTrending200JSONResponse{Items: items}, nil
}

func (h *Handlers) GetPopularMovies(_ context.Context, _ GetPopularMoviesRequestObject) (GetPopularMoviesResponseObject, error) {
	items, err := h.fetchDiscover(func(c *tmdb.Client) ([]DiscoverItem, error) {
		results, err := c.PopularMovies()
		if err != nil {
			return nil, err
		}
		out := make([]DiscoverItem, len(results))
		for i, r := range results {
			out[i] = toDiscoverItem(r.ID, r.Title, r.ReleaseDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeMovie)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return GetPopularMovies200JSONResponse{Items: items}, nil
}

func (h *Handlers) GetPopularSeries(_ context.Context, _ GetPopularSeriesRequestObject) (GetPopularSeriesResponseObject, error) {
	items, err := h.fetchDiscover(func(c *tmdb.Client) ([]DiscoverItem, error) {
		results, err := c.PopularTV()
		if err != nil {
			return nil, err
		}
		out := make([]DiscoverItem, len(results))
		for i, r := range results {
			out[i] = toDiscoverItem(r.ID, r.Name, r.FirstAirDate, r.Overview, r.PosterPath, r.VoteAverage, DiscoverItemMediaTypeSeries)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return GetPopularSeries200JSONResponse{Items: items}, nil
}

const tmdbPosterW342 = "https://image.tmdb.org/t/p/w342"

// fetchDiscover handles the common discover pattern: get TMDB API key, create client,
// call the fetch function, return empty slice on missing key or API error.
func (h *Handlers) fetchDiscover(fetch func(*tmdb.Client) ([]DiscoverItem, error)) ([]DiscoverItem, error) {
	apiKey, err := h.settings.Get(settings.KeyTMDBApiKey)
	if err != nil || apiKey == "" {
		return []DiscoverItem{}, nil
	}
	client := tmdb.NewClient(apiKey)
	items, err := fetch(client)
	if err != nil {
		slog.Warn("discover fetch failed", "error", err)
		return []DiscoverItem{}, nil
	}
	return items, nil
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

package matching

import (
	"github.com/sumia01/media-gate/internal/dateutil"
	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
	"github.com/sumia01/media-gate/internal/store"
)

// Metadata mappings preserve identity, timestamps and fields the provider does
// not assign. They return a value without changing either input or doing I/O.
func tmdbMovieMetadata(meta store.MediaMetadata, details *tmdb.MovieDetails) store.MediaMetadata {
	meta.Title = details.Title
	meta.Overview = details.Overview
	meta.PosterPath = details.PosterPath
	meta.Status = details.Status
	meta.ImdbID = details.ImdbID
	if details.Runtime > 0 {
		rt := details.Runtime
		meta.Runtime = &rt
	}
	if y := dateutil.ParseYear(details.ReleaseDate); y != nil {
		meta.Year = y
	}
	meta.ReleaseDate = details.ReleaseDate
	meta.Genres = genresToJSON(details.Genres)
	meta.Credits = tmdbCreditsToJSON(details.Credits)
	meta.TrailerURL = tmdb.BestTrailerURL(details.Videos)
	meta.ContentRatings = contentRatingsToJSON(tmdb.MovieCertifications(details.ReleaseDates))
	return meta
}

func tmdbTVMetadata(meta store.MediaMetadata, details *tmdb.TVDetails) store.MediaMetadata {
	meta.Title = details.Name
	meta.Overview = details.Overview
	meta.PosterPath = details.PosterPath
	meta.Status = details.Status
	if details.ExternalIds != nil {
		meta.ImdbID = details.ExternalIds.ImdbID
	}
	if details.NumberOfSeasons > 0 {
		ns := details.NumberOfSeasons
		meta.Seasons = &ns
	}
	if y := dateutil.ParseYear(details.FirstAirDate); y != nil {
		meta.Year = y
	}
	meta.ReleaseDate = details.FirstAirDate
	meta.Genres = genresToJSON(details.Genres)
	meta.Credits = tmdbCreditsToJSON(details.Credits)
	meta.TrailerURL = tmdb.BestTrailerURL(details.Videos)
	meta.ContentRatings = contentRatingsToJSON(tmdb.TVCertifications(details.ContentRatings))
	return meta
}

func tvdbSeriesMetadata(meta store.MediaMetadata, details *tvdb.SeriesDetails) store.MediaMetadata {
	meta.Title = details.Name
	meta.Overview = details.Overview
	meta.PosterPath = details.Image
	meta.Status = details.Status.Name
	meta.ImdbID = details.ImdbID()
	if len(details.Seasons) > 0 {
		ns := details.MaxSeasonNumber()
		if ns > 0 {
			meta.Seasons = &ns
		}
	}
	if y := dateutil.ParseYear(details.FirstAired); y != nil {
		meta.Year = y
	}
	meta.ReleaseDate = details.FirstAired
	meta.Credits = tvdbCharactersToJSON(details.Characters)
	meta.ContentRatings = tvdbContentRatingsToJSON(details.ContentRatings)
	return meta
}

func tmdbEpisodeData(entries []tmdb.TVEpisode) []episodeData {
	episodes := make([]episodeData, len(entries))
	for i, ep := range entries {
		episodes[i] = episodeData{
			seasonNumber:  ep.SeasonNumber,
			episodeNumber: ep.EpisodeNumber,
			title:         ep.Name,
			overview:      ep.Overview,
			airDate:       ep.AirDate,
			runtime:       ep.Runtime,
		}
	}
	return episodes
}

func tvdbEpisodeData(entries []tvdb.EpisodeEntry) []episodeData {
	episodes := make([]episodeData, len(entries))
	for i, ep := range entries {
		episodes[i] = episodeData{
			seasonNumber:  ep.SeasonNumber,
			episodeNumber: ep.Number,
			title:         ep.Name,
			overview:      ep.Overview,
			airDate:       ep.Aired,
			runtime:       ep.Runtime,
		}
	}
	return episodes
}

func tmdbMovieCandidate(r tmdb.MovieResult) Candidate {
	c := Candidate{
		Source:     "tmdb",
		ExternalID: r.ID,
		Title:      r.Title,
		Overview:   r.Overview,
	}
	if r.PosterPath != "" {
		c.PosterURL = tmdbPosterBase + r.PosterPath
	}
	if y := dateutil.ParseYear(r.ReleaseDate); y != nil {
		c.Year = y
	}
	return c
}

func tmdbTVCandidate(r tmdb.TVResult) Candidate {
	c := Candidate{
		Source:     "tmdb",
		ExternalID: r.ID,
		Title:      r.Name,
		Overview:   r.Overview,
	}
	if r.PosterPath != "" {
		c.PosterURL = tmdbPosterBase + r.PosterPath
	}
	if y := dateutil.ParseYear(r.FirstAirDate); y != nil {
		c.Year = y
	}
	return c
}

func tvdbSeriesCandidate(r tvdb.SeriesResult) Candidate {
	c := Candidate{
		Source:     "tvdb",
		ExternalID: r.ID(),
		Title:      r.Name,
		Overview:   r.Overview,
		PosterURL:  r.ImageURL,
	}
	if y := dateutil.ParseYear(r.FirstAirDate); y != nil {
		c.Year = y
	}
	return c
}

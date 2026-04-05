package indexer

import (
	"encoding/json"

	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
)

// FilterByProfile filters torrent results using the given profile criteria.
// This is the single source of truth for profile-based filtering, used by
// both the monitor auto-grab worker and the test-search API endpoint.
func FilterByProfile(results []TorrentResult, resolutions, sources, excludeTags []string) []TorrentResult {
	var filtered []TorrentResult
	for _, r := range results {
		if len(excludeTags) > 0 && fileparse.ContainsExcludedTag(r.Title, excludeTags) {
			continue
		}
		res := fileparse.ParseResolution(r.Title)
		src := fileparse.ParseSource(r.Title)
		if fileparse.MatchesProfile(res, src, resolutions, sources) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FilterByMediaProfile unmarshals profile criteria from a MediaProfile and applies FilterByProfile.
func FilterByMediaProfile(results []TorrentResult, profile *store.MediaProfile) []TorrentResult {
	var resolutions, sources, excludeTags []string
	_ = json.Unmarshal([]byte(profile.Resolutions), &resolutions)
	if profile.Sources != "" {
		_ = json.Unmarshal([]byte(profile.Sources), &sources)
	}
	if profile.ExcludeTags != "" {
		_ = json.Unmarshal([]byte(profile.ExcludeTags), &excludeTags)
	}
	return FilterByProfile(results, resolutions, sources, excludeTags)
}

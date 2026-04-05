package indexer

import "github.com/sumia01/media-gate/internal/fileparse"

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

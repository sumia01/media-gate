package indexer

import (
	"encoding/json"
	"sort"

	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
)

// FilterByProfile filters torrent results using the given profile criteria.
// This is the single source of truth for profile-based filtering, used by
// both the monitor auto-grab worker and the test-search API endpoint.
//
// Languages are matched against tokens parsed from the release title.
// languageMode controls the logic: "and" requires all languages present,
// "or" (default) requires at least one.
func FilterByProfile(results []TorrentResult, resolutions, sources, languages, excludeTags []string, languageMode string) []TorrentResult {
	var filtered []TorrentResult
	for _, r := range results {
		if len(excludeTags) > 0 && fileparse.ContainsExcludedTag(r.Title, excludeTags) {
			continue
		}
		res := fileparse.ParseResolution(r.Title)
		src := fileparse.ParseSource(r.Title)
		if !fileparse.MatchesProfile(res, src, resolutions, sources) {
			continue
		}
		if len(languages) > 0 {
			detected := fileparse.ParseLanguages(r.Title)
			if !fileparse.MatchesLanguages(detected, languages, languageMode) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// RankResults re-sorts filtered results by combined priority: resolution > language > source.
// Each dimension uses the profile's ordered list as priority (lower index = higher priority).
// Within the same combined score, the original order (seeders) is preserved (stable sort).
// Language ranking only applies in OR mode.
func RankResults(results []TorrentResult, resolutions, sources, languages []string, languageMode string) []TorrentResult {
	hasRes := len(resolutions) > 1
	hasSrc := len(sources) > 1
	hasLang := len(languages) > 0 && languageMode != "and"

	if !hasRes && !hasSrc && !hasLang {
		return results
	}

	sort.SliceStable(results, func(i, j int) bool {
		// Resolution priority (highest weight)
		if hasRes {
			iRes := fileparse.PriorityScore(fileparse.ParseResolution(results[i].Title), resolutions)
			jRes := fileparse.PriorityScore(fileparse.ParseResolution(results[j].Title), resolutions)
			if iRes != jRes {
				return iRes < jRes
			}
		}

		// Language priority (medium weight)
		if hasLang {
			iLangs := fileparse.ParseLanguages(results[i].Title)
			jLangs := fileparse.ParseLanguages(results[j].Title)
			iLScore := fileparse.LanguageScore(iLangs, languages)
			jLScore := fileparse.LanguageScore(jLangs, languages)
			if iLScore != jLScore {
				return iLScore < jLScore
			}
		}

		// Source priority (lowest weight)
		if hasSrc {
			iSrc := fileparse.PriorityScore(fileparse.ParseSource(results[i].Title), sources)
			jSrc := fileparse.PriorityScore(fileparse.ParseSource(results[j].Title), sources)
			if iSrc != jSrc {
				return iSrc < jSrc
			}
		}

		return false // preserve original order (seeders)
	})
	return results
}

// FilterByMediaProfile unmarshals profile criteria from a MediaProfile and applies FilterByProfile.
// Optional globalExcludeTags are merged with the profile's own exclude tags.
// In OR language mode, results are ranked by language priority after filtering.
func FilterByMediaProfile(results []TorrentResult, profile *store.MediaProfile, globalExcludeTags ...string) []TorrentResult {
	var resolutions, sources, languages, excludeTags []string
	_ = json.Unmarshal([]byte(profile.Resolutions), &resolutions)
	if profile.Languages != "" {
		_ = json.Unmarshal([]byte(profile.Languages), &languages)
	}
	if profile.Sources != "" {
		_ = json.Unmarshal([]byte(profile.Sources), &sources)
	}
	if profile.ExcludeTags != "" {
		_ = json.Unmarshal([]byte(profile.ExcludeTags), &excludeTags)
	}
	excludeTags = append(excludeTags, globalExcludeTags...)

	mode := profile.LanguageMode
	if mode == "" {
		mode = "or"
	}

	filtered := FilterByProfile(results, resolutions, sources, languages, excludeTags, mode)
	return RankResults(filtered, resolutions, sources, languages, mode)
}

// MatchesMediaProfile checks if a single torrent result matches the given media profile.
// Returns true if the result would pass through FilterByMediaProfile.
// Optional globalExcludeTags are merged with the profile's own exclude tags.
func MatchesMediaProfile(result *TorrentResult, profile *store.MediaProfile, globalExcludeTags ...string) bool {
	var resolutions, sources, languages, excludeTags []string
	_ = json.Unmarshal([]byte(profile.Resolutions), &resolutions)
	if profile.Languages != "" {
		_ = json.Unmarshal([]byte(profile.Languages), &languages)
	}
	if profile.Sources != "" {
		_ = json.Unmarshal([]byte(profile.Sources), &sources)
	}
	if profile.ExcludeTags != "" {
		_ = json.Unmarshal([]byte(profile.ExcludeTags), &excludeTags)
	}
	excludeTags = append(excludeTags, globalExcludeTags...)

	mode := profile.LanguageMode
	if mode == "" {
		mode = "or"
	}

	if len(excludeTags) > 0 && fileparse.ContainsExcludedTag(result.Title, excludeTags) {
		return false
	}
	res := fileparse.ParseResolution(result.Title)
	src := fileparse.ParseSource(result.Title)
	if !fileparse.MatchesProfile(res, src, resolutions, sources) {
		return false
	}
	if len(languages) > 0 {
		detected := fileparse.ParseLanguages(result.Title)
		if !fileparse.MatchesLanguages(detected, languages, mode) {
			return false
		}
	}
	return true
}

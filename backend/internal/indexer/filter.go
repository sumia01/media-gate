package indexer

import (
	"encoding/json"
	"sort"
	"strings"

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
	excludeTagsLow := fileparse.LowercaseTags(excludeTags)
	filtered := make([]TorrentResult, 0, len(results))
	for _, r := range results {
		if len(excludeTagsLow) > 0 && fileparse.ContainsExcludedTagLower(r.Title, excludeTagsLow) {
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

// rankScore holds pre-computed priority scores for a single torrent result.
type rankScore struct {
	idx  int // original index into results
	res  int
	lang int
	src  int
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

	// Pre-compute scores once per result instead of re-parsing on every comparison.
	scores := make([]rankScore, len(results))
	for i := range results {
		scores[i].idx = i
		if hasRes {
			scores[i].res = fileparse.PriorityScore(fileparse.ParseResolution(results[i].Title), resolutions)
		}
		if hasLang {
			langs := fileparse.ParseLanguages(results[i].Title)
			scores[i].lang = fileparse.LanguageScore(langs, languages)
		}
		if hasSrc {
			scores[i].src = fileparse.PriorityScore(fileparse.ParseSource(results[i].Title), sources)
		}
	}

	sort.SliceStable(scores, func(i, j int) bool {
		// Resolution priority (highest weight)
		if hasRes && scores[i].res != scores[j].res {
			return scores[i].res < scores[j].res
		}
		// Language priority (medium weight)
		if hasLang && scores[i].lang != scores[j].lang {
			return scores[i].lang < scores[j].lang
		}
		// Source priority (lowest weight)
		if hasSrc && scores[i].src != scores[j].src {
			return scores[i].src < scores[j].src
		}
		return false // preserve original order (seeders)
	})

	ranked := make([]TorrentResult, len(results))
	for i, s := range scores {
		ranked[i] = results[s.idx]
	}
	return ranked
}

// ProfileCriteria holds pre-parsed profile criteria to avoid repeated JSON
// unmarshaling when checking multiple results against the same profile.
type ProfileCriteria struct {
	Resolutions     []string
	Sources         []string
	Languages       []string
	ExcludeTags     []string
	excludeTagsLow []string // pre-lowercased for ContainsExcludedTagLower
	LanguageMode    string
}

// ParseProfileCriteria unmarshals a MediaProfile's JSON fields once.
// Optional globalExcludeTags are merged with the profile's own exclude tags.
func ParseProfileCriteria(profile *store.MediaProfile, globalExcludeTags ...string) ProfileCriteria {
	var c ProfileCriteria
	_ = json.Unmarshal([]byte(profile.Resolutions), &c.Resolutions)
	if profile.Languages != "" {
		_ = json.Unmarshal([]byte(profile.Languages), &c.Languages)
	}
	if profile.Sources != "" {
		_ = json.Unmarshal([]byte(profile.Sources), &c.Sources)
	}
	if profile.ExcludeTags != "" {
		_ = json.Unmarshal([]byte(profile.ExcludeTags), &c.ExcludeTags)
	}
	c.ExcludeTags = append(c.ExcludeTags, globalExcludeTags...)
	c.excludeTagsLow = fileparse.LowercaseTags(c.ExcludeTags)
	c.LanguageMode = profile.LanguageMode
	if c.LanguageMode == "" {
		c.LanguageMode = "or"
	}
	return c
}

// FilterByMediaProfile unmarshals profile criteria from a MediaProfile and applies FilterByProfile.
// Optional globalExcludeTags are merged with the profile's own exclude tags.
// In OR language mode, results are ranked by language priority after filtering.
func FilterByMediaProfile(results []TorrentResult, profile *store.MediaProfile, globalExcludeTags ...string) []TorrentResult {
	c := ParseProfileCriteria(profile, globalExcludeTags...)
	filtered := FilterByProfile(results, c.Resolutions, c.Sources, c.Languages, c.ExcludeTags, c.LanguageMode)
	return RankResults(filtered, c.Resolutions, c.Sources, c.Languages, c.LanguageMode)
}

// PreferReleases stably moves results whose title contains any of the preferred
// keywords to the front of the list, so the auto-grab picker (which takes the
// first matching result) selects them first. Matching is case-insensitive
// substring. preferred is a comma-separated keyword list (e.g. "ETHEL, FLUX");
// an empty string is a no-op. This is a soft preference: when nothing matches,
// the original (already profile-ranked) order is returned unchanged.
func PreferReleases(results []TorrentResult, preferred string) []TorrentResult {
	keywords := parsePreferredKeywords(preferred)
	if len(keywords) == 0 || len(results) < 2 {
		return results
	}
	preferredResults := make([]TorrentResult, 0, len(results))
	rest := make([]TorrentResult, 0, len(results))
	for _, r := range results {
		if fileparse.ContainsExcludedTagLower(r.Title, keywords) {
			preferredResults = append(preferredResults, r)
		} else {
			rest = append(rest, r)
		}
	}
	if len(preferredResults) == 0 {
		return results
	}
	return append(preferredResults, rest...)
}

// parsePreferredKeywords splits a comma-separated preferred-release string into
// lowercased, trimmed, non-empty keywords.
func parsePreferredKeywords(preferred string) []string {
	if strings.TrimSpace(preferred) == "" {
		return nil
	}
	parts := strings.Split(preferred, ",")
	keywords := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			keywords = append(keywords, p)
		}
	}
	return keywords
}

// MatchesMediaProfile checks if a single torrent result matches the given media profile.
// Returns true if the result would pass through FilterByMediaProfile.
// Optional globalExcludeTags are merged with the profile's own exclude tags.
func MatchesMediaProfile(result *TorrentResult, profile *store.MediaProfile, globalExcludeTags ...string) bool {
	c := ParseProfileCriteria(profile, globalExcludeTags...)
	return MatchesCriteria(result, &c)
}

// MatchesCriteria checks if a single torrent result matches pre-parsed profile criteria.
// Use this in loops to avoid repeated JSON unmarshaling of the same profile.
func MatchesCriteria(result *TorrentResult, c *ProfileCriteria) bool {
	if len(c.excludeTagsLow) > 0 && fileparse.ContainsExcludedTagLower(result.Title, c.excludeTagsLow) {
		return false
	}
	res := fileparse.ParseResolution(result.Title)
	src := fileparse.ParseSource(result.Title)
	if !fileparse.MatchesProfile(res, src, c.Resolutions, c.Sources) {
		return false
	}
	if len(c.Languages) > 0 {
		detected := fileparse.ParseLanguages(result.Title)
		if !fileparse.MatchesLanguages(detected, c.Languages, c.LanguageMode) {
			return false
		}
	}
	return true
}

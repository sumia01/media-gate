package fileparse

import "strings"

// MatchesProfile returns true if the given resolution and source match the profile criteria.
// If profileResolutions is empty, any resolution is accepted.
// If profileSources is empty, any source is accepted.
func MatchesProfile(resolution, source string, profileResolutions, profileSources []string) bool {
	if len(profileResolutions) > 0 {
		if resolution == "" || !contains(profileResolutions, resolution) {
			return false
		}
	}
	if len(profileSources) > 0 {
		if source == "" || !contains(profileSources, source) {
			return false
		}
	}
	return true
}

// ContainsExcludedTag returns true if the title contains any of the excluded tags
// (case-insensitive substring match).
func ContainsExcludedTag(title string, excludeTags []string) bool {
	lower := strings.ToLower(title)
	for _, tag := range excludeTags {
		if tag != "" && strings.Contains(lower, strings.ToLower(tag)) {
			return true
		}
	}
	return false
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

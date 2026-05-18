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

// MatchesLanguages checks whether the detected languages from a release title satisfy
// the profile's language requirements.
//
// Mode "and": ALL profileLanguages must be present in detectedLanguages.
// Mode "or":  At least ONE profileLanguage must be present in detectedLanguages.
//
// If profileLanguages is empty, any release is accepted (no language filter).
// The special language "multi" in detectedLanguages satisfies any requirement.
//
// English fallback: when a release has no detectable language tag and "eng" is
// in the profile, the release is treated as English. Many indexers omit the
// language tag for English-only releases (the implicit default). The fallback
// applies in BOTH "or" and "and" modes — an untagged release with profile ["eng"]
// in AND mode passes, but profile ["hun","eng"] in AND mode still fails because
// "hun" is missing.
func MatchesLanguages(detectedLanguages, profileLanguages []string, mode string) bool {
	if len(profileLanguages) == 0 {
		return true
	}

	// "multi" in the release satisfies any language requirement.
	for _, d := range detectedLanguages {
		if d == "multi" {
			return true
		}
	}

	detected := make(map[string]bool, len(detectedLanguages)+1)
	for _, d := range detectedLanguages {
		detected[d] = true
	}

	// Untagged release + English in profile -> treat as English.
	if len(detected) == 0 && contains(profileLanguages, "eng") {
		detected["eng"] = true
	}

	switch mode {
	case "and":
		for _, lang := range profileLanguages {
			if !detected[lang] {
				return false
			}
		}
		return true
	default: // "or" or empty
		for _, lang := range profileLanguages {
			if detected[lang] {
				return true
			}
		}
		return false
	}
}

// LanguageScore returns a priority score for the release based on language match.
// Lower score = better match. Used for ranking in OR mode where language order
// represents preference priority.
//
// Returns 0 if no profile languages are configured.
// Returns the 1-based index of the first matching profile language found in the release.
// Returns len(profileLanguages)+1 if no match (worst score).
//
// English fallback: when no language tag is detected and "eng" is in the profile,
// the release is treated as English and scored at eng's 1-based position.
// Mirrors MatchesLanguages so filtering and ranking stay consistent. Note this
// means untagged releases out-rank explicitly-tagged non-English releases when
// "eng" is the first profile entry — intentional, since the fallback's whole
// point is to surface untagged English releases.
func LanguageScore(detectedLanguages, profileLanguages []string) int {
	if len(profileLanguages) == 0 {
		return 0
	}

	// "multi" gets score 1 (best possible)
	for _, d := range detectedLanguages {
		if d == "multi" {
			return 1
		}
	}

	detected := make(map[string]bool, len(detectedLanguages)+1)
	for _, d := range detectedLanguages {
		detected[d] = true
	}

	// Untagged release + English in profile -> treat as English.
	if len(detected) == 0 && contains(profileLanguages, "eng") {
		detected["eng"] = true
	}

	for i, lang := range profileLanguages {
		if detected[lang] {
			return i + 1
		}
	}
	return len(profileLanguages) + 1
}

// PriorityScore returns the 1-based index of val in the priority list.
// Lower score = higher priority. Returns 0 if the priority list is empty.
// Returns len(priorities)+1 if val is not in the list (worst score).
func PriorityScore(val string, priorities []string) int {
	if len(priorities) == 0 {
		return 0
	}
	for i, p := range priorities {
		if p == val {
			return i + 1
		}
	}
	return len(priorities) + 1
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

// LowercaseTags returns a new slice with all tags lowercased.
// Use with ContainsExcludedTagLower to pre-compute tag lowercasing once.
func LowercaseTags(tags []string) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = strings.ToLower(t)
	}
	return out
}

// ContainsExcludedTagLower returns true if the title contains any of the pre-lowercased tags.
// Tags must already be lowercased (via LowercaseTags).
func ContainsExcludedTagLower(title string, loweredTags []string) bool {
	lower := strings.ToLower(title)
	for _, tag := range loweredTags {
		if tag != "" && strings.Contains(lower, tag) {
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

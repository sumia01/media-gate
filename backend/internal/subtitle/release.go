package subtitle

import (
	"regexp"
	"strings"
)

var tokenSeparators = regexp.MustCompile(`[.\-_ ()\[\]]+`)

// TokenizeRelease splits a release name into normalized lowercase tokens.
func TokenizeRelease(name string) []string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}
	parts := tokenSeparators.Split(name, -1)
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

// ReleaseMatch compares two release names by their token overlap.
// exact is true when the token sets are identical.
// partial is true when >60% of tokens overlap.
func ReleaseMatch(subtitleRelease, downloadTitle string) (exact bool, partial bool) {
	subTokens := TokenizeRelease(subtitleRelease)
	dlTokens := TokenizeRelease(downloadTitle)

	if len(subTokens) == 0 || len(dlTokens) == 0 {
		return false, false
	}

	dlSet := make(map[string]struct{}, len(dlTokens))
	for _, t := range dlTokens {
		dlSet[t] = struct{}{}
	}

	subSet := make(map[string]struct{}, len(subTokens))
	for _, t := range subTokens {
		subSet[t] = struct{}{}
	}

	// Count overlap
	matches := 0
	for _, t := range subTokens {
		if _, ok := dlSet[t]; ok {
			matches++
		}
	}

	// Check exact: same token sets
	if len(subSet) == len(dlSet) && matches == len(subSet) {
		return true, true
	}

	// Partial: >60% overlap relative to the smaller set
	smaller := len(subSet)
	if len(dlSet) < smaller {
		smaller = len(dlSet)
	}
	if smaller > 0 && float64(matches)/float64(smaller) > 0.6 {
		return false, true
	}

	return false, false
}

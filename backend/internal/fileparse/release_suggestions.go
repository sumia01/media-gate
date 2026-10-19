package fileparse

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// ReleaseSuggestionScore ranks saved-name suggestions, not download eligibility.
// Lower scores win: exact, prefix, substring, ordered subsequence, then a single
// insertion/deletion/substitution or adjacent transposition in the typed prefix.
// Typo tolerance starts at three characters to avoid noisy short queries.
// A negative score means no match. Work is linear in the two bounded strings.
func ReleaseSuggestionScore(keyword, query string) int {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	query = strings.ToLower(strings.TrimSpace(query))
	if keyword == "" || query == "" {
		return -1
	}
	if keyword == query {
		return 0
	}
	name, typed := []rune(keyword), []rune(query)
	if strings.HasPrefix(keyword, query) {
		return 1000 + len(name) - len(typed)
	}
	if index := strings.Index(keyword, query); index >= 0 {
		return 2000 + utf8.RuneCountInString(keyword[:index]) + len(name) - len(typed)
	}
	matched := 0
	for i, r := range name {
		if r != typed[matched] {
			continue
		}
		matched++
		if matched == len(typed) {
			extra := len(name) - len(typed)
			skipped := i + 1 - len(typed)
			return 3000 + extra + skipped
		}
	}
	if len(typed) < 3 {
		return -1
	}
	for length := max(1, len(typed)-1); length <= min(len(name), len(typed)+1); length++ {
		if oneReleaseEdit(typed, name[:length]) {
			return 4000 + max(len(name)-len(typed), len(typed)-len(name))
		}
	}
	return -1
}

func oneReleaseEdit(a, b []rune) bool {
	i := 0
	for i < min(len(a), len(b)) && a[i] == b[i] {
		i++
	}
	if i == min(len(a), len(b)) {
		return true // Prefix lengths differ by at most one at the call site.
	}
	if len(a) < len(b) {
		return slices.Equal(a[i:], b[i+1:])
	}
	if len(a) > len(b) {
		return slices.Equal(a[i+1:], b[i:])
	}
	if slices.Equal(a[i+1:], b[i+1:]) {
		return true
	}
	return i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i] && slices.Equal(a[i+2:], b[i+2:])
}

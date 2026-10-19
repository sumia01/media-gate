package fileparse

import "strings"

// ParseReleaseKeywords canonicalizes a comma-separated keyword list, preserving
// first-use order while dropping empty and case-insensitive duplicate entries.
func ParseReleaseKeywords(value string) []string {
	var keywords []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		keyword := strings.ToLower(strings.TrimSpace(part))
		if keyword != "" && !seen[keyword] {
			seen[keyword] = true
			keywords = append(keywords, keyword)
		}
	}
	return keywords
}

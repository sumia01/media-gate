// Package dateutil provides shared date helpers for parsing media metadata.
//
// Why hand-rolled parsing instead of strconv/time?
// ParseYear is called for every media item during TMDB/TVDB matching.
// Manual digit parsing avoids string slicing allocations and strconv/time
// overhead when we only need the first 4 characters as a year.
//
// Why *int return?
// Year is an optional field — TMDB/TVDB responses may contain empty strings,
// "TBA", or other non-date values. A nil pointer is the idiomatic Go way to
// represent "no value" without a separate (value, ok) return.
//
// Why the 1900–2099 range?
// Input strings aren't guaranteed to be dates — they could be IDs, codes, or
// garbage that happens to start with 4 digits. A tight domain-specific range
// rejects values like "5000" or "3412" that are valid integers but not
// plausible release years for film/TV media.
package dateutil

// ParseYear extracts a 4-digit year from the start of a date string (e.g. "2024-01-15").
// Returns nil if the string is too short, not numeric, or outside 1900–2099.
func ParseYear(dateStr string) *int {
	if len(dateStr) < 4 {
		return nil
	}
	y := 0
	for _, c := range dateStr[:4] {
		if c < '0' || c > '9' {
			return nil
		}
		y = y*10 + int(c-'0')
	}
	if y < 1900 || y > 2099 {
		return nil
	}
	return &y
}

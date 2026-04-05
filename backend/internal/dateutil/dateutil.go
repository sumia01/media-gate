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

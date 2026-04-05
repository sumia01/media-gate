package dateutil

import "testing"

func TestParseYear(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want *int
	}{
		{"full date", "2024-01-15", new(2024)},
		{"year only", "2024", new(2024)},
		{"lower bound", "1900-06-01", new(1900)},
		{"upper bound", "2099-12-31", new(2099)},
		{"below range", "1899-01-01", nil},
		{"above range", "2100-01-01", nil},
		{"empty", "", nil},
		{"short", "202", nil},
		{"non-numeric", "TBA", nil},
		{"mixed chars", "20X4-01-01", nil},
		{"all zeros", "0000-01-01", nil},
		{"numeric but not year", "5000", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseYear(tt.in)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseYear(%q) = %d, want nil", tt.in, *got)
				}
			} else {
				if got == nil {
					t.Errorf("ParseYear(%q) = nil, want %d", tt.in, *tt.want)
				} else if *got != *tt.want {
					t.Errorf("ParseYear(%q) = %d, want %d", tt.in, *got, *tt.want)
				}
			}
		})
	}
}

func TestParseYear_ZeroAllocs(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		_ = ParseYear("2024-01-15")
	})
	// The only allocation is the returned *int pointer.
	// If this ever exceeds 1, it means string slicing or other
	// heap work crept in.
	if allocs > 1 {
		t.Errorf("ParseYear allocations = %v, want <= 1", allocs)
	}
}

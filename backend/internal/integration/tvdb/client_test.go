package tvdb

import "testing"

func TestMaxSeasonNumber(t *testing.T) {
	tests := []struct {
		name    string
		seasons []SeasonEntry
		want    int
	}{
		{
			name:    "empty",
			seasons: nil,
			want:    0,
		},
		{
			name:    "single season",
			seasons: []SeasonEntry{{ID: 1, Number: 1}},
			want:    1,
		},
		{
			name: "with specials (season 0)",
			seasons: []SeasonEntry{
				{ID: 1, Number: 0},
				{ID: 2, Number: 1},
				{ID: 3, Number: 2},
			},
			want: 2,
		},
		{
			name: "specials inflate len but not max",
			seasons: []SeasonEntry{
				{ID: 1, Number: 0},
				{ID: 2, Number: 1},
			},
			want: 1,
		},
		{
			name: "unordered entries",
			seasons: []SeasonEntry{
				{ID: 3, Number: 3},
				{ID: 1, Number: 1},
				{ID: 2, Number: 2},
				{ID: 0, Number: 0},
			},
			want: 3,
		},
		{
			name: "duplicate season numbers from different orderings",
			seasons: []SeasonEntry{
				{ID: 1, Number: 0},
				{ID: 2, Number: 1},
				{ID: 3, Number: 1}, // e.g. DVD order duplicate
				{ID: 4, Number: 2},
			},
			want: 2,
		},
		{
			name:    "only specials",
			seasons: []SeasonEntry{{ID: 1, Number: 0}},
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &SeriesDetails{Seasons: tt.seasons}
			if got := d.MaxSeasonNumber(); got != tt.want {
				t.Errorf("MaxSeasonNumber() = %d, want %d", got, tt.want)
			}
		})
	}
}

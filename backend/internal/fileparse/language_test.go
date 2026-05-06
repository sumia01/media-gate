package fileparse

import (
	"reflect"
	"testing"
)

func TestParseLanguages(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  []string
	}{
		{
			"dual audio HUN+ENG",
			"Movie.2024.1080p.HUN.ENG.BluRay.x264",
			[]string{"hun", "eng"},
		},
		{
			"full language name",
			"Movie.2024.Hungarian.1080p.BluRay",
			[]string{"hun"},
		},
		{
			"multi tag",
			"Movie.2024.MULTI.1080p.BluRay",
			[]string{"multi"},
		},
		{
			"no language",
			"Movie.2024.1080p.BluRay.x264-GROUP",
			nil,
		},
		{
			"case insensitive",
			"Movie.2024.hun.ENG.1080p",
			[]string{"hun", "eng"},
		},
		{
			"hyphen separator",
			"Movie-2024-HUN-ENG-1080p-BluRay",
			[]string{"hun", "eng"},
		},
		{
			"underscore separator",
			"Movie_2024_HUN_ENG_1080p",
			[]string{"hun", "eng"},
		},
		{
			"space separator",
			"Movie 2024 HUN ENG 1080p",
			[]string{"hun", "eng"},
		},
		{
			"mixed separators",
			"Movie.2024 HUN-ENG_1080p.BluRay",
			[]string{"hun", "eng"},
		},
		{
			"multiple languages",
			"Movie.2024.1080p.HUN.ENG.GER.BluRay",
			[]string{"hun", "eng", "ger"},
		},
		{
			"magyar keyword",
			"Film.2024.Magyar.1080p.WEB-DL",
			[]string{"hun"},
		},
		{
			"deutsch keyword",
			"Film.2024.Deutsch.1080p.BluRay",
			[]string{"ger"},
		},
		{
			"dedup repeated language",
			"Movie.HUN.Something.HUN.1080p",
			[]string{"hun"},
		},
		{
			"language at start",
			"HUN.Movie.2024.1080p",
			[]string{"hun"},
		},
		{
			"language at end",
			"Movie.2024.1080p.BluRay.HUN",
			[]string{"hun"},
		},
		{
			"three-letter ISO codes",
			"Movie.2024.1080p.FRA.SPA.BluRay",
			[]string{"fre", "spa"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLanguages(tt.title)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseLanguages(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

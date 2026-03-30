package importer

import (
	"strings"
	"testing"
)

func TestBuildReleaseFolderName(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "normal release title",
			title: "Movie.Name.2024.1080p.BluRay.x264-GROUP",
			want:  "Movie.Name.2024.1080p.BluRay.x264-GROUP",
		},
		{
			name:  "title with illegal chars",
			title: `Movie: The "Sequel" 2024`,
			want:  "Movie The Sequel 2024",
		},
		{
			name:  "empty title",
			title: "",
			want:  "release",
		},
		{
			name:  "only illegal chars",
			title: `<>:"/\|?*`,
			want:  "release",
		},
		{
			name:  "very long title truncated",
			title: strings.Repeat("A", 250),
			want:  strings.Repeat("A", 200),
		},
		{
			name:  "dots trimmed after truncation",
			title: strings.Repeat("A", 198) + "..",
			want:  strings.Repeat("A", 198),
		},
		{
			name:  "preserves dots and dashes in normal title",
			title: "Show.S01E01.1080p.WEB-DL.DDP5.1.x264-GROUP",
			want:  "Show.S01E01.1080p.WEB-DL.DDP5.1.x264-GROUP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildReleaseFolderName(tt.title)
			if got != tt.want {
				t.Errorf("buildReleaseFolderName(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

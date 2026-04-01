package importer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
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
			got := BuildReleaseFolderName(tt.title)
			if got != tt.want {
				t.Errorf("BuildReleaseFolderName(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestSafePath_RejectsTraversal(t *testing.T) {
	base := "/mnt/downloads/complete"

	tests := []struct {
		name string
		path string
	}{
		{"parent traversal", "/mnt/downloads/complete/../etc/passwd"},
		{"deep traversal", "/mnt/downloads/complete/sub/../../etc"},
		{"absolute escape", "/etc/passwd"},
		{"prefix trick", "/mnt/downloads/completeevil/file.mkv"},
		{"root", "/"},
		{"relative dot-dot", ".."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := safePath(base, tt.path)
			if err == nil {
				t.Errorf("safePath(%q, %q) = nil, want error", base, tt.path)
			}
		})
	}
}

func TestSafePath_AcceptsValidPaths(t *testing.T) {
	base := "/mnt/downloads/complete"

	tests := []struct {
		name string
		path string
	}{
		{"exact base", "/mnt/downloads/complete"},
		{"subdirectory", "/mnt/downloads/complete/torrent/video.mkv"},
		{"nested subdir", "/mnt/downloads/complete/a/b/c.srt"},
		{"subs folder", "/mnt/downloads/complete/Subs/english.srt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := safePath(base, tt.path)
			if err != nil {
				t.Errorf("safePath(%q, %q) = %v, want nil", base, tt.path, err)
			}
		})
	}
}

func TestSafePath_TorrentFileNameTraversal(t *testing.T) {
	savePath := "/mnt/downloads/complete"
	releaseDir := "/mnt/media/Movies/Movie (2024)/Movie.2024.1080p-GROUP"

	// Simulate malicious torrent file names
	maliciousNames := []string{
		"../../etc/cron.d/evil",
		"../../../tmp/pwned.sh",
		"subdir/../../../etc/shadow",
	}

	for _, name := range maliciousNames {
		t.Run("src_"+name, func(t *testing.T) {
			srcPath := filepath.Join(savePath, name)
			if err := safePath(savePath, srcPath); err == nil {
				t.Errorf("safePath(savePath, %q) should reject traversal in torrent filename %q", srcPath, name)
			}
		})
		t.Run("dst_"+name, func(t *testing.T) {
			dstPath := filepath.Join(releaseDir, name)
			if err := safePath(releaseDir, dstPath); err == nil {
				t.Errorf("safePath(releaseDir, %q) should reject traversal in torrent filename %q", dstPath, name)
			}
		})
	}
}

func TestBuildTargetDir_AdversarialTitle(t *testing.T) {
	lib := &store.Library{Path: "/mnt/media/Movies"}
	year := 2024
	season := 1

	tests := []struct {
		name  string
		title string
	}{
		{"slash in title", "Movie/../../../etc"},
		{"backslash in title", "Movie\\..\\..\\etc"},
		{"colon traversal", "Movie:../../etc"},
		{"just dots", ".."},
		{"empty title", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &store.MediaItem{Title: tt.title, Year: &year, MediaType: "movie"}
			dir := BuildTargetDir(lib, item, nil, nil)
			if !strings.HasPrefix(dir, lib.Path+"/") {
				t.Errorf("BuildTargetDir with title %q = %q, escapes library path %q", tt.title, dir, lib.Path)
			}

			// Also test with series + season
			item.MediaType = "series"
			dir = BuildTargetDir(lib, item, nil, &season)
			if !strings.HasPrefix(dir, lib.Path+"/") {
				t.Errorf("BuildTargetDir (series) with title %q = %q, escapes library path %q", tt.title, dir, lib.Path)
			}
		})
	}
}

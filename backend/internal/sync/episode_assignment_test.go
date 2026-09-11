package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

func TestResyncRetainsKnownEpisodeOnlyWhenParsingIsAmbiguous(t *testing.T) {
	for _, tc := range []struct {
		name         string
		fileName     string
		folderSeason string
		wantSeason   int
		wantEpisode  int
	}{
		{"special", "Show.S02.Special.White.Christmas.mkv", "Season 02", 2, 4},
		{"no parsed season", "video.mkv", "release", 2, 4},
		{"parsed episode corrects old value", "Show.S02E03.mkv", "Season 02", 2, 3},
		{"different season does not inherit old episode", "video.mkv", "Season 03", 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := sqlite.New(filepath.Join(t.TempDir(), "resync.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			lib := &store.Library{Name: "Shows", MediaType: "series", Path: t.TempDir()}
			if err := st.CreateLibrary(lib); err != nil {
				t.Fatal(err)
			}
			item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: "series"}
			if err := st.CreateMediaItem(item); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(lib.Path, "Show", tc.folderSeason, tc.fileName)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("video"), 0644); err != nil {
				t.Fatal(err)
			}
			file := &store.MediaFile{MediaItemID: item.ID, Path: path, FileName: tc.fileName, SeasonNumber: ptr(2), EpisodeNumber: ptr(4)}
			if err := st.CreateMediaFile(file); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, _, _, err := NewService(st).ResyncMediaItem(item.ID); err != nil {
					t.Fatal(err)
				}
				fresh, err := st.GetMediaFile(file.ID)
				if err != nil {
					t.Fatal(err)
				}
				if fresh.SeasonNumber == nil || *fresh.SeasonNumber != tc.wantSeason {
					t.Fatalf("season=%v, want %d", fresh.SeasonNumber, tc.wantSeason)
				}
				if tc.wantEpisode == 0 {
					if fresh.EpisodeNumber != nil {
						t.Fatal("copied episode across season boundary")
					}
				} else if fresh.EpisodeNumber == nil || *fresh.EpisodeNumber != tc.wantEpisode {
					t.Fatalf("episode=%v, want %d", fresh.EpisodeNumber, tc.wantEpisode)
				}
			}
		})
	}
}

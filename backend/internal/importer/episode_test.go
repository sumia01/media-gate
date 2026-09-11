package importer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
	"github.com/sumia01/media-gate/internal/store"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

type unavailableImportEpisodeStore struct{ store.Store }

func (s *unavailableImportEpisodeStore) ListEpisodesByMediaItem(uint) ([]store.Episode, error) {
	return nil, errors.New("temporary database read failure")
}

func TestImportEpisodeLookupFailureRetriesBeforeWritingFiles(t *testing.T) {
	st, dl := newImportFixture(t, false)
	ep, err := st.GetEpisodeByNumber(dl.MediaItemID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	dl.EpisodeID, dl.Title = &ep.ID, "Show.S01.Special.1080p"
	if err := st.UpdateDownload(dl); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dl.SavePath, "video.mkv"), []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeQBit{files: []qbittorrent.TorrentFile{{Name: "video.mkv", Size: 5}}}
	server := fake.server(t)
	defer server.Close()
	svc := &Service{store: &unavailableImportEpisodeStore{Store: st}, syncSvc: mediasync.NewService(st), bus: eventbus.New(8)}
	svc.importOne(qbittorrent.NewClient(server.URL, "u", "p", server.Client()), dl)
	if dl.Status != "downloaded" || dl.RetryCount != 1 || dl.NextRetryAt == nil || dl.LinkedToLibrary || fake.deleteCalled {
		t.Fatalf("target lookup failure was not retryable: %+v", dl)
	}
	files, err := st.ListMediaFilesByMediaItem(dl.MediaItemID)
	if err != nil || len(files) != 0 {
		t.Fatalf("files created without target lookup: %+v %v", files, err)
	}
}

func TestImportExplicitEpisodeFallback(t *testing.T) {
	const special = "Black.Mirror.S02.Special.White.Christmas.1080p.WEB-DL.DD2.0.H.264-CS"
	for _, tc := range []struct {
		name          string
		title         string
		files         []string
		noTarget      bool
		foreignTarget bool
		wantEpisode   int // zero means unnumbered
	}{
		{name: "named special", title: special, files: []string{special + ".mkv"}, wantEpisode: 4},
		{name: "unnumbered file", title: special, files: []string{"video.mkv"}, wantEpisode: 4},
		{name: "numbered torrent folder with unnumbered file", title: special, files: []string{"Release.S02E04/video.mkv"}, wantEpisode: 4},
		{name: "samples and companions excluded", title: special, files: []string{"video.mkv", "video.sample.mkv", "release.nfo"}, wantEpisode: 4},
		{name: "explicit filename wins", title: special, files: []string{"Show.S02E01.mkv"}, wantEpisode: 1},
		{name: "different season", title: special, files: []string{"Show.S03.Special.mkv"}},
		{name: "multiple videos", title: special, files: []string{"part-one.mkv", "part-two.mkv"}},
		{name: "range in one file", title: "Show.S02E04-E05.1080p", files: []string{"video.mkv"}},
		{name: "pack in one file", title: "Show.S02.Complete.1080p", files: []string{"video.mkv"}},
		{name: "no explicit target", title: special, files: []string{special + ".mkv"}, noTarget: true},
		{name: "target belongs to another item", title: special, files: []string{special + ".mkv"}, foreignTarget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, dl := newImportFixture(t, false)
			if err := st.DeleteEpisodesByMediaItem(dl.MediaItemID); err != nil {
				t.Fatal(err)
			}
			episode := &store.Episode{MediaItemID: dl.MediaItemID, SeasonNumber: 2, EpisodeNumber: 4, Title: "White Christmas", AirDate: "2014-12-16"}
			if err := st.CreateEpisode(episode); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: dl.MediaItemID, SeasonNumber: 2, Monitored: true}); err != nil {
				t.Fatal(err)
			}
			dl.Title, dl.EpisodeID, dl.SeasonNumber = tc.title, &episode.ID, &episode.SeasonNumber
			if tc.noTarget {
				dl.EpisodeID = nil
			}
			if tc.foreignTarget {
				item, err := st.GetMediaItem(dl.MediaItemID)
				if err != nil {
					t.Fatal(err)
				}
				other := &store.MediaItem{LibraryID: item.LibraryID, Title: "Other", MediaType: "series"}
				if err := st.CreateMediaItem(other); err != nil {
					t.Fatal(err)
				}
				otherEpisode := &store.Episode{MediaItemID: other.ID, SeasonNumber: 2, EpisodeNumber: 4}
				if err := st.CreateEpisode(otherEpisode); err != nil {
					t.Fatal(err)
				}
				dl.EpisodeID = &otherEpisode.ID
			}
			if err := st.UpdateDownload(dl); err != nil {
				t.Fatal(err)
			}
			var torrentFiles []qbittorrent.TorrentFile
			for _, name := range tc.files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dl.SavePath, name)), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dl.SavePath, name), []byte("video"), 0644); err != nil {
					t.Fatal(err)
				}
				torrentFiles = append(torrentFiles, qbittorrent.TorrentFile{Name: name, Size: 5})
			}
			fake := &fakeQBit{files: torrentFiles}
			server := fake.server(t)
			defer server.Close()
			syncSvc := mediasync.NewService(st)
			svc := &Service{store: st, syncSvc: syncSvc, bus: eventbus.New(16)}
			svc.importOne(qbittorrent.NewClient(server.URL, "u", "p", server.Client()), dl)
			if dl.Status != "completed" || !dl.LinkedToLibrary {
				t.Fatalf("import did not complete: %+v", dl)
			}
			assertFiles := func() {
				t.Helper()
				files, err := st.ListMediaFilesByMediaItem(dl.MediaItemID)
				if err != nil || len(files) == 0 {
					t.Fatalf("files=%+v err=%v", files, err)
				}
				for _, file := range files {
					if tc.wantEpisode == 0 {
						if file.EpisodeNumber != nil {
							t.Errorf("invented episode for %s: %d", file.FileName, *file.EpisodeNumber)
						}
					} else if file.EpisodeNumber == nil || *file.EpisodeNumber != tc.wantEpisode || file.SeasonNumber == nil || *file.SeasonNumber != 2 {
						t.Errorf("wrong file target: %+v, want S02E%02d", file, tc.wantEpisode)
					}
				}
			}
			assertFiles() // includes the importer's automatic resync
			if _, _, _, err := syncSvc.ResyncMediaItem(dl.MediaItemID); err != nil {
				t.Fatal(err)
			}
			assertFiles() // also survives later explicit resync
			if tc.wantEpisode == 4 {
				item, err := st.GetMediaItem(dl.MediaItemID)
				if err != nil || item.Status != "available" {
					t.Fatalf("item=%+v err=%v", item, err)
				}
			}
		})
	}
}

package devharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/indexer/cardigann"
	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
)

func TestQBitTorrentWorkflow(t *testing.T) {
	root := t.TempDir()
	handler, err := NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := qbittorrent.NewClient(server.URL, "harness", "harness", server.Client())
	if err := client.TestConnection(); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if err := client.EnsureCategory("media-gate-dl"); err != nil {
		t.Fatalf("ensure category: %v", err)
	}

	torrentData := getBytes(t, server.Client(), server.URL+"/tracker/download/harness.torrent")
	wantHash, err := qbittorrent.InfoHash(torrentData)
	if err != nil {
		t.Fatalf("fixture info hash: %v", err)
	}
	savePath := filepath.Join(root, "downloads")
	hash, err := client.AddTorrentFile("harness.torrent", torrentData, qbittorrent.AddTorrentOptions{
		SavePath: savePath,
		Category: "media-gate-dl",
	})
	if err != nil {
		t.Fatalf("add torrent: %v", err)
	}
	if hash != wantHash {
		t.Fatalf("hash = %q, want %q", hash, wantHash)
	}

	info, err := client.GetTorrent(hash)
	if err != nil {
		t.Fatalf("get torrent: %v", err)
	}
	if info.Name != fixtureName || info.State != "downloading" || info.Progress != 0 || info.SavePath != savePath {
		t.Fatalf("unexpected initial torrent info: %+v", info)
	}
	files, err := client.GetTorrentFiles(hash)
	if err != nil {
		t.Fatalf("get torrent files: %v", err)
	}
	if len(files) != 1 || files[0].Name != fixtureName || files[0].Progress != 0 {
		t.Fatalf("unexpected initial files: %+v", files)
	}

	post(t, server.Client(), server.URL+"/_harness/complete")
	info, err = client.GetTorrent(hash)
	if err != nil {
		t.Fatalf("get completed torrent: %v", err)
	}
	if info.State != "uploading" || info.Progress != 1 || info.CompletionOn <= 0 {
		t.Fatalf("unexpected completed torrent info: %+v", info)
	}
	files, err = client.GetTorrentFiles(hash)
	if err != nil {
		t.Fatalf("get completed torrent files: %v", err)
	}
	if len(files) != 1 || files[0].Progress != 1 {
		t.Fatalf("unexpected completed files: %+v", files)
	}
	payload := filepath.Join(savePath, fixtureName)
	content, err := os.ReadFile(payload)
	if err != nil {
		t.Fatalf("read completed payload: %v", err)
	}
	if string(content) != string(fixtureContent) {
		t.Fatalf("payload = %q, want %q", content, fixtureContent)
	}

	var state State
	decodeURL(t, server.Client(), server.URL+"/_harness/state", &state)
	if len(state.Torrents) != 1 || state.TrackerDownloadCount != 1 {
		t.Fatalf("unexpected state: %+v", state)
	}

	if err := client.DeleteTorrent(hash, true); err != nil {
		t.Fatalf("delete torrent: %v", err)
	}
	if _, err := client.GetTorrent(hash); !errors.Is(err, qbittorrent.ErrTorrentNotFound) {
		t.Fatalf("get deleted torrent error = %v, want ErrTorrentNotFound", err)
	}
	if _, err := os.Stat(payload); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted payload stat error = %v, want not exist", err)
	}
}

func TestQBitRejectsInvalidCredentials(t *testing.T) {
	handler, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := qbittorrent.NewClient(server.URL, "wrong", "credentials", server.Client())
	if err := client.TestConnection(); err == nil {
		t.Fatal("TestConnection() unexpectedly accepted invalid credentials")
	}
}

func TestTrackerDefinitionSearchAndDownload(t *testing.T) {
	handler, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	definitionData := getBytes(t, server.Client(), server.URL+"/_harness/indexer.yml")
	definition, err := cardigann.ParseDefinition(definitionData)
	if err != nil {
		t.Fatalf("parse generated definition: %v\n%s", err, definitionData)
	}
	if len(definition.Links) != 1 || definition.Links[0] != server.URL+"/" {
		t.Fatalf("definition links = %v, want %q", definition.Links, server.URL+"/")
	}
	engine, err := cardigann.NewEngine(definition, nil)
	if err != nil {
		t.Fatalf("new Cardigann engine: %v", err)
	}
	results, err := engine.Search(context.Background(), cardigann.SearchQuery{Type: "movie-search", Q: "Harness Movie"})
	if err != nil {
		t.Fatalf("search tracker: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search returned %d results, want 1", len(results))
	}
	missing, err := engine.Search(context.Background(), cardigann.SearchQuery{Type: "movie-search", Q: "Unrelated"})
	if err != nil {
		t.Fatalf("search unrelated tracker query: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("unrelated search returned %d results, want 0", len(missing))
	}
	result := results[0]
	if result.Title != "Harness.Movie.2026.1080p.WEB-DL" || result.Category != "Movies/HD" || result.Seeders != 42 {
		t.Fatalf("unexpected search result: %+v", result)
	}
	if result.Download != server.URL+"/tracker/download/harness.torrent" {
		t.Fatalf("download URL = %q", result.Download)
	}

	torrentData, err := engine.FetchDownload(context.Background(), result.Download)
	if err != nil {
		t.Fatalf("fetch tracker download: %v", err)
	}
	if _, err := qbittorrent.InfoHash(torrentData); err != nil {
		t.Fatalf("downloaded torrent is invalid: %v", err)
	}
	var state State
	decodeURL(t, server.Client(), server.URL+"/_harness/state", &state)
	if state.TrackerDownloadCount != 1 {
		t.Fatalf("tracker download count = %d, want 1", state.TrackerDownloadCount)
	}
}

func TestRejectsEscapingSaveAndTorrentPaths(t *testing.T) {
	root := t.TempDir()
	handler, err := NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := qbittorrent.NewClient(server.URL, "harness", "harness", server.Client())

	outside := filepath.Join(filepath.Dir(root), "outside")
	if _, err := client.AddTorrentFile("harness.torrent", trackerTorrent, qbittorrent.AddTorrentOptions{SavePath: outside}); err == nil {
		t.Fatal("add with escaping save path unexpectedly succeeded")
	}
	malicious := singleFileTorrent("../escape.mkv", 1)
	if _, err := client.AddTorrentFile("malicious.torrent", malicious, qbittorrent.AddTorrentOptions{SavePath: root}); err == nil {
		t.Fatal("add with escaping torrent path unexpectedly succeeded")
	}
	post(t, server.Client(), server.URL+"/_harness/complete")
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.mkv")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside payload stat error = %v, want not exist", err)
	}
	var state State
	decodeURL(t, server.Client(), server.URL+"/_harness/state", &state)
	if len(state.Torrents) != 0 {
		t.Fatalf("rejected torrents were retained: %+v", state.Torrents)
	}
}

func TestCompleteRechecksSymlinkContainment(t *testing.T) {
	root := t.TempDir()
	handler, err := NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := qbittorrent.NewClient(server.URL, "harness", "harness", server.Client())

	savePath := filepath.Join(root, "later-symlink")
	hash, err := client.AddTorrentFile("harness.torrent", trackerTorrent, qbittorrent.AddTorrentOptions{SavePath: savePath})
	if err != nil {
		t.Fatalf("add torrent: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, savePath); err != nil {
		t.Fatalf("create save path symlink: %v", err)
	}

	resp, err := server.Client().Post(server.URL+"/_harness/complete", "text/plain", nil)
	if err != nil {
		t.Fatalf("complete request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("complete status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	if _, err := os.Stat(filepath.Join(outside, fixtureName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside payload stat error = %v, want not exist", err)
	}
	info, err := client.GetTorrent(hash)
	if err != nil {
		t.Fatalf("get torrent: %v", err)
	}
	if info.State != "downloading" || info.Progress != 0 {
		t.Fatalf("unsafe completion changed state: %+v", info)
	}
}

func TestDeleteDoesNotRemovePreexistingPayload(t *testing.T) {
	root := t.TempDir()
	handler, err := NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := qbittorrent.NewClient(server.URL, "harness", "harness", server.Client())

	payload := filepath.Join(root, fixtureName)
	const original = "preexisting fixture"
	if err := os.WriteFile(payload, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := client.AddTorrentFile("harness.torrent", trackerTorrent, qbittorrent.AddTorrentOptions{SavePath: root})
	if err != nil {
		t.Fatalf("add torrent: %v", err)
	}
	if err := client.DeleteTorrent(hash, true); err != nil {
		t.Fatalf("delete torrent: %v", err)
	}
	content, err := os.ReadFile(payload)
	if err != nil {
		t.Fatalf("read preexisting payload: %v", err)
	}
	if string(content) != original {
		t.Fatalf("preexisting payload = %q, want %q", content, original)
	}
}

func TestRetainedRootCannotBeRedirected(t *testing.T) {
	root := t.TempDir()
	handler, err := NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := qbittorrent.NewClient(server.URL, "harness", "harness", server.Client())

	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddTorrentFile("harness.torrent", trackerTorrent, qbittorrent.AddTorrentOptions{SavePath: root}); err != nil {
		t.Fatalf("add torrent: %v", err)
	}
	post(t, server.Client(), server.URL+"/_harness/complete")
	if _, err := os.Stat(filepath.Join(moved, fixtureName)); err != nil {
		t.Fatalf("payload was not written through retained root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fixtureName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement root payload stat error = %v, want not exist", err)
	}
}

func singleFileTorrent(name string, length int) []byte {
	return []byte(fmt.Sprintf("d4:infod6:lengthi%de4:name%d:%s12:piece lengthi16384e6:pieces20:00000000000000000000ee", length, len(name), name))
}

func getBytes(t *testing.T, client *http.Client, target string) []byte {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s returned %d: %s", target, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	return data
}

func decodeURL(t *testing.T, client *http.Client, target string, value any) {
	t.Helper()
	data := getBytes(t, client, target)
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %s: %v", target, err)
	}
}

func post(t *testing.T, client *http.Client, target string) {
	t.Helper()
	resp, err := client.Post(target, "text/plain", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s returned %d: %s", target, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

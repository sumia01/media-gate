package devharness

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sumia01/media-gate/internal/integration/qbittorrent"
)

const (
	maxTorrentSize  = 10 << 20
	maxTorrents     = 20
	maxTorrentFiles = 200
	fakeSID         = "media-gate-harness"
	fixtureName     = "Harness.Movie.2026.1080p.WEB-DL.mkv"
)

var (
	fixtureContent = []byte("media-gate harness fixture\n")
	trackerTorrent = buildTrackerTorrent()
)

// State is the machine-readable snapshot returned by /_harness/state.
type State struct {
	Torrents             []TorrentState `json:"torrents"`
	TrackerDownloadCount int            `json:"tracker_download_count"`
}

// TorrentState combines qBittorrent's torrent fields with its file listing.
type TorrentState struct {
	qbittorrent.TorrentInfo
	Files []qbittorrent.TorrentFile `json:"files"`
}

type torrent struct {
	info  qbittorrent.TorrentInfo
	files []qbittorrent.TorrentFile
}

// Server is a stateful fake qBittorrent and tracker HTTP handler.
type Server struct {
	downloadRoot string
	root         *os.Root
	mux          *http.ServeMux

	mu                   sync.RWMutex
	torrents             map[string]*torrent
	payloads             map[string]struct{}
	trackerDownloadCount int
}

// NewServer creates a fake integration server restricted to downloadRoot.
func NewServer(downloadRoot string) (*Server, error) {
	if strings.TrimSpace(downloadRoot) == "" {
		return nil, errors.New("download root is required")
	}

	absRoot, err := filepath.Abs(downloadRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving download root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return nil, fmt.Errorf("creating download root: %w", err)
	}
	root, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving download root symlinks: %w", err)
	}

	root = filepath.Clean(root)
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening download root: %w", err)
	}

	s := &Server{
		downloadRoot: root,
		root:         rootHandle,
		mux:          http.NewServeMux(),
		torrents:     make(map[string]*torrent),
		payloads:     make(map[string]struct{}),
	}
	s.routes()
	return s, nil
}

// Close releases the filesystem root retained by the fake server.
func (s *Server) Close() error {
	return s.root.Close()
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/v2/auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /api/v2/app/version", s.authenticated(s.handleVersion))
	s.mux.HandleFunc("POST /api/v2/torrents/createCategory", s.authenticated(s.handleCreateCategory))
	s.mux.HandleFunc("POST /api/v2/torrents/add", s.authenticated(s.handleAdd))
	s.mux.HandleFunc("GET /api/v2/torrents/info", s.authenticated(s.handleInfo))
	s.mux.HandleFunc("GET /api/v2/torrents/files", s.authenticated(s.handleFiles))
	s.mux.HandleFunc("POST /api/v2/torrents/delete", s.authenticated(s.handleDelete))

	s.mux.HandleFunc("GET /tracker/search", s.handleTrackerSearch)
	s.mux.HandleFunc("GET /tracker/download/harness.torrent", s.handleTrackerDownload)

	s.mux.HandleFunc("GET /_harness/health", s.handleHealth)
	s.mux.HandleFunc("GET /_harness/state", s.handleState)
	s.mux.HandleFunc("GET /_harness/indexer.yml", s.handleIndexerDefinition)
	s.mux.HandleFunc("POST /_harness/complete", s.handleComplete)
	s.mux.HandleFunc("POST /_harness/error", s.handleError)
	s.mux.HandleFunc("POST /_harness/reset", s.handleReset)
}

func (s *Server) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("SID")
		if err != nil || cookie.Value != fakeSID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.FormValue("username") != "harness" || r.FormValue("password") != "harness" {
		writeText(w, "Fails.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "SID", Value: fakeSID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeText(w, "Ok.")
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeText(w, "v4.6.0")
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, _ *http.Request) {
	writeText(w, "Ok.")
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTorrentSize+(1<<20))
	if err := r.ParseMultipartForm(maxTorrentSize); err != nil {
		http.Error(w, "invalid multipart torrent upload", http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	file, _, err := r.FormFile("torrents")
	if err != nil {
		http.Error(w, "torrent file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxTorrentSize+1))
	if err != nil || len(data) > maxTorrentSize {
		http.Error(w, "invalid torrent file", http.StatusBadRequest)
		return
	}
	hash, err := qbittorrent.InfoHash(data)
	if err != nil {
		http.Error(w, "invalid torrent file", http.StatusBadRequest)
		return
	}
	name, files, err := parseTorrent(data)
	if err != nil {
		http.Error(w, "invalid torrent metadata", http.StatusBadRequest)
		return
	}

	savePath, err := s.multipartSavePath(r)
	if err != nil {
		http.Error(w, "unsafe torrent save path", http.StatusBadRequest)
		return
	}
	for _, file := range files {
		if _, err := s.payloadPath(savePath, file.Name); err != nil {
			http.Error(w, "unsafe torrent file path", http.StatusBadRequest)
			return
		}
	}

	var size int64
	for _, file := range files {
		size += file.Size
	}
	info := qbittorrent.TorrentInfo{
		Hash:     strings.ToLower(hash),
		Name:     name,
		State:    "downloading",
		Progress: 0,
		SavePath: savePath,
		Size:     size,
	}

	s.mu.Lock()
	if _, exists := s.torrents[info.Hash]; !exists && len(s.torrents) >= maxTorrents {
		s.mu.Unlock()
		http.Error(w, "too many harness torrents", http.StatusTooManyRequests)
		return
	}
	if _, exists := s.torrents[info.Hash]; !exists {
		s.torrents[info.Hash] = &torrent{info: info, files: files}
	}
	s.mu.Unlock()
	writeText(w, "Ok.")
}

func (s *Server) multipartSavePath(r *http.Request) (string, error) {
	var selected string
	for _, field := range []string{"savepath", "downloadPath"} {
		raw := r.FormValue(field)
		if raw == "" {
			continue
		}
		resolved, err := s.resolveSavePath(raw)
		if err != nil {
			return "", err
		}
		if selected != "" && selected != resolved {
			return "", errors.New("conflicting save paths")
		}
		selected = resolved
	}
	if selected == "" {
		return s.downloadRoot, nil
	}
	return selected, nil
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	wanted := make(map[string]bool)
	if hashes := r.URL.Query().Get("hashes"); hashes != "" {
		for _, hash := range strings.Split(hashes, "|") {
			wanted[strings.ToLower(hash)] = true
		}
	}

	s.mu.RLock()
	infos := make([]qbittorrent.TorrentInfo, 0, len(s.torrents))
	for hash, torrent := range s.torrents {
		if len(wanted) == 0 || wanted[hash] {
			infos = append(infos, torrent.info)
		}
	}
	s.mu.RUnlock()
	sort.Slice(infos, func(i, j int) bool { return infos[i].Hash < infos[j].Hash })
	writeJSON(w, infos)
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.URL.Query().Get("hash"))
	s.mu.RLock()
	torrent, ok := s.torrents[hash]
	var files []qbittorrent.TorrentFile
	if ok {
		files = append(files, torrent.files...)
	}
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	writeJSON(w, files)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid delete request", http.StatusBadRequest)
		return
	}
	hashes := r.FormValue("hashes")
	if hashes == "" {
		http.Error(w, "torrent hash is required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	selected := make(map[string]bool)
	if hashes == "all" {
		for hash := range s.torrents {
			selected[hash] = true
		}
	} else {
		for _, hash := range strings.Split(hashes, "|") {
			selected[strings.ToLower(hash)] = true
		}
	}

	if r.FormValue("deleteFiles") == "true" {
		for hash := range selected {
			torrent, ok := s.torrents[hash]
			if !ok {
				continue
			}
			for _, file := range torrent.files {
				payload, err := s.payloadPath(torrent.info.SavePath, file.Name)
				if err != nil {
					http.Error(w, "unsafe torrent payload path", http.StatusInternalServerError)
					return
				}
				if err := s.removePayload(payload, torrent.info.SavePath); err != nil {
					http.Error(w, "failed to remove torrent payload", http.StatusInternalServerError)
					return
				}
			}
		}
	}
	for hash := range selected {
		delete(s.torrents, hash)
	}
	writeText(w, "Ok.")
}

func (s *Server) handleTrackerSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !strings.Contains(strings.ToLower(r.URL.Query().Get("q")), "harness") {
		_, _ = io.WriteString(w, `<!doctype html><html><body><table id="results"><tbody></tbody></table></body></html>`)
		return
	}
	fmt.Fprintf(w, `<!doctype html>
<html><body><table id="results"><tbody>
<tr class="torrent">
<td><a class="title" href="/tracker/search">Harness.Movie.2026.1080p.WEB-DL</a></td>
<td><a class="download" href="/tracker/download/harness.torrent">Download</a></td>
<td class="category">movies</td><td class="size">%d B</td>
<td class="seeders">42</td><td class="leechers">1</td><td class="date">1767225600</td>
</tr>
</tbody></table></body></html>`, len(fixtureContent))
}

func (s *Server) handleTrackerDownload(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.trackerDownloadCount++
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", `attachment; filename="harness.torrent"`)
	_, _ = w.Write(trackerTorrent)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	state := s.snapshotLocked()
	s.mu.RUnlock()
	writeJSON(w, state)
}

func (s *Server) handleIndexerDefinition(w http.ResponseWriter, r *http.Request) {
	if r.Host == "" {
		http.Error(w, "request host is required", http.StatusBadRequest)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := (&url.URL{Scheme: scheme, Host: r.Host, Path: "/"}).String()

	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	fmt.Fprintf(w, `id: media-gate-harness
name: Media Gate Harness
description: Local deterministic tracker for the disposable Media Gate harness
language: en-US
type: public
encoding: UTF-8
links:
  - %s
caps:
  categorymappings:
    - {id: movies, cat: Movies/HD, desc: Harness Movies}
  modes:
    search: [q]
    movie-search: [q]
  allowrawsearch: true
settings: []
login:
  method: get
download:
  selectors:
    - selector: a.download
      attribute: href
search:
  paths:
    - path: /tracker/search
  inputs:
    q: "{{ .Keywords }}"
  rows:
    selector: tr.torrent
  fields:
    category:
      selector: .category
    title:
      selector: .title
    details:
      selector: .title
      attribute: href
    download:
      selector: .download
      attribute: href
    size:
      selector: .size
    seeders:
      selector: .seeders
    leechers:
      selector: .leechers
    date:
      selector: .date
    downloadvolumefactor:
      text: "0"
    uploadvolumefactor:
      text: "1"
`, strconv.Quote(baseURL))
}

func (s *Server) handleComplete(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, torrent := range s.torrents {
		for _, file := range torrent.files {
			payload, err := s.payloadPath(torrent.info.SavePath, file.Name)
			if err != nil {
				http.Error(w, "unsafe torrent payload path", http.StatusInternalServerError)
				return
			}
			if err := s.writePayload(payload); err != nil {
				http.Error(w, "failed to create torrent payload", http.StatusInternalServerError)
				return
			}
		}
	}

	completedAt := time.Now().Unix()
	for _, torrent := range s.torrents {
		torrent.info.State = "uploading"
		torrent.info.Progress = 1
		torrent.info.Ratio = 100
		torrent.info.SeedingTime = 1 << 30
		torrent.info.CompletionOn = completedAt
		for i := range torrent.files {
			torrent.files[i].Progress = 1
		}
	}
	writeJSON(w, s.snapshotLocked())
}

func (s *Server) handleError(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	for _, torrent := range s.torrents {
		torrent.info.State = "error"
	}
	state := s.snapshotLocked()
	s.mu.Unlock()
	writeJSON(w, state)
}

func (s *Server) handleReset(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	payloads := make([]string, 0, len(s.payloads))
	for payload := range s.payloads {
		payloads = append(payloads, payload)
	}
	sort.Strings(payloads)
	for _, payload := range payloads {
		if err := s.removePayload(payload, s.downloadRoot); err != nil {
			http.Error(w, "failed to remove harness payload", http.StatusInternalServerError)
			return
		}
	}
	s.torrents = make(map[string]*torrent)
	s.payloads = make(map[string]struct{})
	s.trackerDownloadCount = 0
	writeJSON(w, s.snapshotLocked())
}

func (s *Server) snapshotLocked() State {
	state := State{
		Torrents:             make([]TorrentState, 0, len(s.torrents)),
		TrackerDownloadCount: s.trackerDownloadCount,
	}
	for _, torrent := range s.torrents {
		state.Torrents = append(state.Torrents, TorrentState{
			TorrentInfo: torrent.info,
			Files:       append([]qbittorrent.TorrentFile(nil), torrent.files...),
		})
	}
	sort.Slice(state.Torrents, func(i, j int) bool { return state.Torrents[i].Hash < state.Torrents[j].Hash })
	return state
}

func (s *Server) resolveSavePath(raw string) (string, error) {
	candidate := raw
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(s.downloadRoot, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if err := containedBy(s.downloadRoot, abs); err != nil {
		return "", err
	}
	if err := rejectSymlinks(s.downloadRoot, abs); err != nil {
		return "", err
	}
	return abs, nil
}

func (s *Server) payloadPath(savePath, torrentPath string) (string, error) {
	if err := containedBy(s.downloadRoot, savePath); err != nil {
		return "", err
	}
	clean := path.Clean(torrentPath)
	if clean == "." || clean == ".." || path.IsAbs(clean) || strings.HasPrefix(clean, "../") || strings.Contains(torrentPath, "\\") {
		return "", errors.New("unsafe torrent path")
	}
	payload := filepath.Join(savePath, filepath.FromSlash(clean))
	if err := containedBy(savePath, payload); err != nil {
		return "", err
	}
	if err := containedBy(s.downloadRoot, payload); err != nil {
		return "", err
	}
	if err := rejectSymlinks(s.downloadRoot, payload); err != nil {
		return "", err
	}
	return payload, nil
}

func containedBy(root, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return fmt.Errorf("checking path containment: %w", err)
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path escapes allowed root")
	}
	return nil
}

func rejectSymlinks(root, candidate string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("allowed root is not a real directory")
	}

	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return err
	}
	if err := containedBy(root, candidate); err != nil {
		return err
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("path contains a symlink")
		}
	}
	return nil
}

func (s *Server) writePayload(payload string) error {
	rel, err := filepath.Rel(s.downloadRoot, payload)
	if err != nil {
		return err
	}
	if err := containedBy(s.downloadRoot, payload); err != nil {
		return err
	}
	if _, owned := s.payloads[payload]; owned {
		if _, err := s.root.Stat(rel); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := s.root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	file, err := s.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(fixtureContent)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = s.root.Remove(rel)
		return errors.Join(writeErr, closeErr)
	}
	s.payloads[payload] = struct{}{}
	return nil
}

func (s *Server) removePayload(payload, stopAt string) error {
	if _, owned := s.payloads[payload]; !owned {
		return nil
	}
	if err := containedBy(s.downloadRoot, payload); err != nil {
		return err
	}
	rel, err := filepath.Rel(s.downloadRoot, payload)
	if err != nil {
		return err
	}
	if err := s.root.Remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	delete(s.payloads, payload)

	for parent := filepath.Dir(payload); parent != stopAt && parent != s.downloadRoot; parent = filepath.Dir(parent) {
		if err := containedBy(s.downloadRoot, parent); err != nil {
			return err
		}
		rel, err := filepath.Rel(s.downloadRoot, parent)
		if err != nil {
			return err
		}
		if err := s.root.Remove(rel); err != nil {
			break
		}
	}
	return nil
}

func parseTorrent(data []byte) (string, []qbittorrent.TorrentFile, error) {
	pos := 0
	budget := 10_000
	decoded, err := decodeBencode(data, &pos, 0, &budget)
	if err != nil || pos != len(data) {
		return "", nil, errors.New("invalid bencode")
	}
	top, ok := decoded.(map[string]any)
	if !ok {
		return "", nil, errors.New("torrent root is not a dictionary")
	}
	info, ok := top["info"].(map[string]any)
	if !ok {
		return "", nil, errors.New("torrent info is missing")
	}
	name, err := bencodedString(info, "name.utf-8", "name")
	if err != nil || !safePathPart(name) {
		return "", nil, errors.New("torrent name is invalid")
	}

	if length, ok := info["length"].(int64); ok {
		if length < 0 {
			return "", nil, errors.New("torrent length is invalid")
		}
		if err := validatePieces(info, length); err != nil {
			return "", nil, err
		}
		return name, []qbittorrent.TorrentFile{{Name: name, Size: length, Priority: 1}}, nil
	}

	encodedFiles, ok := info["files"].([]any)
	if !ok || len(encodedFiles) == 0 || len(encodedFiles) > maxTorrentFiles {
		return "", nil, errors.New("torrent files are missing")
	}
	files := make([]qbittorrent.TorrentFile, 0, len(encodedFiles))
	var total int64
	for _, encodedFile := range encodedFiles {
		file, ok := encodedFile.(map[string]any)
		if !ok {
			return "", nil, errors.New("torrent file is invalid")
		}
		length, ok := file["length"].(int64)
		if !ok || length < 0 || length > math.MaxInt64-total {
			return "", nil, errors.New("torrent file length is invalid")
		}
		encodedPath, ok := file["path.utf-8"].([]any)
		if !ok {
			encodedPath, ok = file["path"].([]any)
		}
		if !ok || len(encodedPath) == 0 {
			return "", nil, errors.New("torrent file path is invalid")
		}
		parts := []string{name}
		for _, encodedPart := range encodedPath {
			partBytes, ok := encodedPart.([]byte)
			if !ok || !utf8.Valid(partBytes) || !safePathPart(string(partBytes)) {
				return "", nil, errors.New("torrent file path is invalid")
			}
			parts = append(parts, string(partBytes))
		}
		files = append(files, qbittorrent.TorrentFile{Name: path.Join(parts...), Size: length, Priority: 1})
		total += length
	}
	if err := validatePieces(info, total); err != nil {
		return "", nil, err
	}
	return name, files, nil
}

func validatePieces(info map[string]any, total int64) error {
	pieceLength, ok := info["piece length"].(int64)
	if !ok || pieceLength <= 0 {
		return errors.New("torrent piece length is invalid")
	}
	pieces, ok := info["pieces"].([]byte)
	if !ok || len(pieces) == 0 || len(pieces)%sha1.Size != 0 {
		return errors.New("torrent pieces are invalid")
	}
	want := (total + pieceLength - 1) / pieceLength
	if total <= 0 || int64(len(pieces)/sha1.Size) != want {
		return errors.New("torrent piece count does not match its size")
	}
	return nil
}

func safePathPart(part string) bool {
	return part != "" && part != "." && part != ".." && utf8.ValidString(part) && !strings.ContainsAny(part, "/\\\x00")
}

func bencodedString(values map[string]any, keys ...string) (string, error) {
	for _, key := range keys {
		if value, ok := values[key].([]byte); ok {
			if !utf8.Valid(value) {
				return "", errors.New("invalid UTF-8")
			}
			return string(value), nil
		}
	}
	return "", errors.New("string is missing")
}

func decodeBencode(data []byte, pos *int, depth int, budget *int) (any, error) {
	if depth > 32 || *pos >= len(data) || *budget <= 0 {
		return nil, errors.New("invalid bencode value")
	}
	*budget = *budget - 1
	switch data[*pos] {
	case 'i':
		*pos = *pos + 1
		start := *pos
		for *pos < len(data) && data[*pos] != 'e' {
			*pos = *pos + 1
		}
		if start == *pos || *pos >= len(data) {
			return nil, errors.New("invalid bencode integer")
		}
		value, err := strconv.ParseInt(string(data[start:*pos]), 10, 64)
		if err != nil {
			return nil, err
		}
		*pos = *pos + 1
		return value, nil
	case 'l':
		*pos = *pos + 1
		var values []any
		for *pos < len(data) && data[*pos] != 'e' {
			value, err := decodeBencode(data, pos, depth+1, budget)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if *pos >= len(data) {
			return nil, errors.New("unterminated bencode list")
		}
		*pos = *pos + 1
		return values, nil
	case 'd':
		*pos = *pos + 1
		values := make(map[string]any)
		for *pos < len(data) && data[*pos] != 'e' {
			if *budget <= 0 {
				return nil, errors.New("bencode node limit exceeded")
			}
			*budget = *budget - 1
			key, err := decodeBencodeBytes(data, pos)
			if err != nil {
				return nil, err
			}
			value, err := decodeBencode(data, pos, depth+1, budget)
			if err != nil {
				return nil, err
			}
			values[string(key)] = value
		}
		if *pos >= len(data) {
			return nil, errors.New("unterminated bencode dictionary")
		}
		*pos = *pos + 1
		return values, nil
	default:
		return decodeBencodeBytes(data, pos)
	}
}

func decodeBencodeBytes(data []byte, pos *int) ([]byte, error) {
	start := *pos
	for *pos < len(data) && data[*pos] >= '0' && data[*pos] <= '9' {
		*pos = *pos + 1
	}
	if start == *pos || *pos >= len(data) || data[*pos] != ':' {
		return nil, errors.New("invalid bencode string")
	}
	length, err := strconv.ParseInt(string(data[start:*pos]), 10, 64)
	if err != nil || length < 0 {
		return nil, errors.New("invalid bencode string length")
	}
	*pos = *pos + 1
	if length > int64(len(data)-*pos) {
		return nil, errors.New("truncated bencode string")
	}
	end := *pos + int(length)
	value := data[*pos:end]
	*pos = end
	return value, nil
}

func buildTrackerTorrent() []byte {
	pieceHash := sha1.Sum(fixtureContent)
	var info bytes.Buffer
	info.WriteString("d6:lengthi")
	info.WriteString(strconv.Itoa(len(fixtureContent)))
	info.WriteString("e4:name")
	writeBencodedString(&info, []byte(fixtureName))
	info.WriteString("12:piece lengthi16384e6:pieces20:")
	info.Write(pieceHash[:])
	info.WriteByte('e')

	var torrent bytes.Buffer
	torrent.WriteString("d8:announce")
	writeBencodedString(&torrent, []byte("http://tracker.invalid/announce"))
	torrent.WriteString("4:info")
	torrent.Write(info.Bytes())
	torrent.WriteByte('e')
	return torrent.Bytes()
}

func writeBencodedString(w io.Writer, value []byte) {
	_, _ = fmt.Fprintf(w, "%d:", len(value))
	_, _ = w.Write(value)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeText(w http.ResponseWriter, value string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, value)
}

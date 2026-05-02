package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/worker"
)

const defaultCheckInterval = 6 * time.Hour

// ReleaseInfo holds metadata about a GitHub release.
type ReleaseInfo struct {
	TagName     string
	PublishedAt time.Time
	AssetURL    string // GitHub API asset URL (not exposed to frontend)
	Body        string // release notes markdown
}

// Service periodically checks GitHub for new releases and can apply updates
// by replacing the running binary in-process (Linux only).
type Service struct {
	version  string
	ghToken  string
	ghRepo   string
	bus      *eventbus.Bus
	loop     *worker.Loop
	mu       sync.Mutex
	latest   *ReleaseInfo // cached latest release, nil if none newer
}

// NewService creates an updater service. Returns nil if preconditions are not
// met (not Linux, dev build, or missing GitHub credentials).
func NewService(version, ghToken, ghRepo string, settingsSvc *settings.Service, bus *eventbus.Bus) *Service {
	if runtime.GOOS != "linux" || version == "dev" || ghToken == "" || ghRepo == "" {
		return nil
	}
	svc := &Service{
		version: version,
		ghToken: ghToken,
		ghRepo:  ghRepo,
		bus:     bus,
	}
	svc.loop = worker.New(worker.Config{
		Name:            "update-check",
		DefaultInterval: defaultCheckInterval,
		IntervalKey:     settings.KeyWorkerUpdateCheckInterval,
		Settings:        settingsSvc,
		Process:         svc.processOnce,
		StartupDelay:    1 * time.Minute,
	})
	return svc
}

// Start begins the periodic update check loop.
func (s *Service) Start() { s.loop.Start() }

// Stop halts the periodic update check loop.
func (s *Service) Stop() { s.loop.Stop() }

// Loop returns the underlying worker loop for registry purposes.
func (s *Service) Loop() *worker.Loop { return s.loop }

// Latest returns the cached latest release info, or nil if no update is available.
func (s *Service) Latest() *ReleaseInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest
}

// CheckNow performs an immediate check for updates.
func (s *Service) CheckNow() (*ReleaseInfo, error) {
	rel, err := s.fetchLatestRelease()
	if err != nil {
		return nil, err
	}
	if rel == nil {
		s.mu.Lock()
		s.latest = nil
		s.mu.Unlock()
		return nil, nil
	}
	if !isNewer(rel.TagName, s.version) {
		s.mu.Lock()
		s.latest = nil
		s.mu.Unlock()
		return nil, nil
	}
	s.mu.Lock()
	s.latest = rel
	s.mu.Unlock()
	return rel, nil
}

// Apply downloads and applies the cached latest release, replacing the running binary.
func (s *Service) Apply() error {
	s.mu.Lock()
	rel := s.latest
	s.mu.Unlock()

	if rel == nil {
		return fmt.Errorf("no update available")
	}

	s.bus.Publish(eventbus.UpdateApplying, eventbus.UpdatePayload{
		CurrentVersion: s.version,
		NewVersion:     rel.TagName,
	})

	// 1. Download asset to temp file.
	tmpFile, err := s.downloadAsset(rel.AssetURL)
	if err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}
	defer os.Remove(tmpFile)

	// 2. Make executable.
	if err := os.Chmod(tmpFile, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// 3. Resolve current binary path.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving symlinks: %w", err)
	}

	// 4. Backup current binary.
	bakPath := exe + ".bak"
	if err := os.Rename(exe, bakPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// 5. Move new binary into place.
	if err := os.Rename(tmpFile, exe); err != nil {
		// Attempt to restore backup.
		_ = os.Rename(bakPath, exe)
		return fmt.Errorf("replacing binary: %w", err)
	}

	slog.Info("update applied, re-executing", "from", s.version, "to", rel.TagName)

	// 6. Re-exec the process. On Linux this replaces the current process image.
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("re-exec failed: %w", err)
	}

	// Unreachable after successful Exec.
	return nil
}

// Version returns the current build version.
func (s *Service) Version() string {
	return s.version
}

func (s *Service) processOnce() {
	rel, err := s.fetchLatestRelease()
	if err != nil {
		slog.Warn("update-check: failed to check for updates", "error", err)
		return
	}
	if rel == nil {
		return
	}
	if !isNewer(rel.TagName, s.version) {
		slog.Debug("update-check: already up to date", "current", s.version, "latest", rel.TagName)
		return
	}

	s.mu.Lock()
	alreadyNotified := s.latest != nil && s.latest.TagName == rel.TagName
	s.latest = rel
	s.mu.Unlock()

	if !alreadyNotified {
		slog.Info("update-check: new version available", "current", s.version, "latest", rel.TagName)
		s.bus.Publish(eventbus.UpdateAvailable, eventbus.UpdatePayload{
			CurrentVersion: s.version,
			NewVersion:     rel.TagName,
			ReleaseNotes:   rel.Body,
			PublishedAt:    rel.PublishedAt.Format(time.RFC3339),
		})
	}
}

// ghRelease mirrors the subset of GitHub API release JSON we need.
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"assets"`
}

func (s *Service) fetchLatestRelease() (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", s.ghRepo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+s.ghToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}

	// Find the linux-amd64 asset.
	var assetURL string
	for _, a := range rel.Assets {
		if a.Name == "media-gate-linux-amd64" {
			assetURL = a.URL
			break
		}
	}
	if assetURL == "" {
		return nil, fmt.Errorf("media-gate-linux-amd64 asset not found in release %s", rel.TagName)
	}

	return &ReleaseInfo{
		TagName:     rel.TagName,
		PublishedAt: rel.PublishedAt,
		AssetURL:    assetURL,
		Body:        rel.Body,
	}, nil
}

func (s *Service) downloadAsset(assetURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+s.ghToken)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Create temp file in the same directory as the running binary so that
	// os.Rename is same-device (avoids EXDEV with PrivateTmp=true / tmpfs).
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving executable: %w", err)
	}
	exeDir := filepath.Dir(exe)
	tmpFile, err := os.CreateTemp(exeDir, ".media-gate-update-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("writing asset: %w", err)
	}
	tmpFile.Close()

	return tmpPath, nil
}

// isNewer compares two version tags. Returns true if latest > current.
// Handles both "vX.Y.Z" and "X.Y.Z" formats.
func isNewer(latest, current string) bool {
	// Simple string comparison: if they're equal, not newer.
	if latest == current {
		return false
	}
	// Strip leading 'v' for comparison.
	l := stripV(latest)
	c := stripV(current)
	if l == c {
		return false
	}
	// Parse semver components.
	lp := parseSemver(l)
	cp := parseSemver(c)
	for i := 0; i < 3; i++ {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

func stripV(s string) string {
	if len(s) > 0 && s[0] == 'v' {
		return s[1:]
	}
	return s
}

func parseSemver(s string) [3]int {
	var parts [3]int
	var idx, partIdx int
	for i := 0; i < len(s) && partIdx < 3; i++ {
		if s[i] == '.' {
			partIdx++
			idx = 0
			continue
		}
		if s[i] >= '0' && s[i] <= '9' {
			parts[partIdx] = parts[partIdx]*10 + int(s[i]-'0')
			idx++
		}
	}
	_ = idx
	return parts
}

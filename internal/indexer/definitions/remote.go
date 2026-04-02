package definitions

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	tarballURL    = "https://api.github.com/repos/Prowlarr/Indexers/tarball/master"
	maxTarballSize = 50 * 1024 * 1024 // 50 MB safety limit
	v11Prefix     = "definitions/v11/"
)

// FetchFromGitHub downloads the Prowlarr/Indexers tarball and extracts
// all v11 YAML definitions. Returns the same map[id]rawYAML shape as LoadBuiltin.
func FetchFromGitHub() (map[string][]byte, error) {
	req, err := http.NewRequest("GET", tarballURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "media-gate/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned status %d", resp.StatusCode)
	}

	// Safety: limit how much we read.
	limited := io.LimitReader(resp.Body, maxTarballSize)

	gz, err := gzip.NewReader(limited)
	if err != nil {
		return nil, fmt.Errorf("decompressing tarball: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	defs := make(map[string][]byte)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// Tarball paths look like: Prowlarr-Indexers-<sha>/definitions/v11/foo.yml
		// Find the v11 prefix and check for .yml suffix.
		idx := strings.Index(hdr.Name, v11Prefix)
		if idx < 0 {
			continue
		}
		relPath := hdr.Name[idx+len(v11Prefix):]
		if !strings.HasSuffix(relPath, ".yml") || strings.Contains(relPath, "/") {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			slog.Warn("failed to read tarball entry", "file", hdr.Name, "error", err)
			continue
		}

		var header struct {
			ID string `yaml:"id"`
		}
		if err := yaml.Unmarshal(data, &header); err != nil {
			slog.Warn("failed to parse YAML header", "file", relPath, "error", err)
			continue
		}
		if header.ID == "" {
			slog.Warn("skipping definition without id", "file", relPath)
			continue
		}

		defs[header.ID] = data
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no definitions found in tarball")
	}

	return defs, nil
}

// LoadCached reads all .yml files from the disk cache directory.
// Returns the same map[id]rawYAML shape as LoadBuiltin.
func LoadCached(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading cache dir: %w", err)
	}

	defs := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			slog.Warn("failed to read cached definition", "file", e.Name(), "error", err)
			continue
		}

		var header struct {
			ID string `yaml:"id"`
		}
		if err := yaml.Unmarshal(data, &header); err != nil {
			slog.Warn("failed to parse cached definition header", "file", e.Name(), "error", err)
			continue
		}
		if header.ID == "" {
			continue
		}
		defs[header.ID] = data
	}

	if len(defs) == 0 {
		return nil, fmt.Errorf("no definitions in cache dir %s", dir)
	}
	return defs, nil
}

// SaveCache writes YAML definitions to the disk cache directory and
// updates the last_updated timestamp.
func SaveCache(dir string, defs map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	for id, data := range defs {
		path := filepath.Join(dir, id+".yml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}

	tsPath := filepath.Join(dir, "last_updated")
	if err := os.WriteFile(tsPath, []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
		return fmt.Errorf("writing timestamp: %w", err)
	}

	return nil
}

// IsCacheFresh returns true if the cache directory has a last_updated
// timestamp that is less than maxAge old.
func IsCacheFresh(dir string, maxAge time.Duration) bool {
	data, err := os.ReadFile(filepath.Join(dir, "last_updated"))
	if err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return time.Since(t) < maxAge
}

package matching

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func downloadPoster(httpClient *http.Client, url, destPath string) error {
	if url == "" {
		return nil
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating poster dir: %w", err)
	}

	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("downloading poster: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("poster download returned %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating poster file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, io.LimitReader(resp.Body, 10<<20)); err != nil {
		return fmt.Errorf("writing poster: %w", err)
	}

	return nil
}

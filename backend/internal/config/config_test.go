package config

import "testing"

func TestLoadHarnessOverrides(t *testing.T) {
	t.Setenv("MEDIAGATE_ENV_FILE", "")
	t.Setenv("MEDIAGATE_DATA_DIR", "/tmp/media-gate-harness")
	t.Setenv("MEDIAGATE_API_HOST", "127.0.0.1")
	t.Setenv("MEDIAGATE_BROWSER_OPEN", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Data.Dir != "/tmp/media-gate-harness" {
		t.Fatalf("Data.Dir = %q", cfg.Data.Dir)
	}
	if cfg.API.Host != "127.0.0.1" {
		t.Fatalf("API.Host = %q", cfg.API.Host)
	}
	if cfg.Browser.Open {
		t.Fatal("Browser.Open = true, want false")
	}
}

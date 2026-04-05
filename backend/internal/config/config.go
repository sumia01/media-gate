package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	API         APIConfig         `koanf:"api"`
	DB          DBConfig          `koanf:"db"`
	Log         LogConfig         `koanf:"log"`
	Library     LibraryConfig     `koanf:"library"`
	TMDB        TMDBConfig        `koanf:"tmdb"`
	TVDB        TVDBConfig        `koanf:"tvdb"`
	Secret      SecretConfig      `koanf:"secret"`
	DefaultUser DefaultUserConfig `koanf:"defaultuser"`
}

type TMDBConfig struct {
	ApiKey string `koanf:"apikey"`
}

type TVDBConfig struct {
	ApiKey string `koanf:"apikey"`
}

type SecretConfig struct {
	Key string `koanf:"key"`
}

type DefaultUserConfig struct {
	Email    string `koanf:"email"`
	Password string `koanf:"password"`
}

type LibraryConfig struct {
	BasePath string `koanf:"basepath"`
}

type APIConfig struct {
	Port int `koanf:"port"`
}

type DBConfig struct {
	Path string `koanf:"path"`
}

type LogConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

func Load() (*Config, error) {
	k := koanf.New(".")

	// Load from .env file (optional — silently ignored if missing).
	// Keys like API_PORT are mapped to api.port via the callback.
	if err := k.Load(file.Provider(".env"), dotenv.ParserEnv("", ".", func(s string) string {
		return strings.ToLower(strings.ReplaceAll(s, "_", "."))
	})); err != nil && !isFileNotFound(err) {
		return nil, fmt.Errorf("loading .env: %w", err)
	}

	// Overlay with MEDIAGATE_ prefixed environment variables.
	// MEDIAGATE_API_PORT -> api.port
	if err := k.Load(env.Provider("MEDIAGATE_", ".", func(s string) string {
		return strings.ToLower(strings.ReplaceAll(
			strings.TrimPrefix(s, "MEDIAGATE_"), "_", ".",
		))
	}), nil); err != nil {
		return nil, fmt.Errorf("loading env vars: %w", err)
	}

	cfg := Config{
		API:     APIConfig{Port: 8080},
		DB:      DBConfig{Path: "media-gate.db"},
		Log:     LogConfig{Level: "info", Format: "text"},
		Library: LibraryConfig{BasePath: "/mnt"},
	}

	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &cfg, nil
}

func isFileNotFound(err error) bool {
	return strings.Contains(err.Error(), "no such file")
}

package logging

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	mu        sync.Mutex
	curFormat string
	curLevel  string
)

// Setup configures the default slog logger.
// format: "json" or "text" (default).
// level: "debug", "info" (default), "warn", "error".
func Setup(format, level string) {
	mu.Lock()
	defer mu.Unlock()
	curFormat = format
	curLevel = level
	rebuild(nil)
}

// SetOTelHandler installs (or removes) a secondary slog handler that receives
// a copy of every log record. Pass nil to disable.
// When non-nil, a tee handler fans out to both stdout and the OTel handler.
// The level parameter controls the minimum severity sent to OTel independently
// of the stdout log level.
func SetOTelHandler(h slog.Handler, level slog.Level) {
	mu.Lock()
	defer mu.Unlock()
	if h == nil {
		rebuild(nil)
		return
	}
	rebuild(&levelFilterHandler{minLevel: level, inner: h})
}

// rebuild constructs the global slog default from the current format/level
// and an optional secondary (OTel) handler.
func rebuild(otelHandler slog.Handler) {
	primary := buildPrimary(curFormat, curLevel)
	if otelHandler == nil {
		slog.SetDefault(slog.New(primary))
		return
	}
	slog.SetDefault(slog.New(&teeHandler{
		primary:   primary,
		secondary: otelHandler,
	}))
}

func buildPrimary(format, level string) slog.Handler {
	opts := &slog.HandlerOptions{Level: ParseLevel(level)}
	switch strings.ToLower(format) {
	case "json":
		return slog.NewJSONHandler(os.Stdout, opts)
	default:
		return slog.NewTextHandler(os.Stdout, opts)
	}
}

// ParseLevel converts a level string to slog.Level.
// Accepted values: "debug", "info" (default), "warn", "error".
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

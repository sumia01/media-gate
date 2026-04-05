package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup configures the default slog logger.
// format: "json" or "text" (default).
// level: "debug", "info" (default), "warn", "error".
func Setup(format, level string) {
	var handler slog.Handler

	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

func parseLevel(s string) slog.Level {
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

package logging

import (
	"context"
	"log/slog"
)

// teeHandler dispatches each log record to both a primary and a secondary handler.
// Used to fan-out logs to stdout (primary) and OTel (secondary).
type teeHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.primary.Enabled(ctx, level) || t.secondary.Enabled(ctx, level)
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	if t.primary.Enabled(ctx, r.Level) {
		_ = t.primary.Handle(ctx, r.Clone())
	}
	if t.secondary.Enabled(ctx, r.Level) {
		_ = t.secondary.Handle(ctx, r.Clone())
	}
	return nil
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		primary:   t.primary.WithAttrs(attrs),
		secondary: t.secondary.WithAttrs(attrs),
	}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return t
	}
	return &teeHandler{
		primary:   t.primary.WithGroup(name),
		secondary: t.secondary.WithGroup(name),
	}
}

// levelFilterHandler wraps a handler and drops records below a minimum level.
type levelFilterHandler struct {
	minLevel slog.Level
	inner    slog.Handler
}

func (h *levelFilterHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *levelFilterHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.minLevel {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

func (h *levelFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelFilterHandler{
		minLevel: h.minLevel,
		inner:    h.inner.WithAttrs(attrs),
	}
}

func (h *levelFilterHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &levelFilterHandler{
		minLevel: h.minLevel,
		inner:    h.inner.WithGroup(name),
	}
}

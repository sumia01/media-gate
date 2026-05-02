package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/sumia01/media-gate/internal/logging"
)

// Manager handles hot-swapping the global TracerProvider and LoggerProvider at runtime.
// When disabled, a noop provider is set so all instrumentation becomes zero-cost.
type Manager struct {
	mu            sync.Mutex
	version       string
	traceProvider *sdktrace.TracerProvider
	logProvider   *sdklog.LoggerProvider
}

// NewManager creates a Manager and sets the global TracerProvider to noop.
func NewManager(version string) *Manager {
	otel.SetTracerProvider(noop.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return &Manager{version: version}
}

// Reconfigure shuts down the current providers and creates new ones.
// If enabled is false or endpoint is empty, switches to noop.
// logLevel controls the minimum severity for log export (e.g. "info", "warn").
func (m *Manager) Reconfigure(enabled bool, endpoint, service, logLevel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shut down existing trace provider.
	if m.traceProvider != nil {
		if err := m.traceProvider.Shutdown(ctx); err != nil {
			slog.Warn("failed to shut down previous TracerProvider", "error", err)
		}
		m.traceProvider = nil
	}

	// Shut down existing log provider.
	if m.logProvider != nil {
		if err := m.logProvider.Shutdown(ctx); err != nil {
			slog.Warn("failed to shut down previous LoggerProvider", "error", err)
		}
		m.logProvider = nil
	}

	if !enabled || endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		logging.SetOTelHandler(nil, slog.LevelInfo)
		slog.Info("OpenTelemetry disabled")
		return nil
	}

	if service == "" {
		service = "media-gate"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(service),
			semconv.ServiceVersion(m.version),
		),
	)
	if err != nil {
		otel.SetTracerProvider(noop.NewTracerProvider())
		logging.SetOTelHandler(nil, slog.LevelInfo)
		return fmt.Errorf("creating resource: %w", err)
	}

	// --- Traces ---
	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
	)
	if err != nil {
		otel.SetTracerProvider(noop.NewTracerProvider())
		logging.SetOTelHandler(nil, slog.LevelInfo)
		return fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	m.traceProvider = tp

	// --- Logs ---
	logExporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(endpoint),
		otlploghttp.WithURLPath("/v1/logs"),
	)
	if err != nil {
		// Traces still work, just log export fails.
		slog.Warn("failed to create OTLP log exporter, log export disabled", "error", err)
		logging.SetOTelHandler(nil, slog.LevelInfo)
		return nil
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	m.logProvider = lp

	otelHandler := otelslog.NewHandler(service,
		otelslog.WithLoggerProvider(lp),
		otelslog.WithSource(true),
	)

	// Add hardcoded ingestion type attribute to every log record.
	taggedHandler := otelHandler.WithAttrs([]slog.Attr{
		slog.String("mediagate_ingestion_type", "mediagate-log"),
	})

	minLevel := logging.ParseLevel(logLevel)
	logging.SetOTelHandler(taggedHandler, minLevel)

	slog.Info("OpenTelemetry enabled (traces + logs)", "endpoint", endpoint, "service", service, "log_level", logLevel)
	return nil
}

// Shutdown gracefully shuts down both providers.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error

	if m.logProvider != nil {
		if err := m.logProvider.Shutdown(ctx); err != nil {
			firstErr = err
		}
		m.logProvider = nil
		logging.SetOTelHandler(nil, slog.LevelInfo)
	}

	if m.traceProvider != nil {
		if err := m.traceProvider.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		m.traceProvider = nil
		otel.SetTracerProvider(noop.NewTracerProvider())
	}

	return firstErr
}

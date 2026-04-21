package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"
)

// Manager handles hot-swapping the global TracerProvider at runtime.
// When disabled, a noop provider is set so all instrumentation becomes zero-cost.
type Manager struct {
	mu      sync.Mutex
	version string
	current *sdktrace.TracerProvider
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

// Reconfigure shuts down the current provider and creates a new one.
// If enabled is false or endpoint is empty, switches to noop.
func (m *Manager) Reconfigure(enabled bool, endpoint, service string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Shut down existing provider.
	if m.current != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.current.Shutdown(ctx); err != nil {
			slog.Warn("failed to shut down previous TracerProvider", "error", err)
		}
		m.current = nil
	}

	if !enabled || endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		slog.Info("OpenTelemetry tracing disabled")
		return nil
	}

	ctx := context.Background()

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
	)
	if err != nil {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return fmt.Errorf("creating OTLP exporter: %w", err)
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
		return fmt.Errorf("creating resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	m.current = tp

	slog.Info("OpenTelemetry tracing enabled", "endpoint", endpoint, "service", service)
	return nil
}

// Shutdown gracefully shuts down the current TracerProvider.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		err := m.current.Shutdown(ctx)
		m.current = nil
		otel.SetTracerProvider(noop.NewTracerProvider())
		return err
	}
	return nil
}

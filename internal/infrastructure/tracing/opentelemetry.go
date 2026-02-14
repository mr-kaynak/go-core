package tracing

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingService manages OpenTelemetry tracing
type TracingService struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	logger   *logger.Logger
	config   *config.Config
}

// NewTracingService creates and configures OpenTelemetry tracing
func NewTracingService(cfg *config.Config) (*TracingService, error) {
	service := &TracingService{
		config: cfg,
		logger: logger.Get().WithFields(logger.Fields{"service": "tracing"}),
	}

	// Create resource with service information
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.OTEL.ServiceName),
			semconv.ServiceVersionKey.String(cfg.App.Version),
			semconv.DeploymentEnvironmentKey.String(cfg.App.Env),
			attribute.String("service.instance_id", generateInstanceID()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter based on configuration
	exporter, err := service.createExporter(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// Create sampler
	sampler := service.createSampler(cfg)

	// Create tracer provider
	service.provider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxQueueSize(2048),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithSampler(sampler),
	)

	// Register as global provider
	otel.SetTracerProvider(service.provider)

	// Set global propagator
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	// Create tracer
	service.tracer = service.provider.Tracer(
		cfg.OTEL.ServiceName,
		trace.WithInstrumentationVersion(cfg.App.Version),
	)

	service.logger.Info("OpenTelemetry tracing initialized",
		"service", cfg.OTEL.ServiceName,
		"endpoint", cfg.OTEL.Endpoint,
		"enabled", cfg.OTEL.TracesEnabled,
	)

	return service, nil
}

// createExporter creates the appropriate trace exporter
func (s *TracingService) createExporter(cfg *config.Config) (sdktrace.SpanExporter, error) {
	if !cfg.OTEL.TracesEnabled {
		return &noopExporter{}, nil
	}

	endpoint := cfg.OTEL.Endpoint
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
}

// createSampler creates appropriate sampler based on environment
func (s *TracingService) createSampler(cfg *config.Config) sdktrace.Sampler {
	if cfg.IsDevelopment() {
		// Sample everything in development
		return sdktrace.AlwaysSample()
	}

	// Use probability sampling in production
	// Sample 10% of traces in production to reduce overhead
	const productionSampleRate = 0.1
	return sdktrace.TraceIDRatioBased(productionSampleRate)
}

// GetTracer returns the configured tracer
func (s *TracingService) GetTracer() trace.Tracer {
	return s.tracer
}

// GetProvider returns the tracer provider
func (s *TracingService) GetProvider() *sdktrace.TracerProvider {
	return s.provider
}

// Shutdown gracefully shuts down the tracing provider
func (s *TracingService) Shutdown(ctx context.Context) error {
	if s.provider != nil {
		return s.provider.Shutdown(ctx)
	}
	return nil
}

// StartSpan starts a new span
func (s *TracingService) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return s.tracer.Start(ctx, name, opts...)
}

// RecordError records an error in the current span
func RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.IsRecording() {
		span.RecordError(err, opts...)
	}
}

// AddEvent adds an event to the current span
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// SetAttributes sets attributes on the current span
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// SetStatus sets the status of the current span
func SetStatus(ctx context.Context, code codes.Code, description string) {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.IsRecording() {
		span.SetStatus(code, description)
	}
}

// generateInstanceID generates a unique instance ID
func generateInstanceID() string {
	// In production, this could be the pod name, container ID, etc.
	return fmt.Sprintf("instance-%d", time.Now().UnixNano())
}

// noopExporter is a no-op exporter for when tracing is disabled
type noopExporter struct{}

func (e *noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *noopExporter) Shutdown(ctx context.Context) error {
	return nil
}

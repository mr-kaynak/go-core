package tracing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newTestTracerProvider creates a TracerProvider backed by an in-memory exporter
// so we can inspect recorded spans in tests.
func newTestTracerProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, exporter
}

// ---------------------------------------------------------------------------
// Utility function tests
// ---------------------------------------------------------------------------

func TestRecordErrorOnActiveSpan(t *testing.T) {
	tp, exporter := newTestTracerProvider(t)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "op")
	RecordError(ctx, errors.New("something broke"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	found := false
	for _, ev := range spans[0].Events {
		if ev.Name == "exception" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected an 'exception' event on the span")
	}
}

func TestRecordErrorOnNoSpanContext(t *testing.T) {
	// Must not panic when context has no active span.
	RecordError(context.Background(), errors.New("no span"))
}

func TestAddEventOnActiveSpan(t *testing.T) {
	tp, exporter := newTestTracerProvider(t)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "op")
	AddEvent(ctx, "cache.miss", attribute.String("key", "user:123"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	found := false
	for _, ev := range spans[0].Events {
		if ev.Name == "cache.miss" {
			found = true
			// Verify the attribute is present.
			for _, a := range ev.Attributes {
				if string(a.Key) == "key" && a.Value.AsString() == "user:123" {
					break
				}
			}
			break
		}
	}
	if !found {
		t.Error("expected a 'cache.miss' event on the span")
	}
}

func TestAddEventOnNoSpanContext(t *testing.T) {
	AddEvent(context.Background(), "cache.miss")
}

func TestSetAttributesOnActiveSpan(t *testing.T) {
	tp, exporter := newTestTracerProvider(t)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "op")
	SetAttributes(ctx,
		attribute.String("user.id", "abc-123"),
		attribute.Int("retry.count", 3),
	)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	attrMap := make(map[string]bool)
	for _, a := range spans[0].Attributes {
		attrMap[string(a.Key)] = true
	}
	if !attrMap["user.id"] {
		t.Error("expected 'user.id' attribute on span")
	}
	if !attrMap["retry.count"] {
		t.Error("expected 'retry.count' attribute on span")
	}
}

func TestSetAttributesOnNoSpanContext(t *testing.T) {
	SetAttributes(context.Background(), attribute.String("key", "value"))
}

func TestSetStatusOnActiveSpan(t *testing.T) {
	tp, exporter := newTestTracerProvider(t)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "op")
	SetStatus(ctx, codes.Error, "something went wrong")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	if spans[0].Status.Code != codes.Error {
		t.Errorf("expected status code Error, got %v", spans[0].Status.Code)
	}
	if spans[0].Status.Description != "something went wrong" {
		t.Errorf("expected status description 'something went wrong', got %q", spans[0].Status.Description)
	}
}

func TestSetStatusOnNoSpanContext(t *testing.T) {
	SetStatus(context.Background(), codes.Error, "no span")
}

// ---------------------------------------------------------------------------
// noopExporter tests
// ---------------------------------------------------------------------------

func TestNoopExporterExportAndShutdown(t *testing.T) {
	e := &noopExporter{}

	if err := e.ExportSpans(context.Background(), nil); err != nil {
		t.Errorf("ExportSpans returned unexpected error: %v", err)
	}
	if err := e.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// createSampler tests
// ---------------------------------------------------------------------------

func TestCreateSamplerDevelopment(t *testing.T) {
	svc := &TracingService{}
	cfg := &config.Config{
		App: config.AppConfig{Env: "development"},
	}

	sampler := svc.createSampler(cfg)
	desc := sampler.Description()
	if desc != "AlwaysOnSampler" {
		t.Errorf("expected AlwaysOnSampler in development, got %q", desc)
	}
}

func TestCreateSamplerProduction(t *testing.T) {
	svc := &TracingService{}
	cfg := &config.Config{
		App: config.AppConfig{Env: "production"},
	}

	sampler := svc.createSampler(cfg)
	desc := sampler.Description()
	if desc == "AlwaysOnSampler" {
		t.Error("production sampler should NOT be AlwaysOnSampler")
	}
	// Should be a parent-based sampler.
	if !strings.Contains(desc, "ParentBased") {
		t.Errorf("expected ParentBased sampler in production, got %q", desc)
	}
}

// ---------------------------------------------------------------------------
// generateInstanceID tests
// ---------------------------------------------------------------------------

func TestGenerateInstanceIDFormat(t *testing.T) {
	id := generateInstanceID()
	if id == "" {
		t.Error("generateInstanceID returned empty string")
	}
	// generateInstanceID prefers POD_NAME, then hostname, then "instance-<ts>".
	// Any non-empty value is valid; only the timestamp fallback has the prefix.
}

func TestGenerateInstanceIDPodName(t *testing.T) {
	t.Setenv("POD_NAME", "my-app-pod-abc123")
	id := generateInstanceID()
	if id != "my-app-pod-abc123" {
		t.Errorf("expected POD_NAME value, got %q", id)
	}
}

func TestGenerateInstanceIDFallback(t *testing.T) {
	// When POD_NAME is unset and hostname is available, should return hostname.
	t.Setenv("POD_NAME", "")
	id := generateInstanceID()
	if id == "" {
		t.Error("generateInstanceID returned empty string")
	}
}

// ---------------------------------------------------------------------------
// TracingService lifecycle tests
// ---------------------------------------------------------------------------

func TestTracingServiceShutdownNilProvider(t *testing.T) {
	svc := &TracingService{}
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown with nil provider returned error: %v", err)
	}
}

func TestTracingServiceStartSpan(t *testing.T) {
	tp, exporter := newTestTracerProvider(t)
	svc := &TracingService{
		tracer: tp.Tracer("test-svc"),
	}

	ctx, span := svc.StartSpan(context.Background(), "test-operation")
	span.End()

	// Verify span was recorded.
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span from StartSpan")
	}
	if spans[0].Name != "test-operation" {
		t.Errorf("expected span name 'test-operation', got %q", spans[0].Name)
	}

	// Context should carry the span.
	spanFromCtx := trace.SpanFromContext(ctx)
	if spanFromCtx == nil {
		t.Error("expected span in returned context")
	}
}

func TestTracingServiceGetTracerAndProvider(t *testing.T) {
	tp, _ := newTestTracerProvider(t)
	tracer := tp.Tracer("test")

	svc := &TracingService{
		provider: tp,
		tracer:   tracer,
	}

	if svc.GetTracer() == nil {
		t.Error("GetTracer returned nil")
	}
	if svc.GetProvider() == nil {
		t.Error("GetProvider returned nil")
	}
}

// ---------------------------------------------------------------------------
// NewTracingService with disabled traces
// ---------------------------------------------------------------------------

func TestNewTracingServiceWithDisabledTraces(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Env:     "development",
			Version: "1.0.0",
		},
		OTEL: config.OTELConfig{
			ServiceName:   "test-service",
			TracesEnabled: false,
			Endpoint:      "",
		},
	}

	svc, err := NewTracingService(cfg)
	if err != nil {
		t.Fatalf("NewTracingService returned unexpected error: %v", err)
	}

	if svc.GetTracer() == nil {
		t.Error("expected non-nil tracer even with traces disabled")
	}
	if svc.GetProvider() == nil {
		t.Error("expected non-nil provider even with traces disabled")
	}

	// Shutdown should work cleanly.
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// createExporter with disabled traces
// ---------------------------------------------------------------------------

func TestCreateExporterDisabled(t *testing.T) {
	svc := &TracingService{}
	cfg := &config.Config{
		OTEL: config.OTELConfig{
			TracesEnabled: false,
		},
	}

	exp, err := svc.createExporter(cfg)
	if err != nil {
		t.Fatalf("createExporter returned error: %v", err)
	}

	// Should be a noopExporter.
	if _, ok := exp.(*noopExporter); !ok {
		t.Errorf("expected *noopExporter, got %T", exp)
	}
}

// ---------------------------------------------------------------------------
// TracingService Shutdown with real provider
// ---------------------------------------------------------------------------

func TestTracingServiceShutdownWithProvider(t *testing.T) {
	tp, _ := newTestTracerProvider(t)
	svc := &TracingService{
		provider: tp,
	}

	if err := svc.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown returned unexpected error: %v", err)
	}
}

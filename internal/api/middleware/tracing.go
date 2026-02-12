package middleware

import (
	"fmt"

	"github.com/gofiber/contrib/otelfiber"
	"github.com/gofiber/fiber/v2"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware creates OpenTelemetry tracing middleware for Fiber
func TracingMiddleware(tracingService *tracing.TracingService) fiber.Handler {
	return otelfiber.Middleware(
		otelfiber.WithTracerProvider(tracingService.GetProvider()),
		otelfiber.WithSpanNameFormatter(spanNameFormatter),
	)
}

// spanNameFormatter formats the span name based on the request
func spanNameFormatter(c *fiber.Ctx) string {
	method := c.Method()
	route := c.Route().Path

	if route != "" {
		return fmt.Sprintf("%s %s", method, route)
	}

	// Fallback to path if route is not available
	return fmt.Sprintf("%s %s", method, c.Path())
}

// TracingHelper provides utility functions for tracing in handlers
type TracingHelper struct {
	tracer trace.Tracer
}

// NewTracingHelper creates a new tracing helper
func NewTracingHelper(tracer trace.Tracer) *TracingHelper {
	return &TracingHelper{
		tracer: tracer,
	}
}

// StartSpanFromFiber starts a new span from Fiber context
func (h *TracingHelper) StartSpanFromFiber(c *fiber.Ctx, name string, opts ...trace.SpanStartOption) (span trace.Span, end func()) {
	ctx := c.UserContext()
	ctx, span = h.tracer.Start(ctx, name, opts...)
	c.SetUserContext(ctx)

	return span, func() {
		span.End()
	}
}

// RecordError records an error in the current span
func (h *TracingHelper) RecordError(c *fiber.Ctx, err error) {
	span := trace.SpanFromContext(c.UserContext())
	if span != nil && span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// AddEvent adds an event to the current span
func (h *TracingHelper) AddEvent(c *fiber.Ctx, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(c.UserContext())
	if span != nil && span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// SetAttributes sets attributes on the current span
func (h *TracingHelper) SetAttributes(c *fiber.Ctx, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(c.UserContext())
	if span != nil && span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// DatabaseTracingMiddleware adds database query tracing
func DatabaseTracingMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// This would be integrated with GORM hooks for automatic database tracing
		// For now, it's a placeholder that adds database context
		span := trace.SpanFromContext(c.UserContext())
		if span != nil && span.IsRecording() {
			span.SetAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.name", "go_core"),
			)
		}

		return c.Next()
	}
}

// CacheTracingMiddleware adds cache operation tracing
func CacheTracingMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// This would be integrated with Redis operations
		span := trace.SpanFromContext(c.UserContext())
		if span != nil && span.IsRecording() {
			span.SetAttributes(
				attribute.String("cache.system", "redis"),
			)
		}

		return c.Next()
	}
}

// MessageQueueTracingMiddleware adds message queue tracing
func MessageQueueTracingMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// This would be integrated with RabbitMQ operations
		span := trace.SpanFromContext(c.UserContext())
		if span != nil && span.IsRecording() {
			span.SetAttributes(
				attribute.String("messaging.system", "rabbitmq"),
			)
		}

		return c.Next()
	}
}

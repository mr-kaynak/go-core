package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"
)

// Logger is a wrapper around slog.Logger with additional functionality
type Logger struct {
	*slog.Logger
	level slog.Level
}

// Fields is a type alias for structured logging fields
type Fields map[string]interface{}

var (
	// defaultLogger is the global logger instance
	defaultLogger *Logger
)

// Initialize sets up the global logger
func Initialize(level, format, output string) error {
	var handler slog.Handler
	var logLevel slog.Level

	// Parse log level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	// Configure output
	var writer *os.File
	switch strings.ToLower(output) {
	case "stdout":
		writer = os.Stdout
	case "stderr":
		writer = os.Stderr
	default:
		file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		writer = file
	}

	// Configure handler based on format
	handlerOpts := &slog.HandlerOptions{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize time format
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format(time.RFC3339))
				}
			}
			// Add source location for errors
			if a.Key == slog.SourceKey {
				if src, ok := a.Value.Any().(*slog.Source); ok {
					a.Value = slog.StringValue(fmt.Sprintf("%s:%d", src.File, src.Line))
				}
			}
			return a
		},
	}

	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(writer, handlerOpts)
	case "text":
		handler = slog.NewTextHandler(writer, handlerOpts)
	default:
		handler = slog.NewJSONHandler(writer, handlerOpts)
	}

	logger := slog.New(handler)
	defaultLogger = &Logger{
		Logger: logger,
		level:  logLevel,
	}

	// Set as default slog logger
	slog.SetDefault(logger)

	return nil
}

// Get returns the global logger instance
func Get() *Logger {
	if defaultLogger == nil {
		// Initialize with default settings if not already initialized
		_ = Initialize("info", "json", "stdout")
	}
	return defaultLogger
}

// WithContext returns a logger with context values
func WithContext(ctx context.Context) *Logger {
	logger := Get()

	// Extract trace ID from context if available
	if traceID := GetTraceID(ctx); traceID != "" {
		logger = &Logger{
			Logger: logger.With("trace_id", traceID),
			level:  logger.level,
		}
	}

	// Extract user ID from context if available
	if userID := GetUserID(ctx); userID != "" {
		logger = &Logger{
			Logger: logger.With("user_id", userID),
			level:  logger.level,
		}
	}

	// Extract request ID from context if available
	if requestID := GetRequestID(ctx); requestID != "" {
		logger = &Logger{
			Logger: logger.With("request_id", requestID),
			level:  logger.level,
		}
	}

	return logger
}

// WithFields returns a logger with additional fields
func (l *Logger) WithFields(fields Fields) *Logger {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return &Logger{
		Logger: l.With(args...),
		level:  l.level,
	}
}

// WithField returns a logger with a single additional field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{
		Logger: l.With(key, value),
		level:  l.level,
	}
}

// IsDebugEnabled returns true if debug level logging is enabled
func (l *Logger) IsDebugEnabled() bool {
	return l.level <= slog.LevelDebug
}

// WithError adds an error field to the logger
func (l *Logger) WithError(err error) *Logger {
	if err == nil {
		return l
	}
	return &Logger{
		Logger: l.With("error", err.Error()),
		level:  l.level,
	}
}

// WithCaller adds the caller information to the logger
func (l *Logger) WithCaller() *Logger {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		return l
	}
	return &Logger{
		Logger: l.With("caller", fmt.Sprintf("%s:%d", file, line)),
		level:  l.level,
	}
}

// Debug logs a debug message
func Debug(msg string, args ...interface{}) {
	Get().Debug(msg, args...)
}

// Info logs an info message
func Info(msg string, args ...interface{}) {
	Get().Info(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...interface{}) {
	Get().Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...interface{}) {
	Get().Error(msg, args...)
}

// Fatal logs a fatal message and exits
func Fatal(msg string, args ...interface{}) {
	Get().Error(msg, args...)
	os.Exit(1)
}

// Context key types
type contextKey string

const (
	traceIDKey   contextKey = "trace_id"
	userIDKey    contextKey = "user_id"
	requestIDKey contextKey = "request_id"
)

// SetTraceID sets the trace ID in the context
func SetTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// GetTraceID gets the trace ID from the context
func GetTraceID(ctx context.Context) string {
	if v := ctx.Value(traceIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SetUserID sets the user ID in the context
func SetUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// GetUserID gets the user ID from the context
func GetUserID(ctx context.Context) string {
	if v := ctx.Value(userIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SetRequestID sets the request ID in the context
func SetRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// GetRequestID gets the request ID from the context
func GetRequestID(ctx context.Context) string {
	if v := ctx.Value(requestIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Middleware returns a function that can be used as middleware
func Middleware(next func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		logger := WithContext(ctx)
		logger.Debug("Starting request processing")

		start := time.Now()
		err := next(ctx)
		duration := time.Since(start)

		if err != nil {
			logger.WithError(err).WithFields(Fields{
				"duration_ms": duration.Milliseconds(),
			}).Error("Request failed")
		} else {
			logger.WithFields(Fields{
				"duration_ms": duration.Milliseconds(),
			}).Info("Request completed successfully")
		}

		return err
	}
}

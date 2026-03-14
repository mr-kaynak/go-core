package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func resetDefaultLogger(t *testing.T) {
	t.Helper()
	prev := defaultLogger
	t.Cleanup(func() { defaultLogger = prev })
}

func TestInitializeLevelFormatOutput(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "app.log")

	if err := Initialize("debug", "text", logFile); err != nil {
		t.Fatalf("expected initialize success, got %v", err)
	}

	l := Get()
	if !l.IsDebugEnabled() {
		t.Fatalf("expected debug logging to be enabled")
	}

	l.Debug("debug message", "k", "v")
	l.Info("info message", "k", "v")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log output: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "debug message") {
		t.Fatalf("expected debug message in log output")
	}
	if !strings.Contains(content, "info message") {
		t.Fatalf("expected info message in log output")
	}
}

func TestGetReturnsSingleton(t *testing.T) {
	resetDefaultLogger(t)
	if err := Initialize("info", "json", "stdout"); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l1 := Get()
	l2 := Get()
	if l1 != l2 {
		t.Fatalf("expected Get to return singleton instance")
	}
}

func TestLogLevelFilteringInfoDropsDebug(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "level.log")

	if err := Initialize("info", "text", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get()
	l.Debug("should be filtered")
	l.Info("should stay")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log output: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "should be filtered") {
		t.Fatalf("expected debug log to be filtered at info level")
	}
	if !strings.Contains(content, "should stay") {
		t.Fatalf("expected info log to be present")
	}
}

// --- Context round-trip tests ---

func TestContextSetGetTraceID(t *testing.T) {
	ctx := context.Background()
	if got := GetTraceID(ctx); got != "" {
		t.Fatalf("expected empty trace_id on fresh context, got %q", got)
	}
	ctx = SetTraceID(ctx, "abc-trace-123")
	if got := GetTraceID(ctx); got != "abc-trace-123" {
		t.Fatalf("expected abc-trace-123, got %q", got)
	}
}

func TestContextSetGetUserID(t *testing.T) {
	ctx := context.Background()
	if got := GetUserID(ctx); got != "" {
		t.Fatalf("expected empty user_id on fresh context, got %q", got)
	}
	ctx = SetUserID(ctx, "user-456")
	if got := GetUserID(ctx); got != "user-456" {
		t.Fatalf("expected user-456, got %q", got)
	}
}

func TestContextSetGetRequestID(t *testing.T) {
	ctx := context.Background()
	if got := GetRequestID(ctx); got != "" {
		t.Fatalf("expected empty request_id on fresh context, got %q", got)
	}
	ctx = SetRequestID(ctx, "req-789")
	if got := GetRequestID(ctx); got != "req-789" {
		t.Fatalf("expected req-789, got %q", got)
	}
}

// --- WithContext enrichment ---

func TestWithContextEnrichesLogger(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "ctx.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	ctx := context.Background()
	ctx = SetTraceID(ctx, "trace-aaa")
	ctx = SetUserID(ctx, "user-bbb")
	ctx = SetRequestID(ctx, "req-ccc")

	l := WithContext(ctx)
	l.Info("context enriched message")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	for _, want := range []string{"trace-aaa", "user-bbb", "req-ccc"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q in log output, got: %s", want, content)
		}
	}
}

// --- WithFields, WithField, WithError ---

func TestWithFieldsAddsMultipleFields(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "fields.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get().WithFields(Fields{"module": "auth", "action": "login"})
	l.Info("multi-field log")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "auth") {
		t.Fatalf("expected 'auth' in log output")
	}
	if !strings.Contains(content, "login") {
		t.Fatalf("expected 'login' in log output")
	}
}

func TestWithFieldAddsSingleField(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "field.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get().WithField("component", "cache")
	l.Info("single-field log")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "cache") {
		t.Fatalf("expected 'cache' in log output")
	}
}

func TestWithErrorAddsErrorField(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "err.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get().WithError(fmt.Errorf("connection refused"))
	l.Error("something failed")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "connection refused") {
		t.Fatalf("expected 'connection refused' in log output")
	}
}

func TestWithErrorNilReturnsOriginalLogger(t *testing.T) {
	resetDefaultLogger(t)
	if err := Initialize("info", "json", "stdout"); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get()
	l2 := l.WithError(nil)
	if l != l2 {
		t.Fatalf("expected WithError(nil) to return the same logger pointer")
	}
}

// --- WithCaller ---

func TestWithCallerAddsCallerInfo(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "caller.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get().WithCaller()
	l.Info("caller info test")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "logger_test.go") {
		t.Fatalf("expected 'logger_test.go' in log output, got: %s", content)
	}
}

// --- Sensitive key redaction ---

func TestSensitiveKeyRedaction(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "redact.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	l := Get()
	l.Info("sensitive test",
		"password", "supersecret",
		"token", "tok-abc",
		"authorization", "Bearer xyz",
		"api_key", "key-123",
		"username", "johndoe",
	)

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)

	// Sensitive values should be redacted
	for _, secret := range []string{"supersecret", "tok-abc", "Bearer xyz", "key-123"} {
		if strings.Contains(content, secret) {
			t.Fatalf("expected %q to be redacted, but found in output", secret)
		}
	}
	// [REDACTED] should appear for each sensitive key
	if !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in output")
	}
	// Non-sensitive field should remain
	if !strings.Contains(content, "johndoe") {
		t.Fatalf("expected 'johndoe' to remain in output")
	}
}

func TestIsSensitiveKeyTableDriven(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"Password", true},
		{"PASSWORD", true},
		{"token", true},
		{"Token", true},
		{"secret", true},
		{"authorization", true},
		{"Authorization", true},
		{"api_key", true},
		{"apikey", true},
		{"access_token", true},
		{"refresh_token", true},
		{"credit_card", true},
		{"ssn", true},
		{"username", false},
		{"email", false},
		{"status", false},
		{"module", false},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got := isSensitiveKey(tc.key)
			if got != tc.want {
				t.Fatalf("isSensitiveKey(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// --- Middleware ---

func TestMiddlewareLogsSuccessAndError(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "mw.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	ctx := context.Background()

	// Success path
	successHandler := Middleware(func(ctx context.Context) error {
		return nil
	})
	if err := successHandler(ctx); err != nil {
		t.Fatalf("expected nil error from success handler, got %v", err)
	}

	// Error path
	errHandler := Middleware(func(ctx context.Context) error {
		return fmt.Errorf("something broke")
	})
	if err := errHandler(ctx); err == nil {
		t.Fatalf("expected error from error handler, got nil")
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "Request completed successfully") {
		t.Fatalf("expected 'Request completed successfully' in log output")
	}
	if !strings.Contains(content, "Request failed") {
		t.Fatalf("expected 'Request failed' in log output")
	}
	if !strings.Contains(content, "duration_ms") {
		t.Fatalf("expected 'duration_ms' in log output")
	}
}

// --- Close and lifecycle ---

func TestCloseFileLogger(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "close.log")
	if err := Initialize("info", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	l := Get()
	if err := l.Close(); err != nil {
		t.Fatalf("expected Close to succeed, got %v", err)
	}
}

func TestCloseNilLogFile(t *testing.T) {
	resetDefaultLogger(t)
	if err := Initialize("info", "json", "stdout"); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	l := Get()
	if err := l.Close(); err != nil {
		t.Fatalf("expected Close to succeed for stdout logger, got %v", err)
	}
}

func TestPackageLevelCloseNilLogger(t *testing.T) {
	resetDefaultLogger(t)
	defaultLogger = nil
	if err := Close(); err != nil {
		t.Fatalf("expected Close to succeed when defaultLogger is nil, got %v", err)
	}
}

func TestGetLazyInitializesDefaultLogger(t *testing.T) {
	resetDefaultLogger(t)
	defaultLogger = nil
	loggerOnce = sync.Once{}
	l := Get()
	if l == nil {
		t.Fatalf("expected Get() to lazy-initialize a non-nil logger")
	}
}

// --- Level and format variations ---

func TestInitializeJSONFormat(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "json.log")
	if err := Initialize("info", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	Get().Info("json format test")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"msg"`) {
		t.Fatalf("expected JSON key '\"msg\"' in output, got: %s", content)
	}
}

func TestInitializeWarnLevel(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "warn.log")
	if err := Initialize("warn", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	l := Get()
	l.Info("should be filtered at warn level")
	l.Warn("warn should stay")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "should be filtered at warn level") {
		t.Fatalf("expected info to be filtered at warn level")
	}
	if !strings.Contains(content, "warn should stay") {
		t.Fatalf("expected warn message to be present")
	}
}

func TestInitializeErrorLevel(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "error.log")
	if err := Initialize("error", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	l := Get()
	l.Warn("warn filtered at error level")
	l.Error("error should stay")

	if l.IsDebugEnabled() {
		t.Fatalf("expected IsDebugEnabled to be false at error level")
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "warn filtered at error level") {
		t.Fatalf("expected warn to be filtered at error level")
	}
	if !strings.Contains(content, "error should stay") {
		t.Fatalf("expected error message to be present")
	}
}

func TestInitializeDefaultLevel(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "default.log")
	if err := Initialize("unknown_level", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	l := Get()
	l.Debug("debug should be filtered with default info level")
	l.Info("info should stay with default level")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "debug should be filtered") {
		t.Fatalf("expected debug to be filtered at default info level")
	}
	if !strings.Contains(content, "info should stay") {
		t.Fatalf("expected info message to be present at default level")
	}
}

func TestInitializeDefaultFormat(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "defaultfmt.log")
	if err := Initialize("info", "unknown_format", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	Get().Info("default format test")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)
	// Default format is json, so we expect JSON-style output
	if !strings.Contains(content, `"msg"`) {
		t.Fatalf("expected JSON format as default, got: %s", content)
	}
}

func TestInitializeStderrOutput(t *testing.T) {
	resetDefaultLogger(t)
	if err := Initialize("info", "json", "stderr"); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	l := Get()
	if l.logFile != nil {
		t.Fatalf("expected logFile to be nil for stderr output")
	}
}

func TestInitializeInvalidFilePath(t *testing.T) {
	resetDefaultLogger(t)
	err := Initialize("info", "json", "/nonexistent/directory/that/doesnt/exist/app.log")
	if err == nil {
		t.Fatalf("expected error for invalid file path, got nil")
	}
}

// --- Package-level log functions ---

func TestPackageLevelLogFunctions(t *testing.T) {
	resetDefaultLogger(t)
	logFile := filepath.Join(t.TempDir(), "pkglevel.log")
	if err := Initialize("debug", "json", logFile); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	Debug("debug pkg msg", "k", "v")
	Info("info pkg msg", "k", "v")
	Warn("warn pkg msg", "k", "v")
	Error("error pkg msg", "k", "v")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed reading log: %v", err)
	}
	content := string(data)

	for _, msg := range []string{"debug pkg msg", "info pkg msg", "warn pkg msg", "error pkg msg"} {
		if !strings.Contains(content, msg) {
			t.Fatalf("expected %q in log output", msg)
		}
	}
}

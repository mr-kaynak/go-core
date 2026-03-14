package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const testTraceID = "550e8400-e29b-41d4-a716-446655440000"

// TestNewBadRequest creates and validates bad request error
func TestNewBadRequest(t *testing.T) {
	detail := "Invalid input"
	err := NewBadRequest(detail)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, pd.Status)
	}

	if pd.Title == "" {
		t.Errorf("expected non-empty title")
	}
}

// TestNewUnauthorized creates and validates unauthorized error
func TestNewUnauthorized(t *testing.T) {
	detail := "Missing authentication"
	err := NewUnauthorized(detail)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, pd.Status)
	}
}

// TestNewForbidden creates and validates forbidden error
func TestNewForbidden(t *testing.T) {
	detail := "Insufficient permissions"
	err := NewForbidden(detail)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, pd.Status)
	}
}

// TestNewNotFound creates and validates not found error
func TestNewNotFound(t *testing.T) {
	resource := "User"
	id := "123e4567-e89b-12d3-a456-426614174000"
	err := NewNotFound(resource, id)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, pd.Status)
	}
}

// TestNewConflict creates and validates conflict error
func TestNewConflict(t *testing.T) {
	detail := "Resource already exists"
	err := NewConflict(detail)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, pd.Status)
	}
}

// TestNewInternalError creates and validates internal server error
func TestNewInternalError(t *testing.T) {
	detail := "Something went wrong"
	err := NewInternalError(detail)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, pd.Status)
	}
}

// TestNewServiceUnavailable creates and validates service unavailable error
func TestNewServiceUnavailable(t *testing.T) {
	service := "database"
	err := NewServiceUnavailable(service)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, pd.Status)
	}
}

// TestNewRateLimitExceeded creates and validates rate limit error
func TestNewRateLimitExceeded(t *testing.T) {
	limit := 60
	err := NewRateLimitExceeded(limit)

	if err == nil {
		t.Errorf("expected non-nil error")
	}

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	if pd.Status != http.StatusTooManyRequests {
		t.Errorf("expected status %d, got %d", http.StatusTooManyRequests, pd.Status)
	}
}

// TestProblemDetailWithTraceID tests adding trace ID to error
func TestProblemDetailWithTraceID(t *testing.T) {
	err := NewBadRequest("Invalid input")
	traceID := testTraceID

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	result := pd.WithTraceID(traceID)
	if result == nil {
		t.Fatalf("expected non-nil result from WithTraceID")
	}

	if result.TraceID != traceID {
		t.Errorf("expected trace ID %s, got %s", traceID, result.TraceID)
	}
}

// TestProblemDetailWithInstance tests adding instance to error
func TestProblemDetailWithInstance(t *testing.T) {
	err := NewBadRequest("Invalid input")
	instance := "/api/v1/users"

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	result := pd.WithInstance(instance)
	if result == nil {
		t.Fatalf("expected non-nil result from WithInstance")
	}

	if result.Instance != instance {
		t.Errorf("expected instance %s, got %s", instance, result.Instance)
	}
}

// TestProblemDetailMethodChaining tests method chaining
func TestProblemDetailMethodChaining(t *testing.T) {
	err := NewBadRequest("Invalid input")
	traceID := testTraceID
	instance := "/api/v1/users"

	pd := GetProblemDetail(err)
	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	result := pd.WithTraceID(traceID).WithInstance(instance)

	if result.TraceID != traceID {
		t.Errorf("expected trace ID %s, got %s", traceID, result.TraceID)
	}

	if result.Instance != instance {
		t.Errorf("expected instance %s, got %s", instance, result.Instance)
	}
}

// TestGetProblemDetailNil tests GetProblemDetail with nil
func TestGetProblemDetailNil(t *testing.T) {
	pd := GetProblemDetail(nil)
	if pd != nil {
		t.Errorf("expected nil for nil error, got %v", pd)
	}
}

// TestGetProblemDetailNonProblemDetail tests GetProblemDetail with non-ProblemDetail error
func TestGetProblemDetailNonProblemDetail(t *testing.T) {
	regularErr := NewBadRequest("test")
	pd := GetProblemDetail(regularErr)

	// Should still return ProblemDetail if it wraps one
	if pd == nil {
		t.Errorf("expected ProblemDetail for wrapped error")
	}
}

// TestErrorCodeConstants validates error code constants
func TestErrorCodeConstants(t *testing.T) {
	codes := []ErrorCode{
		CodeUnauthorized,
		CodeInvalidCredentials,
		CodeTokenExpired,
		CodeTokenInvalid,
		CodeInsufficientRights,
		CodeUserNotFound,
		CodeUserAlreadyExists,
		CodeValidationFailed,
	}

	for _, code := range codes {
		if code == "" {
			t.Errorf("error code should not be empty")
		}
	}
}

// TestProblemDetailStructure validates ProblemDetail RFC 7807 compliance
func TestProblemDetailStructure(t *testing.T) {
	err := NewBadRequest("Invalid field")
	pd := GetProblemDetail(err)

	if pd == nil {
		t.Fatalf("expected ProblemDetail, got nil")
	}

	// RFC 7807 required fields
	if pd.Type == "" {
		t.Errorf("type should not be empty")
	}

	if pd.Title == "" {
		t.Errorf("title should not be empty")
	}

	if pd.Status == 0 {
		t.Errorf("status should not be zero")
	}

	// RFC 7807 optional but recommended
	if pd.Detail == "" {
		t.Errorf("detail should not be empty")
	}
}

// BenchmarkNewBadRequest benchmarks error creation
func BenchmarkNewBadRequest(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewBadRequest("Invalid input")
	}
}

// BenchmarkGetProblemDetail benchmarks error extraction
func BenchmarkGetProblemDetail(b *testing.B) {
	err := NewBadRequest("Invalid input")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetProblemDetail(err)
	}
}

// BenchmarkWithTraceID benchmarks adding trace ID
func BenchmarkWithTraceID(b *testing.B) {
	err := NewBadRequest("Invalid input")
	pd := GetProblemDetail(err)
	traceID := testTraceID

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pd.WithTraceID(traceID)
	}
}

// --- New tests for uncovered functions ---

// TestNewValidationError tests validation error creation with table-driven cases
func TestNewValidationError(t *testing.T) {
	tests := []struct {
		name   string
		detail string
	}{
		{name: "simple message", detail: "field is required"},
		{name: "empty detail", detail: ""},
		{name: "special characters", detail: "field 'email' must match <user>@<domain>"},
		{name: "unicode characters", detail: "Feld ist ungültig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewValidationError(tt.detail)
			if err == nil {
				t.Fatal("expected non-nil error")
			}

			if err.Status != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d", http.StatusBadRequest, err.Status)
			}

			if err.Title != "Validation Failed" {
				t.Errorf("expected title 'Validation Failed', got %q", err.Title)
			}

			if err.Code != CodeValidationFailed {
				t.Errorf("expected code %s, got %s", CodeValidationFailed, err.Code)
			}

			if err.Detail != tt.detail {
				t.Errorf("expected detail %q, got %q", tt.detail, err.Detail)
			}
		})
	}
}

// TestNewTooManyRequests tests too many requests error creation
func TestNewTooManyRequests(t *testing.T) {
	tests := []struct {
		name   string
		detail string
	}{
		{name: "custom message", detail: "please slow down"},
		{name: "empty message", detail: ""},
		{name: "detailed message", detail: "rate limit of 100 requests per minute exceeded for IP 10.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewTooManyRequests(tt.detail)
			if err == nil {
				t.Fatal("expected non-nil error")
			}

			if err.Status != http.StatusTooManyRequests {
				t.Errorf("expected status %d, got %d", http.StatusTooManyRequests, err.Status)
			}

			if err.Title != "Too Many Requests" {
				t.Errorf("expected title 'Too Many Requests', got %q", err.Title)
			}

			if err.Code != CodeRateLimitExceeded {
				t.Errorf("expected code %s, got %s", CodeRateLimitExceeded, err.Code)
			}

			if err.Detail != tt.detail {
				t.Errorf("expected detail %q, got %q", tt.detail, err.Detail)
			}
		})
	}
}

// TestNew tests the generic New constructor
func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		code   ErrorCode
		status int
		title  string
		detail string
	}{
		{
			name:   "custom error",
			code:   ErrorCode("CUSTOM_001"),
			status: http.StatusTeapot,
			title:  "I'm a teapot",
			detail: "short and stout",
		},
		{
			name:   "empty detail",
			code:   CodeBadRequest,
			status: http.StatusBadRequest,
			title:  "Bad Request",
			detail: "",
		},
		{
			name:   "empty title and detail",
			code:   CodeInternal,
			status: http.StatusInternalServerError,
			title:  "",
			detail: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := New(tt.code, tt.status, tt.title, tt.detail)
			if pd == nil {
				t.Fatal("expected non-nil ProblemDetail")
			}

			if pd.Code != tt.code {
				t.Errorf("expected code %s, got %s", tt.code, pd.Code)
			}
			if pd.Status != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, pd.Status)
			}
			if pd.Title != tt.title {
				t.Errorf("expected title %q, got %q", tt.title, pd.Title)
			}
			if pd.Detail != tt.detail {
				t.Errorf("expected detail %q, got %q", tt.detail, pd.Detail)
			}
			if pd.Time.IsZero() {
				t.Error("expected non-zero time")
			}
			if !strings.Contains(pd.Type, string(tt.code)) {
				t.Errorf("expected type to contain code %q, got %q", tt.code, pd.Type)
			}
		})
	}
}

// TestProblemDetailError tests the Error() method on ProblemDetail
func TestProblemDetailError(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		detail   string
		expected string
	}{
		{
			name:     "returns detail when set",
			title:    "Bad Request",
			detail:   "invalid email format",
			expected: "invalid email format",
		},
		{
			name:     "returns title when detail is empty",
			title:    "Internal Server Error",
			detail:   "",
			expected: "Internal Server Error",
		},
		{
			name:     "returns detail over title",
			title:    "Conflict",
			detail:   "email already exists",
			expected: "email already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := &ProblemDetail{Title: tt.title, Detail: tt.detail}
			result := pd.Error()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestProblemDetailUnwrap tests the Unwrap() method
func TestProblemDetailUnwrap(t *testing.T) {
	t.Run("nil wrapped error", func(t *testing.T) {
		pd := NewBadRequest("test")
		if pd.Unwrap() != nil {
			t.Error("expected nil from Unwrap when no error is wrapped")
		}
	})

	t.Run("non-nil wrapped error", func(t *testing.T) {
		inner := errors.New("database connection failed")
		pd := NewInternalError("something went wrong").WithError(inner)
		unwrapped := pd.Unwrap()
		if unwrapped == nil {
			t.Fatal("expected non-nil from Unwrap")
		}
		if unwrapped.Error() != "database connection failed" {
			t.Errorf("expected wrapped error message, got %q", unwrapped.Error())
		}
	})

	t.Run("errors.Is works through Unwrap", func(t *testing.T) {
		sentinel := errors.New("sentinel error")
		pd := NewInternalError("wrapped").WithError(sentinel)
		if !errors.Is(pd, sentinel) {
			t.Error("expected errors.Is to find sentinel through Unwrap chain")
		}
	})
}

// TestProblemDetailWithError tests the WithError() method
func TestProblemDetailWithError(t *testing.T) {
	t.Run("wraps an error", func(t *testing.T) {
		inner := errors.New("connection refused")
		pd := NewServiceUnavailable("database").WithError(inner)

		if pd.Unwrap() == nil {
			t.Fatal("expected wrapped error")
		}
		if pd.Unwrap().Error() != "connection refused" {
			t.Errorf("expected 'connection refused', got %q", pd.Unwrap().Error())
		}
	})

	t.Run("wraps nil error", func(t *testing.T) {
		pd := NewBadRequest("test").WithError(nil)
		if pd.Unwrap() != nil {
			t.Error("expected nil from Unwrap after wrapping nil")
		}
	})

	t.Run("returns same ProblemDetail for chaining", func(t *testing.T) {
		pd := NewBadRequest("test")
		result := pd.WithError(errors.New("inner"))
		if result != pd {
			t.Error("expected WithError to return the same ProblemDetail instance")
		}
	})
}

// TestProblemDetailWithMeta tests the WithMeta() method
func TestProblemDetailWithMeta(t *testing.T) {
	t.Run("adds single metadata key", func(t *testing.T) {
		pd := NewBadRequest("test").WithMeta("field", "email")
		if pd.Meta == nil {
			t.Fatal("expected non-nil Meta map")
		}
		if pd.Meta["field"] != "email" {
			t.Errorf("expected meta field='email', got %v", pd.Meta["field"])
		}
	})

	t.Run("adds multiple metadata keys", func(t *testing.T) {
		pd := NewBadRequest("test").
			WithMeta("field", "email").
			WithMeta("constraint", "required")

		if len(pd.Meta) != 2 {
			t.Errorf("expected 2 meta entries, got %d", len(pd.Meta))
		}
		if pd.Meta["field"] != "email" {
			t.Errorf("expected field='email', got %v", pd.Meta["field"])
		}
		if pd.Meta["constraint"] != "required" {
			t.Errorf("expected constraint='required', got %v", pd.Meta["constraint"])
		}
	})

	t.Run("overwrites existing key", func(t *testing.T) {
		pd := NewBadRequest("test").
			WithMeta("field", "email").
			WithMeta("field", "username")

		if pd.Meta["field"] != "username" {
			t.Errorf("expected field='username' after overwrite, got %v", pd.Meta["field"])
		}
	})

	t.Run("supports various value types", func(t *testing.T) {
		pd := NewBadRequest("test").
			WithMeta("count", 42).
			WithMeta("valid", false).
			WithMeta("tags", []string{"a", "b"})

		if pd.Meta["count"] != 42 {
			t.Errorf("expected count=42, got %v", pd.Meta["count"])
		}
		if pd.Meta["valid"] != false {
			t.Errorf("expected valid=false, got %v", pd.Meta["valid"])
		}
	})

	t.Run("returns same ProblemDetail for chaining", func(t *testing.T) {
		pd := NewBadRequest("test")
		result := pd.WithMeta("key", "value")
		if result != pd {
			t.Error("expected WithMeta to return the same ProblemDetail instance")
		}
	})
}

// TestProblemDetailJSON tests the JSON() method
func TestProblemDetailJSON(t *testing.T) {
	t.Run("produces valid JSON", func(t *testing.T) {
		pd := NewBadRequest("invalid email")
		data := pd.JSON()
		if len(data) == 0 {
			t.Fatal("expected non-empty JSON")
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("expected valid JSON, got error: %v", err)
		}
	})

	t.Run("contains required RFC 7807 fields", func(t *testing.T) {
		pd := NewBadRequest("invalid email")
		data := pd.JSON()

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		requiredFields := []string{"type", "title", "status", "detail", "code", "time"}
		for _, field := range requiredFields {
			if _, ok := parsed[field]; !ok {
				t.Errorf("expected field %q in JSON output", field)
			}
		}
	})

	t.Run("includes meta when set", func(t *testing.T) {
		pd := NewBadRequest("test").WithMeta("field", "email")
		data := pd.JSON()

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		meta, ok := parsed["meta"].(map[string]interface{})
		if !ok {
			t.Fatal("expected meta to be present in JSON")
		}
		if meta["field"] != "email" {
			t.Errorf("expected meta.field='email', got %v", meta["field"])
		}
	})

	t.Run("omits meta when not set", func(t *testing.T) {
		pd := NewBadRequest("test")
		data := pd.JSON()

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if _, ok := parsed["meta"]; ok {
			t.Error("expected meta to be omitted when nil")
		}
	})

	t.Run("omits instance when empty", func(t *testing.T) {
		pd := NewBadRequest("test")
		data := pd.JSON()

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if _, ok := parsed["instance"]; ok {
			t.Error("expected instance to be omitted when empty")
		}
	})

	t.Run("includes instance when set", func(t *testing.T) {
		pd := NewBadRequest("test").WithInstance("/api/v1/users/123")
		data := pd.JSON()

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if parsed["instance"] != "/api/v1/users/123" {
			t.Errorf("expected instance '/api/v1/users/123', got %v", parsed["instance"])
		}
	})

	t.Run("does not include internal err field", func(t *testing.T) {
		pd := NewBadRequest("test").WithError(errors.New("internal"))
		data := pd.JSON()

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if _, ok := parsed["err"]; ok {
			t.Error("expected internal err field to be excluded from JSON")
		}
	})
}

// TestIsProblemDetail tests the IsProblemDetail function
func TestIsProblemDetail(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "ProblemDetail error",
			err:      NewBadRequest("test"),
			expected: true,
		},
		{
			name:     "wrapped ProblemDetail",
			err:      fmt.Errorf("outer: %w", NewBadRequest("inner")),
			expected: true,
		},
		{
			name:     "plain error",
			err:      errors.New("just a regular error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsProblemDetail(tt.err)
			if result != tt.expected {
				t.Errorf("expected IsProblemDetail=%v, got %v", tt.expected, result)
			}
		})
	}
}

// TestSetErrorDocsURL tests the SetErrorDocsURL function
func TestSetErrorDocsURL(t *testing.T) {
	// Save original and restore after test
	original := errorDocsBaseURL
	t.Cleanup(func() {
		errorDocsBaseURL = original
	})

	t.Run("changes base URL in new errors", func(t *testing.T) {
		SetErrorDocsURL("https://docs.myapp.com/errors")
		pd := NewBadRequest("test")

		if !strings.HasPrefix(pd.Type, "https://docs.myapp.com/errors/") {
			t.Errorf("expected type to start with custom URL, got %q", pd.Type)
		}
	})

	t.Run("empty URL produces error type with slash prefix", func(t *testing.T) {
		SetErrorDocsURL("")
		pd := NewBadRequest("test")

		expected := fmt.Sprintf("/%s", CodeBadRequest)
		if pd.Type != expected {
			t.Errorf("expected type %q, got %q", expected, pd.Type)
		}
	})
}

// TestLegacyErrorType tests the legacy Error struct
func TestLegacyErrorType(t *testing.T) {
	tests := []struct {
		name     string
		errType  ErrorType
		message  string
		code     string
		expected string
	}{
		{
			name:     "not found error",
			errType:  ErrorTypeNotFound,
			message:  "user not found",
			code:     "USER_001",
			expected: "user not found",
		},
		{
			name:     "bad request error",
			errType:  ErrorTypeBadRequest,
			message:  "invalid input",
			code:     "VAL_001",
			expected: "invalid input",
		},
		{
			name:     "empty message",
			errType:  ErrorTypeInternal,
			message:  "",
			code:     "INTERNAL_001",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Error{
				Type:    tt.errType,
				Message: tt.message,
				Code:    tt.code,
			}
			if e.Error() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, e.Error())
			}
		})
	}
}

// TestProblemDetailMethodChainingFull tests full method chaining with all setters
func TestProblemDetailMethodChainingFull(t *testing.T) {
	inner := errors.New("db timeout")
	pd := NewInternalError("something went wrong").
		WithError(inner).
		WithMeta("retry_after", 30).
		WithTraceID(testTraceID).
		WithInstance("/api/v1/orders/456")

	if pd.Unwrap() != inner {
		t.Error("expected wrapped error to match")
	}
	if pd.Meta["retry_after"] != 30 {
		t.Errorf("expected meta retry_after=30, got %v", pd.Meta["retry_after"])
	}
	if pd.TraceID != testTraceID {
		t.Errorf("expected trace ID %s, got %s", testTraceID, pd.TraceID)
	}
	if pd.Instance != "/api/v1/orders/456" {
		t.Errorf("expected instance '/api/v1/orders/456', got %s", pd.Instance)
	}

	// Verify JSON output includes everything
	data := pd.JSON()
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if parsed["trace_id"] != testTraceID {
		t.Errorf("expected trace_id in JSON, got %v", parsed["trace_id"])
	}
	if parsed["instance"] != "/api/v1/orders/456" {
		t.Errorf("expected instance in JSON, got %v", parsed["instance"])
	}
}

// TestProblemDetailErrorInterface verifies ProblemDetail satisfies the error interface
func TestProblemDetailErrorInterface(t *testing.T) {
	var err error = NewBadRequest("test")
	if err == nil {
		t.Fatal("expected non-nil error interface")
	}

	if err.Error() != "test" {
		t.Errorf("expected 'test', got %q", err.Error())
	}
}

// TestLegacyErrorConstants validates all ErrorType constants are non-empty
func TestLegacyErrorConstants(t *testing.T) {
	types := []ErrorType{
		ErrorTypeNotFound,
		ErrorTypeBadRequest,
		ErrorTypeUnauthorized,
		ErrorTypeForbidden,
		ErrorTypeConflict,
		ErrorTypeInternal,
		ErrorTypeServiceUnavailable,
	}

	for _, et := range types {
		if et == "" {
			t.Error("ErrorType constant should not be empty")
		}
	}
}

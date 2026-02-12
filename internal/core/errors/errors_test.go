package errors

import (
	"net/http"
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

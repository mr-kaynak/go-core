package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ErrorCode represents a unique error code for the application
type ErrorCode string

// ErrorType represents the type of error (for legacy compatibility)
type ErrorType string

const (
	ErrorTypeNotFound           ErrorType = "NOT_FOUND"
	ErrorTypeBadRequest         ErrorType = "BAD_REQUEST"
	ErrorTypeUnauthorized       ErrorType = "UNAUTHORIZED"
	ErrorTypeForbidden          ErrorType = "FORBIDDEN"
	ErrorTypeConflict           ErrorType = "CONFLICT"
	ErrorTypeInternal           ErrorType = "INTERNAL"
	ErrorTypeServiceUnavailable ErrorType = "SERVICE_UNAVAILABLE"
)

// Error represents a structured error (legacy support for gRPC)
type Error struct {
	Type    ErrorType `json:"type"`
	Message string    `json:"message"`
	Code    string    `json:"code"`
}

// Error implements the error interface
func (e *Error) Error() string {
	return e.Message
}

// Standard error codes
const (
	// Authentication & Authorization
	CodeUnauthorized        ErrorCode = "AUTH_001"
	CodeInvalidCredentials  ErrorCode = "AUTH_002"
	CodeTokenExpired        ErrorCode = "AUTH_003"
	CodeTokenInvalid        ErrorCode = "AUTH_004"
	CodeInsufficientRights  ErrorCode = "AUTH_005"
	CodeAccountLocked       ErrorCode = "AUTH_006"
	CodeAccountNotActivated ErrorCode = "AUTH_007"

	// User related
	CodeUserNotFound      ErrorCode = "USER_001"
	CodeUserAlreadyExists ErrorCode = "USER_002"
	CodeInvalidUserData   ErrorCode = "USER_003"
	CodeEmailNotVerified  ErrorCode = "USER_004"

	// Validation
	CodeValidationFailed ErrorCode = "VAL_001"
	CodeInvalidInput     ErrorCode = "VAL_002"
	CodeMissingField     ErrorCode = "VAL_003"
	CodeInvalidFormat    ErrorCode = "VAL_004"

	// Database
	CodeDatabaseError      ErrorCode = "DB_001"
	CodeRecordNotFound     ErrorCode = "DB_002"
	CodeDuplicateEntry     ErrorCode = "DB_003"
	CodeConstraintViolated ErrorCode = "DB_004"
	CodeTransactionFailed  ErrorCode = "DB_005"

	// External Services
	CodeServiceUnavailable ErrorCode = "EXT_001"
	CodeTimeout            ErrorCode = "EXT_002"
	CodeRateLimitExceeded  ErrorCode = "EXT_003"
	CodeCircuitBreakerOpen ErrorCode = "EXT_004"

	// Email
	CodeEmailSendFailed ErrorCode = "EMAIL_001"
	CodeInvalidEmail    ErrorCode = "EMAIL_002"

	// Message Queue
	CodeMessagePublishFailed ErrorCode = "MQ_001"
	CodeMessageConsumeFailed ErrorCode = "MQ_002"

	// General
	CodeInternal            ErrorCode = "INTERNAL_001"
	CodeNotFound            ErrorCode = "NOT_FOUND"
	CodeMethodNotAllowed    ErrorCode = "METHOD_NOT_ALLOWED"
	CodeConflict            ErrorCode = "CONFLICT"
	CodeBadRequest          ErrorCode = "BAD_REQUEST"
	CodeUnprocessableEntity ErrorCode = "UNPROCESSABLE_ENTITY"
)

// ProblemDetail represents an RFC 7807 problem detail
type ProblemDetail struct {
	Type     string                 `json:"type"`
	Title    string                 `json:"title"`
	Status   int                    `json:"status"`
	Detail   string                 `json:"detail"`
	Instance string                 `json:"instance,omitempty"`
	Code     ErrorCode              `json:"code"`
	TraceID  string                 `json:"trace_id,omitempty"`
	Time     time.Time              `json:"time"`
	Meta     map[string]interface{} `json:"meta,omitempty"`

	// Internal fields (not serialized)
	err   error  `json:"-"`
	stack string `json:"-"`
}

// Error implements the error interface
func (e *ProblemDetail) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Title
}

// Unwrap returns the wrapped error
func (e *ProblemDetail) Unwrap() error {
	return e.err
}

// WithError wraps an internal error
func (e *ProblemDetail) WithError(err error) *ProblemDetail {
	e.err = err
	return e
}

// WithMeta adds metadata to the error
func (e *ProblemDetail) WithMeta(key string, value interface{}) *ProblemDetail {
	if e.Meta == nil {
		e.Meta = make(map[string]interface{})
	}
	e.Meta[key] = value
	return e
}

// WithTraceID sets the trace ID for distributed tracing
func (e *ProblemDetail) WithTraceID(traceID string) *ProblemDetail {
	e.TraceID = traceID
	return e
}

// WithInstance sets the instance (URI reference) for the error
func (e *ProblemDetail) WithInstance(instance string) *ProblemDetail {
	e.Instance = instance
	return e
}

// JSON returns the JSON representation of the error
func (e *ProblemDetail) JSON() []byte {
	data, _ := json.Marshal(e)
	return data
}

// New creates a new ProblemDetail error
func New(code ErrorCode, status int, title, detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   fmt.Sprintf("https://api.go-core.com/errors/%s", code),
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code,
		Time:   time.Now().UTC(),
	}
}

// NewBadRequest creates a new bad request error
func NewBadRequest(detail string) *ProblemDetail {
	return New(CodeBadRequest, http.StatusBadRequest, "Bad Request", detail)
}

// NewUnauthorized creates a new unauthorized error
func NewUnauthorized(detail string) *ProblemDetail {
	return New(CodeUnauthorized, http.StatusUnauthorized, "Unauthorized", detail)
}

// NewForbidden creates a new forbidden error
func NewForbidden(detail string) *ProblemDetail {
	return New(CodeInsufficientRights, http.StatusForbidden, "Forbidden", detail)
}

// NewNotFound creates a new not found error
func NewNotFound(resource, identifier string) *ProblemDetail {
	return New(
		CodeNotFound,
		http.StatusNotFound,
		"Resource Not Found",
		fmt.Sprintf("%s with identifier '%s' not found", resource, identifier),
	)
}

// NewConflict creates a new conflict error
func NewConflict(detail string) *ProblemDetail {
	return New(CodeConflict, http.StatusConflict, "Conflict", detail)
}

// NewInternal creates a new internal server error
func NewInternal(detail string) *ProblemDetail {
	return New(CodeInternal, http.StatusInternalServerError, "Internal Server Error", detail)
}

// NewValidationError creates a new validation error
func NewValidationError(detail string) *ProblemDetail {
	return New(CodeValidationFailed, http.StatusBadRequest, "Validation Failed", detail)
}

// NewInternalError creates a new internal server error
func NewInternalError(detail string) *ProblemDetail {
	return New(CodeInternal, http.StatusInternalServerError, "Internal Server Error", detail)
}

// NewServiceUnavailable creates a new service unavailable error
func NewServiceUnavailable(service string) *ProblemDetail {
	return New(
		CodeServiceUnavailable,
		http.StatusServiceUnavailable,
		"Service Unavailable",
		fmt.Sprintf("The %s service is currently unavailable", service),
	)
}

// NewRateLimitExceeded creates a new rate limit exceeded error
func NewRateLimitExceeded(limit int) *ProblemDetail {
	return New(
		CodeRateLimitExceeded,
		http.StatusTooManyRequests,
		"Rate Limit Exceeded",
		fmt.Sprintf("Rate limit of %d requests exceeded", limit),
	)
}

// NewTooManyRequests creates a too many requests error with custom message
func NewTooManyRequests(detail string) *ProblemDetail {
	return New(
		CodeRateLimitExceeded,
		http.StatusTooManyRequests,
		"Too Many Requests",
		detail,
	)
}

// IsProblemDetail checks if an error is a ProblemDetail
func IsProblemDetail(err error) bool {
	_, ok := err.(*ProblemDetail)
	return ok
}

// GetProblemDetail extracts ProblemDetail from an error
func GetProblemDetail(err error) *ProblemDetail {
	if pd, ok := err.(*ProblemDetail); ok {
		return pd
	}
	return nil
}

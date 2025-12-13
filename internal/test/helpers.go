package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// TestConfig creates a test configuration
func TestConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{
			Name:    "Go-Core Test",
			Env:     "test",
			Version: "1.0.0",
			Debug:   true,
		},
		JWT: config.JWTConfig{
			Secret:        "test-secret-key-32-characters-ok",
			Expiry:        900000000000,  // 15 minutes in nanoseconds
			RefreshExpiry: 604800000000000, // 7 days in nanoseconds
			Issuer:        "go-core-test",
		},
		Email: config.EmailConfig{
			SMTPHost:     "localhost",
			SMTPPort:     1025,
			SMTPUser:     "test",
			SMTPPassword: "test",
			FromEmail:    "test@example.com",
			FromName:     "Test",
		},
		RateLimit: config.RateLimitConfig{
			PerMinute: 60,
		},
	}
}

// CreateTestUser creates a test user
func CreateTestUser(email, username, password string) *domain.User {
	user := &domain.User{
		ID:       uuid.New(),
		Email:    email,
		Username: username,
		Password: password,
		Status:   domain.UserStatusActive,
		Verified: true,
	}
	return user
}

// CreateTestUserWithDefaults creates a test user with default values
func CreateTestUserWithDefaults() *domain.User {
	return CreateTestUser("test@example.com", "testuser", "TestPassword123!")
}

// CreateTestRole creates a test role
func CreateTestRole(name string) *domain.Role {
	return &domain.Role{
		ID:          uuid.New(),
		Name:        name,
		Description: "Test role: " + name,
	}
}

// CreateTestPermission creates a test permission
func CreateTestPermission(resource, action string) *domain.Permission {
	return &domain.Permission{
		ID:       uuid.New(),
		Name:     resource + ":" + action,
		Resource: resource,
		Action:   action,
	}
}

// AssertError checks if an error matches the expected type
func AssertError(t *testing.T, err error, expected error) {
	if (err == nil) != (expected == nil) {
		t.Errorf("error mismatch: got %v, expected %v", err, expected)
	}
}

// AssertEqual checks if two values are equal
func AssertEqual(t *testing.T, got, want interface{}) {
	if got != want {
		t.Errorf("value mismatch: got %v, want %v", got, want)
	}
}

// AssertNotNil checks if a value is not nil
func AssertNotNil(t *testing.T, got interface{}) {
	if got == nil {
		t.Errorf("expected non-nil value, got nil")
	}
}

// AssertNil checks if a value is nil
func AssertNil(t *testing.T, got interface{}) {
	if got != nil {
		t.Errorf("expected nil value, got %v", got)
	}
}

// ContextWithTimeout creates a context with timeout for tests
func ContextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*1e9) // 30 seconds
}

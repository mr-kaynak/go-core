package test

import (
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
			Expiry:        900000000000,    // 15 minutes in nanoseconds
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
	return &domain.User{
		ID:       uuid.New(),
		Email:    email,
		Username: username,
		Password: password,
		Status:   domain.UserStatusActive,
		Verified: true,
	}
}

// CreateTestUserWithDefaults creates a test user with default values
func CreateTestUserWithDefaults() *domain.User {
	return CreateTestUser("test@example.com", "testuser", "TestPassword123!")
}

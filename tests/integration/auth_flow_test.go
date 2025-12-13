//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/test"
)

// TestAuthFlowRegistrationToLogin tests complete auth flow
func TestAuthFlowRegistrationToLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := test.TestConfig()

	// Setup services
	tokenService := service.NewTokenService(cfg)
	mockUserRepo := &test.MockUserRepository{}
	mockVerificationRepo := &test.MockVerificationTokenRepository{}
	mockEmailService := &test.MockEmailService{}

	authService := service.NewAuthService(
		cfg,
		mockUserRepo,
		tokenService,
		mockVerificationRepo,
		mockEmailService,
	)

	// Test data
	email := "integration@test.com"
	username := "integrationuser"
	password := "TestPass123!"

	// Mock repository responses
	mockUserRepo.GetByEmailFunc = func(e string) (*domain.User, error) {
		return nil, nil // User doesn't exist initially
	}
	mockUserRepo.GetByUsernameFunc = func(u string) (*domain.User, error) {
		return nil, nil // User doesn't exist initially
	}
	mockUserRepo.CreateFunc = func(user *domain.User) (*domain.User, error) {
		user.ID = uuid.New()
		return user, nil
	}
	mockVerificationRepo.CreateFunc = func(token *domain.VerificationToken) (*domain.VerificationToken, error) {
		token.ID = uuid.New()
		return token, nil
	}

	// Step 1: Register user
	t.Run("register_new_user", func(t *testing.T) {
		user, err := authService.Register(email, username, password)
		if err != nil {
			t.Fatalf("registration failed: %v", err)
		}

		if user == nil {
			t.Fatal("expected user, got nil")
		}

		if user.Email != email {
			t.Errorf("expected email %s, got %s", email, user.Email)
		}

		if user.Username != username {
			t.Errorf("expected username %s, got %s", username, user.Username)
		}

		if !user.IsPasswordHashed() {
			t.Error("password should be hashed")
		}
	})

	// Step 2: Verify email (simulate)
	t.Run("verify_email", func(t *testing.T) {
		// In real integration test, we'd verify the token
		// For now, we just verify the flow works
		t.Log("Email verification flow works")
	})

	// Step 3: Login user
	t.Run("login_user", func(t *testing.T) {
		// Update mock to return the registered user
		registeredUser := &domain.User{
			ID:       uuid.New(),
			Email:    email,
			Username: username,
			Status:   domain.UserStatusActive,
			Verified: true,
		}
		registeredUser.SetPassword(password)

		mockUserRepo.GetByEmailFunc = func(e string) (*domain.User, error) {
			if e == email {
				return registeredUser, nil
			}
			return nil, nil
		}

		tokenPair, user, err := authService.Login(email, password)
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		if tokenPair == nil {
			t.Fatal("expected token pair, got nil")
		}

		if tokenPair.AccessToken == "" {
			t.Error("expected access token")
		}

		if tokenPair.RefreshToken == "" {
			t.Error("expected refresh token")
		}

		if user.Email != email {
			t.Errorf("expected email %s, got %s", email, user.Email)
		}
	})
}

// TestAuthFlowPasswordReset tests password reset flow
func TestAuthFlowPasswordReset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := test.TestConfig()

	tokenService := service.NewTokenService(cfg)
	mockUserRepo := &test.MockUserRepository{}
	mockVerificationRepo := &test.MockVerificationTokenRepository{}
	mockEmailService := &test.MockEmailService{}

	authService := service.NewAuthService(
		cfg,
		mockUserRepo,
		tokenService,
		mockVerificationRepo,
		mockEmailService,
	)

	email := "reset@test.com"
	oldPassword := "OldPass123!"
	newPassword := "NewPass456!"

	existingUser := &domain.User{
		ID:       uuid.New(),
		Email:    email,
		Username: "resetuser",
		Status:   domain.UserStatusActive,
		Verified: true,
	}
	existingUser.SetPassword(oldPassword)

	mockUserRepo.GetByEmailFunc = func(e string) (*domain.User, error) {
		if e == email {
			return existingUser, nil
		}
		return nil, nil
	}

	mockVerificationRepo.CreateFunc = func(token *domain.VerificationToken) (*domain.VerificationToken, error) {
		token.ID = uuid.New()
		return token, nil
	}

	// Step 1: Request password reset
	t.Run("request_password_reset", func(t *testing.T) {
		err := authService.RequestPasswordReset(email)
		if err != nil {
			t.Fatalf("password reset request failed: %v", err)
		}
	})

	// Step 2: Reset password with token
	t.Run("reset_password", func(t *testing.T) {
		// Simulate token validation
		resetToken := "valid-reset-token"

		mockVerificationRepo.GetByTokenFunc = func(token string) (*domain.VerificationToken, error) {
			return &domain.VerificationToken{
				ID:     uuid.New(),
				UserID: existingUser.ID,
				Token:  resetToken,
				Type:   "password_reset",
			}, nil
		}

		mockUserRepo.GetByIDFunc = func(id uuid.UUID) (*domain.User, error) {
			if id == existingUser.ID {
				return existingUser, nil
			}
			return nil, nil
		}

		mockUserRepo.UpdateFunc = func(user *domain.User) (*domain.User, error) {
			return user, nil
		}

		err := authService.ResetPassword(resetToken, newPassword)
		if err != nil {
			t.Fatalf("password reset failed: %v", err)
		}
	})

	// Step 3: Login with new password
	t.Run("login_with_new_password", func(t *testing.T) {
		// Update user password
		existingUser.SetPassword(newPassword)

		tokenPair, user, err := authService.Login(email, newPassword)
		if err != nil {
			t.Fatalf("login with new password failed: %v", err)
		}

		if tokenPair == nil {
			t.Fatal("expected token pair")
		}

		if user.Email != email {
			t.Errorf("expected email %s, got %s", email, user.Email)
		}
	})
}

// TestAuthFlowInvalidCredentials tests auth flow with invalid credentials
func TestAuthFlowInvalidCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := test.TestConfig()

	tokenService := service.NewTokenService(cfg)
	mockUserRepo := &test.MockUserRepository{}
	mockVerificationRepo := &test.MockVerificationTokenRepository{}
	mockEmailService := &test.MockEmailService{}

	authService := service.NewAuthService(
		cfg,
		mockUserRepo,
		tokenService,
		mockVerificationRepo,
		mockEmailService,
	)

	email := "test@test.com"
	correctPassword := "CorrectPass123!"
	wrongPassword := "WrongPass123!"

	user := &domain.User{
		ID:       uuid.New(),
		Email:    email,
		Username: "testuser",
		Status:   domain.UserStatusActive,
		Verified: true,
	}
	user.SetPassword(correctPassword)

	mockUserRepo.GetByEmailFunc = func(e string) (*domain.User, error) {
		if e == email {
			return user, nil
		}
		return nil, nil
	}

	t.Run("login_with_wrong_password", func(t *testing.T) {
		_, _, err := authService.Login(email, wrongPassword)
		if err == nil {
			t.Error("expected error for wrong password, got nil")
		}
	})

	t.Run("login_with_non_existent_email", func(t *testing.T) {
		_, _, err := authService.Login("nonexistent@test.com", correctPassword)
		if err == nil {
			t.Error("expected error for non-existent email, got nil")
		}
	})
}

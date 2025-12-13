package domain

import (
	"testing"

	"github.com/google/uuid"
)

// TestUserPasswordHashing tests password hashing functionality
func TestUserPasswordHashing(t *testing.T) {
	tests := []struct {
		name          string
		password      string
		shouldSucceed bool
	}{
		{
			name:          "valid password hashing",
			password:      "TestPassword123!@#",
			shouldSucceed: true,
		},
		{
			name:          "single character password",
			password:      "a",
			shouldSucceed: true,
		},
		{
			name:          "password near bcrypt limit",
			password:      "VeryLongPassword1234567890!@#$%^&*()_+-=[]{}|;:abcdefgh",
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{
				ID:       uuid.New(),
				Email:    "test@example.com",
				Username: "testuser",
				Password: tt.password,
			}

			err := user.HashPassword()

			if tt.shouldSucceed && err != nil {
				t.Errorf("expected success, got error: %v", err)
			}

			if !tt.shouldSucceed && err == nil {
				t.Errorf("expected error, but got success")
			}

			if tt.shouldSucceed {
				// Verify hash is different from original password
				if user.Password == tt.password {
					t.Errorf("password was not hashed")
				}

				// Verify hash length
				if len(user.Password) != 60 {
					t.Errorf("invalid hash length: expected 60, got %d", len(user.Password))
				}

				// Verify bcrypt format
				if !user.IsPasswordHashed() {
					t.Errorf("hash not recognized as valid bcrypt hash")
				}
			}
		})
	}
}

// TestUserComparePassword tests password comparison
func TestUserComparePassword(t *testing.T) {
	plainPassword := "TestPassword123!@#"
	user := &User{
		ID:       uuid.New(),
		Email:    "test@example.com",
		Username: "testuser",
		Password: plainPassword,
	}

	// Hash the password first
	if err := user.HashPassword(); err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	tests := []struct {
		name          string
		inputPassword string
		shouldMatch   bool
	}{
		{
			name:          "correct password",
			inputPassword: plainPassword,
			shouldMatch:   true,
		},
		{
			name:          "incorrect password",
			inputPassword: "WrongPassword123!@#",
			shouldMatch:   false,
		},
		{
			name:          "empty password",
			inputPassword: "",
			shouldMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := user.ComparePassword(tt.inputPassword)

			if tt.shouldMatch && err != nil {
				t.Errorf("expected password match, got error: %v", err)
			}

			if !tt.shouldMatch && err == nil {
				t.Errorf("expected password mismatch, but passwords matched")
			}
		})
	}
}

// TestIsPasswordHashed tests bcrypt hash detection
func TestIsPasswordHashed(t *testing.T) {
	tests := []struct {
		name     string
		password string
		isHashed bool
	}{
		{
			name:     "valid bcrypt hash (2a)",
			password: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36gI957e",
			isHashed: true,
		},
		{
			name:     "valid bcrypt hash (2b)",
			password: "$2b$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36gI957e",
			isHashed: true,
		},
		{
			name:     "valid bcrypt hash (2x)",
			password: "$2x$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36gI957e",
			isHashed: true,
		},
		{
			name:     "plain text password",
			password: "TestPassword123!@#",
			isHashed: false,
		},
		{
			name:     "invalid hash format",
			password: "$2c$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36gI957e",
			isHashed: false,
		},
		{
			name:     "wrong length",
			password: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36gI957",
			isHashed: false,
		},
		{
			name:     "empty string",
			password: "",
			isHashed: false,
		},
		{
			name:     "too short",
			password: "$2a$10$",
			isHashed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{Password: tt.password}
			result := user.IsPasswordHashed()

			if result != tt.isHashed {
				t.Errorf("expected %v, got %v for password: %s", tt.isHashed, result, tt.password)
			}
		})
	}
}

// TestSetPassword tests SetPassword method
func TestSetPassword(t *testing.T) {
	user := &User{
		ID:       uuid.New(),
		Email:    "test@example.com",
		Username: "testuser",
	}

	newPassword := "NewPassword123!@#"
	if err := user.SetPassword(newPassword); err != nil {
		t.Fatalf("failed to set password: %v", err)
	}

	// Verify password was hashed
	if !user.IsPasswordHashed() {
		t.Errorf("password was not hashed after SetPassword")
	}

	// Verify new password can be compared
	if err := user.ComparePassword(newPassword); err != nil {
		t.Errorf("failed to compare password: %v", err)
	}
}

// TestUserIsActive tests IsActive method
func TestUserIsActive(t *testing.T) {
	tests := []struct {
		name     string
		status   UserStatus
		verified bool
		isActive bool
	}{
		{
			name:     "active and verified",
			status:   UserStatusActive,
			verified: true,
			isActive: true,
		},
		{
			name:     "active but not verified",
			status:   UserStatusActive,
			verified: false,
			isActive: false,
		},
		{
			name:     "verified but inactive",
			status:   UserStatusInactive,
			verified: true,
			isActive: false,
		},
		{
			name:     "locked",
			status:   UserStatusLocked,
			verified: true,
			isActive: false,
		},
		{
			name:     "pending",
			status:   UserStatusPending,
			verified: false,
			isActive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{
				Status:   tt.status,
				Verified: tt.verified,
			}

			result := user.IsActive()
			if result != tt.isActive {
				t.Errorf("expected %v, got %v", tt.isActive, result)
			}
		})
	}
}

// TestUserGetFullName tests GetFullName method
func TestUserGetFullName(t *testing.T) {
	tests := []struct {
		name         string
		firstName    string
		lastName     string
		username     string
		expectedName string
	}{
		{
			name:         "both names present",
			firstName:    "John",
			lastName:     "Doe",
			username:     "johndoe",
			expectedName: "John Doe",
		},
		{
			name:         "only first name",
			firstName:    "John",
			lastName:     "",
			username:     "johndoe",
			expectedName: "John",
		},
		{
			name:         "only last name",
			firstName:    "",
			lastName:     "Doe",
			username:     "johndoe",
			expectedName: "Doe",
		},
		{
			name:         "no names, use username",
			firstName:    "",
			lastName:     "",
			username:     "johndoe",
			expectedName: "johndoe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{
				FirstName: tt.firstName,
				LastName:  tt.lastName,
				Username:  tt.username,
			}

			result := user.GetFullName()
			if result != tt.expectedName {
				t.Errorf("expected %q, got %q", tt.expectedName, result)
			}
		})
	}
}

// TestUserHasRole tests HasRole method
func TestUserHasRole(t *testing.T) {
	adminRole := &Role{ID: uuid.New(), Name: "admin"}
	userRole := &Role{ID: uuid.New(), Name: "user"}

	user := &User{
		ID:    uuid.New(),
		Email: "test@example.com",
		Roles: []Role{*adminRole, *userRole},
	}

	tests := []struct {
		name     string
		roleName string
		hasRole  bool
	}{
		{
			name:     "has admin role",
			roleName: "admin",
			hasRole:  true,
		},
		{
			name:     "has user role",
			roleName: "user",
			hasRole:  true,
		},
		{
			name:     "does not have moderator role",
			roleName: "moderator",
			hasRole:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := user.HasRole(tt.roleName)
			if result != tt.hasRole {
				t.Errorf("expected %v, got %v", tt.hasRole, result)
			}
		})
	}
}

// TestUserHasPermission tests HasPermission method
func TestUserHasPermission(t *testing.T) {
	readPermission := &Permission{
		ID:       uuid.New(),
		Name:     "posts.read",
		Category: "posts",
	}
	writePermission := &Permission{
		ID:       uuid.New(),
		Name:     "posts.write",
		Category: "posts",
	}

	role := &Role{
		ID:          uuid.New(),
		Name:        "author",
		Permissions: []Permission{*readPermission, *writePermission},
	}

	user := &User{
		ID:    uuid.New(),
		Email: "test@example.com",
		Roles: []Role{*role},
	}

	tests := []struct {
		name            string
		permissionName  string
		hasPermission   bool
	}{
		{
			name:            "has read permission",
			permissionName:  "posts.read",
			hasPermission:   true,
		},
		{
			name:            "has write permission",
			permissionName:  "posts.write",
			hasPermission:   true,
		},
		{
			name:            "does not have delete permission",
			permissionName:  "posts.delete",
			hasPermission:   false,
		},
		{
			name:            "different resource",
			permissionName:  "comments.read",
			hasPermission:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := user.HasPermission(tt.permissionName)
			if result != tt.hasPermission {
				t.Errorf("expected %v, got %v", tt.hasPermission, result)
			}
		})
	}
}

// TestBeforeCreate tests the BeforeCreate hook
func TestBeforeCreate(t *testing.T) {
	t.Run("generates UUID if not set", func(t *testing.T) {
		user := &User{
			Email:    "test@example.com",
			Username: "testuser",
			Password: "TestPassword123!",
		}

		if user.ID != uuid.Nil {
			t.Errorf("expected nil UUID before BeforeCreate")
		}

		// Simulate GORM hook call (would normally be called by GORM)
		if err := user.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate failed: %v", err)
		}

		if user.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		expectedID := uuid.New()
		user := &User{
			ID:       expectedID,
			Email:    "test@example.com",
			Username: "testuser",
			Password: "TestPassword123!",
		}

		if err := user.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate failed: %v", err)
		}

		if user.ID != expectedID {
			t.Errorf("expected UUID to be preserved: %v, got %v", expectedID, user.ID)
		}
	})

	t.Run("hashes plain password", func(t *testing.T) {
		plainPassword := "TestPassword123!"
		user := &User{
			Email:    "test@example.com",
			Username: "testuser",
			Password: plainPassword,
		}

		if err := user.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate failed: %v", err)
		}

		if user.Password == plainPassword {
			t.Errorf("password was not hashed")
		}

		if !user.IsPasswordHashed() {
			t.Errorf("password is not a valid bcrypt hash")
		}
	})
}

// Benchmark tests for performance-critical operations
func BenchmarkHashPassword(b *testing.B) {
	user := &User{
		Password: "TestPassword123!@#$%^&*()",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user.HashPassword()
	}
}

func BenchmarkComparePassword(b *testing.B) {
	plainPassword := "TestPassword123!@#$%^&*()"
	user := &User{
		Password: plainPassword,
	}
	user.HashPassword()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user.ComparePassword(plainPassword)
	}
}

func BenchmarkIsPasswordHashed(b *testing.B) {
	user := &User{
		Password: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36gI957e",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user.IsPasswordHashed()
	}
}

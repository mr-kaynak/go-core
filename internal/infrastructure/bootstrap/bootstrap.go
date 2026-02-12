package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

// Bootstrap initializes system with default data
type Bootstrap struct {
	db            *gorm.DB
	userRepo      repository.UserRepository
	casbinService *authorization.CasbinService
	logger        *logger.Logger
}

// NewBootstrap creates a new bootstrap instance
func NewBootstrap(db *gorm.DB, userRepo repository.UserRepository, casbinService *authorization.CasbinService) *Bootstrap {
	return &Bootstrap{
		db:            db,
		userRepo:      userRepo,
		casbinService: casbinService,
		logger:        logger.Get().WithFields(logger.Fields{"service": "bootstrap"}),
	}
}

// Run executes bootstrap initialization inside a single database transaction
// so that a failure in any step rolls back all changes.
func (b *Bootstrap) Run() error {
	b.logger.Info("Starting bootstrap initialization")

	if err := b.db.Transaction(func(tx *gorm.DB) error {
		// Create default roles with hierarchy
		if err := b.createDefaultRoles(tx); err != nil {
			b.logger.Error("Failed to create default roles", "error", err)
			return err
		}

		// Create default permissions
		if err := b.createDefaultPermissions(tx); err != nil {
			b.logger.Error("Failed to create default permissions", "error", err)
			return err
		}

		// Assign permissions to system_admin role
		if err := b.assignPermissionsToSystemAdmin(tx); err != nil {
			b.logger.Error("Failed to assign permissions to system_admin", "error", err)
			return err
		}

		// Create initial system admin user
		if err := b.createSystemAdminUser(tx); err != nil {
			b.logger.Error("Failed to create system admin user", "error", err)
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	b.logger.Info("Bootstrap initialization completed successfully")
	return nil
}

// createDefaultRoles creates role hierarchy: system_admin > admin > user
func (b *Bootstrap) createDefaultRoles(tx *gorm.DB) error {
	b.logger.Info("Creating default roles with hierarchy")

	// Define default roles
	roles := []domain.Role{
		{
			ID:          uuid.New(),
			Name:        "system_admin",
			Description: "System administrator - full control",
		},
		{
			ID:          uuid.New(),
			Name:        "admin",
			Description: "Administrator - can manage users and basic resources",
		},
		{
			ID:          uuid.New(),
			Name:        "user",
			Description: "Regular user - basic access",
		},
	}

	// Check if roles already exist
	for i := range roles {
		var count int64
		tx.Model(&domain.Role{}).Where("name = ?", roles[i].Name).Count(&count)
		if count > 0 {
			b.logger.Info("Role already exists", "role", roles[i].Name)
			continue
		}

		// Create role
		if err := tx.Create(&roles[i]).Error; err != nil {
			b.logger.Error("Failed to create role", "role", roles[i].Name, "error", err)
			return err
		}

		b.logger.Info("Role created", "role", roles[i].Name)

		// Add Casbin role inheritance
		// system_admin inherits from admin, admin inherits from user
		switch roles[i].Name {
		case "admin":
			// admin inherits from user
			if err := b.casbinService.AddRoleInheritance("admin", "user"); err != nil {
				b.logger.Warn("Failed to add admin -> user inheritance", "error", err)
			}
		case "system_admin":
			// system_admin inherits from admin
			if err := b.casbinService.AddRoleInheritance("system_admin", "admin"); err != nil {
				b.logger.Warn("Failed to add system_admin -> admin inheritance", "error", err)
			}
		}
	}

	return nil
}

// generateSecurePassword generates a cryptographically secure random password
func (b *Bootstrap) generateSecurePassword() (string, error) {
	const passwordBytes = 32
	// Generate random data for password
	bytes := make([]byte, passwordBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random password: %w", err)
	}

	// Encode to base64 for a readable password
	// This will create a password of approximately 44 characters
	password := base64.URLEncoding.EncodeToString(bytes)

	// Ensure it meets complexity requirements by adding special chars
	// The base64 encoding already includes letters and numbers
	return password[:passwordBytes] + "!Aa1", nil // Ensures uppercase, lowercase, number, and special char
}

// createSystemAdminUser creates the initial system admin user
func (b *Bootstrap) createSystemAdminUser(tx *gorm.DB) error {
	email := "admin@system.local"
	username := "system_admin"

	// Check if admin already exists
	existingUser, err := b.userRepo.GetByEmail(email)
	if err == nil && existingUser != nil {
		b.logger.Info("System admin user already exists", "email", email)
		return nil
	}

	// Generate secure random password
	password, err := b.generateSecurePassword()
	if err != nil {
		b.logger.Error("Failed to generate secure password", "error", err)
		return fmt.Errorf("failed to generate password: %w", err)
	}

	b.logger.Info("Creating system admin user", "email", email, "username", username)
	b.logger.Info("===========================================")
	b.logger.Info("SYSTEM ADMIN INITIAL PASSWORD (SAVE THIS!):")
	b.logger.Info(password)
	b.logger.Info("===========================================")
	b.logger.Info("This password MUST be changed on first login")

	// Create system admin user
	user := &domain.User{
		ID:        uuid.New(),
		Email:     email,
		Username:  username,
		Password:  password, // Generated secure password
		FirstName: "System",
		LastName:  "Admin",
		Status:    domain.UserStatusActive,
		Verified:  true, // Pre-verified
		Metadata:  make(domain.Metadata),
	}

	// Save user (password will be hashed in BeforeCreate hook)
	b.logger.Debug("Attempting to create user", "email", email, "username", username)
	if err := b.userRepo.WithTx(tx).Create(user); err != nil {
		b.logger.Error("Failed to create system admin user", "error", err)
		return err
	}
	b.logger.Debug("User created in repository", "user_id", user.ID, "email", email)

	b.logger.Info("System admin user created", "email", email, "user_id", user.ID, "note", "Password must be changed on first login")

	// Get system_admin role
	var systemAdminRole domain.Role
	if err := tx.Where("name = ?", "system_admin").First(&systemAdminRole).Error; err != nil {
		b.logger.Error("Failed to find system_admin role", "error", err)
		return err
	}

	// Assign system_admin role to user
	if err := tx.Model(user).Association("Roles").Append(&systemAdminRole); err != nil {
		b.logger.Error("Failed to assign system_admin role", "error", err)
		return err
	}

	// Add Casbin role for user
	if err := b.casbinService.AddRoleForUser(user.ID, "system_admin", authorization.DomainDefault); err != nil {
		b.logger.Error("Failed to add Casbin role", "error", err)
		return err
	}

	b.logger.Info("System admin user initialized successfully",
		"email", email,
		"user_id", user.ID,
		"note", "Initial password displayed above - MUST be changed on first login",
	)

	return nil
}

// createDefaultPermissions creates system permissions
func (b *Bootstrap) createDefaultPermissions(tx *gorm.DB) error {
	b.logger.Info("Creating default permissions")

	permissions := []domain.Permission{
		// User permissions
		{Name: "users.view", Category: "user", Description: "View users"},
		{Name: "users.create", Category: "user", Description: "Create new users"},
		{Name: "users.update", Category: "user", Description: "Update users"},
		{Name: "users.delete", Category: "user", Description: "Delete users"},

		// Role permissions
		{Name: "roles.view", Category: "role", Description: "View roles"},
		{Name: "roles.create", Category: "role", Description: "Create new roles"},
		{Name: "roles.update", Category: "role", Description: "Update roles"},
		{Name: "roles.delete", Category: "role", Description: "Delete roles"},

		// Admin permissions
		{Name: "admin.access", Category: "admin", Description: "Access admin panel"},
		{Name: "admin.manage", Category: "admin", Description: "Manage system"},
	}

	for i := range permissions {
		var count int64
		tx.Model(&domain.Permission{}).Where("name = ? AND deleted_at IS NULL", permissions[i].Name).Count(&count)
		if count > 0 {
			b.logger.Debug("Permission already exists", "name", permissions[i].Name)
			continue
		}

		if err := tx.Create(&permissions[i]).Error; err != nil {
			b.logger.Error("Failed to create permission", "name", permissions[i].Name, "error", err)
			return err
		}
		b.logger.Debug("Created permission", "name", permissions[i].Name)
	}

	return nil
}

// assignPermissionsToSystemAdmin assigns all permissions to system_admin role
func (b *Bootstrap) assignPermissionsToSystemAdmin(tx *gorm.DB) error {
	b.logger.Info("Assigning permissions to system_admin role")

	var systemAdminRole domain.Role
	if err := tx.Where("name = ?", "system_admin").First(&systemAdminRole).Error; err != nil {
		b.logger.Error("Failed to find system_admin role", "error", err)
		return err
	}

	var permissions []domain.Permission
	if err := tx.Find(&permissions).Error; err != nil {
		b.logger.Error("Failed to fetch permissions", "error", err)
		return err
	}

	for i := range permissions {
		var count int64
		tx.Model(&domain.RolePermission{}).Where("role_id = ? AND permission_id = ?", systemAdminRole.ID, permissions[i].ID).Count(&count)
		if count > 0 {
			b.logger.Debug("Permission already assigned to system_admin", "permission", permissions[i].Name)
			continue
		}

		if err := tx.Create(&domain.RolePermission{
			RoleID:       systemAdminRole.ID,
			PermissionID: permissions[i].ID,
		}).Error; err != nil {
			b.logger.Error("Failed to assign permission to system_admin", "permission", permissions[i].Name, "error", err)
			return err
		}
		b.logger.Debug("Assigned permission to system_admin", "permission", permissions[i].Name)
	}

	return nil
}

// LogSystemAdminCredentials logs the system admin credentials for initial setup
func LogSystemAdminCredentials() string {
	return `
================================================================================
SYSTEM ADMIN CREDENTIALS - SAVE THESE SECURELY
================================================================================
Email:    admin@system.local
Username: system_admin
Password: [GENERATED - CHECK LOGS ABOVE]

⚠️  IMPORTANT:
1. The password is randomly generated and shown in logs above
2. Change this password immediately on first login
3. Store credentials securely
4. Delete logs after saving credentials

To login:
POST /api/v1/auth/login
{
  "email": "admin@system.local",
  "password": "[USE_GENERATED_PASSWORD_FROM_LOGS]"
}

Then update password:
POST /api/v1/auth/change-password
{
  "old_password": "TempPassword123!",
  "new_password": "YOUR_SECURE_PASSWORD"
}
================================================================================
`
}

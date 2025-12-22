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
	db             *gorm.DB
	userRepo       repository.UserRepository
	casbinService  *authorization.CasbinService
	logger         *logger.Logger
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

// Run executes bootstrap initialization
func (b *Bootstrap) Run() error {
	b.logger.Info("Starting bootstrap initialization")

	// Create default roles with hierarchy
	if err := b.createDefaultRoles(); err != nil {
		b.logger.Error("Failed to create default roles", "error", err)
		return err
	}

	// Create default permissions
	if err := b.createDefaultPermissions(); err != nil {
		b.logger.Error("Failed to create default permissions", "error", err)
		return err
	}

	// Assign permissions to system_admin role
	if err := b.assignPermissionsToSystemAdmin(); err != nil {
		b.logger.Error("Failed to assign permissions to system_admin", "error", err)
		return err
	}

	// Create initial system admin user
	if err := b.createSystemAdminUser(); err != nil {
		b.logger.Error("Failed to create system admin user", "error", err)
		return err
	}

	b.logger.Info("Bootstrap initialization completed successfully")
	return nil
}

// createDefaultRoles creates role hierarchy: system_admin > admin > user
func (b *Bootstrap) createDefaultRoles() error {
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
	for _, role := range roles {
		var count int64
		b.db.Model(&domain.Role{}).Where("name = ?", role.Name).Count(&count)
		if count > 0 {
			b.logger.Info("Role already exists", "role", role.Name)
			continue
		}

		// Create role
		if err := b.db.Create(&role).Error; err != nil {
			b.logger.Error("Failed to create role", "role", role.Name, "error", err)
			return err
		}

		b.logger.Info("Role created", "role", role.Name)

		// Add Casbin role inheritance
		// system_admin inherits from admin, admin inherits from user
		if role.Name == "admin" {
			// admin inherits from user
			if err := b.casbinService.AddRoleInheritance("admin", "user"); err != nil {
				b.logger.Warn("Failed to add admin -> user inheritance", "error", err)
			}
		} else if role.Name == "system_admin" {
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
	// Generate 32 bytes of random data
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random password: %w", err)
	}

	// Encode to base64 for a readable password
	// This will create a password of approximately 44 characters
	password := base64.URLEncoding.EncodeToString(bytes)

	// Ensure it meets complexity requirements by adding special chars
	// The base64 encoding already includes letters and numbers
	return password[:32] + "!Aa1", nil // Ensures uppercase, lowercase, number, and special char
}

// createSystemAdminUser creates the initial system admin user
func (b *Bootstrap) createSystemAdminUser() error {
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
	if err := b.userRepo.Create(user); err != nil {
		b.logger.Error("Failed to create system admin user", "error", err)
		return err
	}
	b.logger.Debug("User created in repository", "user_id", user.ID, "email", email)

	b.logger.Info("System admin user created", "email", email, "user_id", user.ID, "note", "Password must be changed on first login")

	// Get system_admin role
	var systemAdminRole domain.Role
	if err := b.db.Where("name = ?", "system_admin").First(&systemAdminRole).Error; err != nil {
		b.logger.Error("Failed to find system_admin role", "error", err)
		return err
	}

	// Assign system_admin role to user
	if err := b.db.Model(user).Association("Roles").Append(&systemAdminRole); err != nil {
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
func (b *Bootstrap) createDefaultPermissions() error {
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

	for _, perm := range permissions {
		var count int64
		b.db.Model(&domain.Permission{}).Where("name = ? AND deleted_at IS NULL", perm.Name).Count(&count)
		if count > 0 {
			b.logger.Debug("Permission already exists", "name", perm.Name)
			continue
		}

		if err := b.db.Create(&perm).Error; err != nil {
			b.logger.Error("Failed to create permission", "name", perm.Name, "error", err)
			return err
		}
		b.logger.Debug("Created permission", "name", perm.Name)
	}

	return nil
}

// assignPermissionsToSystemAdmin assigns all permissions to system_admin role
func (b *Bootstrap) assignPermissionsToSystemAdmin() error {
	b.logger.Info("Assigning permissions to system_admin role")

	var systemAdminRole domain.Role
	if err := b.db.Where("name = ?", "system_admin").First(&systemAdminRole).Error; err != nil {
		b.logger.Error("Failed to find system_admin role", "error", err)
		return err
	}

	var permissions []domain.Permission
	if err := b.db.Find(&permissions).Error; err != nil {
		b.logger.Error("Failed to fetch permissions", "error", err)
		return err
	}

	for _, perm := range permissions {
		var count int64
		b.db.Model(&domain.RolePermission{}).Where("role_id = ? AND permission_id = ?", systemAdminRole.ID, perm.ID).Count(&count)
		if count > 0 {
			b.logger.Debug("Permission already assigned to system_admin", "permission", perm.Name)
			continue
		}

		if err := b.db.Create(&domain.RolePermission{
			RoleID:       systemAdminRole.ID,
			PermissionID: perm.ID,
		}).Error; err != nil {
			b.logger.Error("Failed to assign permission to system_admin", "permission", perm.Name, "error", err)
			return err
		}
		b.logger.Debug("Assigned permission to system_admin", "permission", perm.Name)
	}

	return nil
}

// LogSystemAdminCredentials logs the system admin credentials for initial setup
func LogSystemAdminCredentials() string {
	return fmt.Sprintf(`
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
`)
}

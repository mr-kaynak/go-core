package bootstrap

import (
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

	b.logger.Info("Creating system admin user", "email", email, "username", username)

	// Create system admin user
	user := &domain.User{
		ID:        uuid.New(),
		Email:     email,
		Username:  username,
		Password:  "TempPassword123!", // MUST be changed on first login
		FirstName: "System",
		LastName:  "Admin",
		Status:    domain.UserStatusActive,
		Verified:  true, // Pre-verified
		Metadata:  make(domain.Metadata),
	}

	// Save user (password will be hashed in BeforeCreate hook)
	if err := b.userRepo.Create(user); err != nil {
		b.logger.Error("Failed to create system admin user", "error", err)
		return err
	}

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
		"note", "Credentials logged - email: admin@system.local, temp password in logs",
	)

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
Temp Password: TempPassword123!

⚠️  IMPORTANT:
1. Change this password immediately on first login
2. Store credentials securely
3. Delete this message after saving credentials

To login:
POST /api/v1/auth/login
{
  "email": "admin@system.local",
  "password": "TempPassword123!"
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

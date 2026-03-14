package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

		// Assign default permissions to admin and user roles
		if err := b.assignDefaultRolePermissions(tx); err != nil {
			b.logger.Error("Failed to assign default role permissions", "error", err)
			return err
		}

		// Sync all role-permission assignments to Casbin
		if err := b.syncPermissionsToCasbin(tx); err != nil {
			b.logger.Error("Failed to sync permissions to Casbin", "error", err)
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
			if err := b.casbinService.AddRoleInheritance("role:admin", "role:user"); err != nil {
				b.logger.Warn("Failed to add admin -> user inheritance", "error", err)
			}
		case "system_admin":
			// system_admin inherits from admin
			if err := b.casbinService.AddRoleInheritance("role:system_admin", "role:admin"); err != nil {
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

	// Check if admin already exists (within the transaction to prevent TOCTOU race)
	existingUser, err := b.userRepo.WithTx(tx).GetByEmail(email)
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
	fmt.Println("===========================================")
	fmt.Println("SYSTEM ADMIN INITIAL PASSWORD (SAVE THIS!):")
	fmt.Println(password)
	fmt.Println("===========================================")
	fmt.Println("This password MUST be changed on first login")

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

	// Create default notification preferences for admin user
	var prefCount int64
	tx.Model(&notificationDomain.NotificationPreference{}).Where("user_id = ?", user.ID).Count(&prefCount)
	if prefCount == 0 {
		if err := tx.Create(&notificationDomain.NotificationPreference{
			UserID:       user.ID,
			EmailEnabled: true,
			InAppEnabled: true,
			Language:     "en",
		}).Error; err != nil {
			b.logger.Warn("Failed to create admin notification preferences", "error", err)
		}
	}

	b.logger.Info("System admin user initialized successfully",
		"email", email,
		"user_id", user.ID,
		"note", "Initial password displayed above - MUST be changed on first login",
	)

	return nil
}

// createDefaultPermissions creates system permissions from the mapping registry.
func (b *Bootstrap) createDefaultPermissions(tx *gorm.DB) error {
	b.logger.Info("Creating default permissions")

	mappings := authorization.GetAllMappings()
	for name := range mappings {
		var count int64
		tx.Model(&domain.Permission{}).Where("name = ? AND deleted_at IS NULL", name).Count(&count)
		if count > 0 {
			b.logger.Debug("Permission already exists", "name", name)
			continue
		}

		permission := domain.Permission{
			Name:        name,
			Description: generateDescription(name),
			Category:    extractCategory(name),
		}

		if err := tx.Create(&permission).Error; err != nil {
			b.logger.Error("Failed to create permission", "name", name, "error", err)
			return err
		}
		b.logger.Debug("Created permission", "name", name)
	}

	return nil
}

// categoryOverrides handles irregular plurals and special cases that naive
// suffix-stripping would break (e.g. "status" → "statu").
var categoryOverrides = map[string]string{
	"permissions":   "permission",
	"notifications": "notification",
	"templates":     "template",
	"users":         "user",
	"roles":         "role",
}

// extractCategory derives the category from a permission name (e.g. "users.view" → "user").
func extractCategory(name string) string {
	prefix := name
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		prefix = name[:idx]
	}
	if override, ok := categoryOverrides[prefix]; ok {
		return override
	}
	return prefix
}

// titleCaser is reused across calls to avoid repeated allocation.
var titleCaser = cases.Title(language.English)

// generateDescription creates a human-readable description from a permission name.
func generateDescription(name string) string {
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		resource := name[:idx]
		action := name[idx+1:]
		return titleCaser.String(action) + " " + resource
	}
	return name
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

// assignDefaultRolePermissions assigns appropriate permissions to admin and user roles.
func (b *Bootstrap) assignDefaultRolePermissions(tx *gorm.DB) error {
	b.logger.Info("Assigning default permissions to admin and user roles")

	// Define which permissions each role should have
	rolePermissions := map[string][]string{
		"admin": {
			"users.view", "users.create", "users.update", "users.delete",
			"roles.view", "roles.create", "roles.update", "roles.delete",
			"permissions.view", "permissions.manage",
			"templates.view", "templates.create", "templates.update", "templates.delete",
			"templates.export", "templates.import",
			"notifications.view", "notifications.create", "notifications.manage",
			"admin.access", "admin.manage", "admin.dashboard",
			"audit.view", "audit.export",
			"blog.posts.view", "blog.posts.create", "blog.posts.update", "blog.posts.delete",
			"blog.categories.view", "blog.categories.create", "blog.categories.update", "blog.categories.delete",
			"blog.tags.view", "blog.tags.create", "blog.tags.update", "blog.tags.delete",
			"blog.comments.view", "blog.comments.create", "blog.comments.update", "blog.comments.delete",
			"blog.media.view", "blog.media.create", "blog.media.delete",
		},
		"user": {
			"users.view",
			"notifications.view",
			"templates.view",
			"blog.posts.view", "blog.categories.view", "blog.tags.view",
			"blog.comments.view", "blog.comments.create",
			"blog.media.view",
		},
	}

	for roleName, permNames := range rolePermissions {
		var role domain.Role
		if err := tx.Where("name = ?", roleName).First(&role).Error; err != nil {
			b.logger.Warn("Role not found, skipping permission assignment", "role", roleName, "error", err)
			continue
		}

		for _, permName := range permNames {
			var perm domain.Permission
			if err := tx.Where("name = ? AND deleted_at IS NULL", permName).First(&perm).Error; err != nil {
				b.logger.Warn("Permission not found, skipping", "permission", permName, "error", err)
				continue
			}

			var count int64
			tx.Model(&domain.RolePermission{}).Where("role_id = ? AND permission_id = ?", role.ID, perm.ID).Count(&count)
			if count > 0 {
				continue
			}

			if err := tx.Create(&domain.RolePermission{
				RoleID:       role.ID,
				PermissionID: perm.ID,
			}).Error; err != nil {
				b.logger.Error("Failed to assign permission to role", "role", roleName, "permission", permName, "error", err)
				return err
			}
			b.logger.Debug("Assigned permission to role", "role", roleName, "permission", permName)
		}
	}

	return nil
}

// syncPermissionsToCasbin reads all role_permissions and ensures each one has a
// corresponding Casbin policy entry.
func (b *Bootstrap) syncPermissionsToCasbin(tx *gorm.DB) error {
	b.logger.Info("Syncing role-permission assignments to Casbin")

	type rolePermRow struct {
		RoleName       string
		PermissionName string
	}

	var rows []rolePermRow
	err := tx.Raw(`
		SELECT r.name AS role_name, p.name AS permission_name
		FROM role_permissions rp
		JOIN roles r ON r.id = rp.role_id AND r.deleted_at IS NULL
		JOIN permissions p ON p.id = rp.permission_id AND p.deleted_at IS NULL
	`).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("failed to query role-permission assignments: %w", err)
	}

	for _, row := range rows {
		mapping, ok := authorization.GetCasbinMapping(row.PermissionName)
		if !ok {
			b.logger.Warn("No Casbin mapping for permission, skipping", "permission", row.PermissionName)
			continue
		}

		if err := b.casbinService.AddPolicy(
			"role:"+row.RoleName,
			authorization.DomainDefault,
			string(mapping.Resource),
			mapping.Action,
			"allow",
		); err != nil {
			b.logger.Warn("Failed to add Casbin policy (may already exist)", "role", row.RoleName, "permission", row.PermissionName, "error", err)
		}
	}

	b.logger.Info("Casbin sync completed", "policies_processed", len(rows))
	return nil
}

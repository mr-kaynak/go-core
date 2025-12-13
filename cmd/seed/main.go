package main

import (
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Warning: .env file not found: %v\n", err)
	}

	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	if err := logger.Initialize(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	logger := logger.Get()
	logger.Info("Starting database seeder")

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}

	// Run seeders
	if err := seedDatabase(db.DB, logger); err != nil {
		logger.Error("Failed to seed database", "error", err)
		os.Exit(1)
	}

	logger.Info("Database seeding completed successfully")
}

func seedDatabase(db *gorm.DB, log *logger.Logger) error {
	// Create permissions
	permissions := []domain.Permission{
		// User permissions
		{Name: "users.view", Resource: "users", Action: "view"},
		{Name: "users.create", Resource: "users", Action: "create"},
		{Name: "users.update", Resource: "users", Action: "update"},
		{Name: "users.delete", Resource: "users", Action: "delete"},

		// Role permissions
		{Name: "roles.view", Resource: "roles", Action: "view"},
		{Name: "roles.create", Resource: "roles", Action: "create"},
		{Name: "roles.update", Resource: "roles", Action: "update"},
		{Name: "roles.delete", Resource: "roles", Action: "delete"},

		// Profile permissions
		{Name: "profile.view", Resource: "profile", Action: "view"},
		{Name: "profile.update", Resource: "profile", Action: "update"},

		// Admin permissions
		{Name: "admin.access", Resource: "admin", Action: "access"},
		{Name: "admin.users", Resource: "admin", Action: "users"},
		{Name: "admin.roles", Resource: "admin", Action: "roles"},
		{Name: "admin.settings", Resource: "admin", Action: "settings"},

		// Notification permissions
		{Name: "notifications.view", Resource: "notifications", Action: "view"},
		{Name: "notifications.send", Resource: "notifications", Action: "send"},
		{Name: "notifications.manage", Resource: "notifications", Action: "manage"},
	}

	log.Info("Creating permissions", "count", len(permissions))
	for i := range permissions {
		var existing domain.Permission
		if err := db.Where("name = ?", permissions[i].Name).First(&existing).Error; err == nil {
			log.Debug("Permission already exists", "name", permissions[i].Name)
			permissions[i] = existing
			continue
		}

		if err := db.Create(&permissions[i]).Error; err != nil {
			return fmt.Errorf("failed to create permission %s: %w", permissions[i].Name, err)
		}
		log.Debug("Created permission", "name", permissions[i].Name)
	}

	// Create roles with permissions
	roles := []struct {
		role        domain.Role
		permissions []string
	}{
		{
			role: domain.Role{
				Name:        "admin",
				Description: "Administrator with full access",
			},
			permissions: []string{
				"users.view", "users.create", "users.update", "users.delete",
				"roles.view", "roles.create", "roles.update", "roles.delete",
				"profile.view", "profile.update",
				"admin.access", "admin.users", "admin.roles", "admin.settings",
				"notifications.view", "notifications.send", "notifications.manage",
			},
		},
		{
			role: domain.Role{
				Name:        "moderator",
				Description: "Moderator with limited admin access",
			},
			permissions: []string{
				"users.view", "users.update",
				"roles.view",
				"profile.view", "profile.update",
				"admin.access", "admin.users",
				"notifications.view", "notifications.send",
			},
		},
		{
			role: domain.Role{
				Name:        "user",
				Description: "Regular user with basic access",
			},
			permissions: []string{
				"profile.view", "profile.update",
				"notifications.view",
			},
		},
	}

	log.Info("Creating roles", "count", len(roles))
	for _, r := range roles {
		var existingRole domain.Role
		if err := db.Where("name = ?", r.role.Name).First(&existingRole).Error; err == nil {
			log.Debug("Role already exists", "name", r.role.Name)
			continue
		}

		// Create role
		if err := db.Create(&r.role).Error; err != nil {
			return fmt.Errorf("failed to create role %s: %w", r.role.Name, err)
		}

		// Assign permissions to role
		for _, permName := range r.permissions {
			var perm domain.Permission
			if err := db.Where("name = ?", permName).First(&perm).Error; err != nil {
				log.Warn("Permission not found", "name", permName)
				continue
			}

			if err := db.Model(&r.role).Association("Permissions").Append(&perm); err != nil {
				log.Warn("Failed to assign permission to role", "role", r.role.Name, "permission", permName)
			}
		}

		log.Info("Created role with permissions", "role", r.role.Name, "permissions", len(r.permissions))
	}

	// Create users
	users := []struct {
		user     domain.User
		password string
		roleName string
	}{
		{
			user: domain.User{
				Email:     "admin@gocore.com",
				Username:  "admin",
				FirstName: "Admin",
				LastName:  "User",
				Status:    domain.UserStatusActive,
				Verified:  true,
			},
			password: "Admin@123456",
			roleName: "admin",
		},
		{
			user: domain.User{
				Email:     "moderator@gocore.com",
				Username:  "moderator",
				FirstName: "Moderator",
				LastName:  "User",
				Status:    domain.UserStatusActive,
				Verified:  true,
			},
			password: "Mod@123456",
			roleName: "moderator",
		},
		{
			user: domain.User{
				Email:     "user@gocore.com",
				Username:  "testuser",
				FirstName: "Test",
				LastName:  "User",
				Status:    domain.UserStatusActive,
				Verified:  true,
			},
			password: "User@123456",
			roleName: "user",
		},
		{
			user: domain.User{
				Email:     "john.doe@example.com",
				Username:  "johndoe",
				FirstName: "John",
				LastName:  "Doe",
				Phone:     "+1234567890",
				Status:    domain.UserStatusActive,
				Verified:  true,
			},
			password: "John@123456",
			roleName: "user",
		},
		{
			user: domain.User{
				Email:     "jane.smith@example.com",
				Username:  "janesmith",
				FirstName: "Jane",
				LastName:  "Smith",
				Phone:     "+0987654321",
				Status:    domain.UserStatusPending,
				Verified:  false,
			},
			password: "Jane@123456",
			roleName: "user",
		},
	}

	log.Info("Creating users", "count", len(users))
	for _, u := range users {
		// Check if user already exists
		var existingUser domain.User
		if err := db.Where("email = ?", u.user.Email).First(&existingUser).Error; err == nil {
			log.Debug("User already exists", "email", u.user.Email)
			continue
		}

		// Set password and create user
		u.user.ID = uuid.New()
		u.user.Password = u.password
		if err := u.user.HashPassword(); err != nil {
			return fmt.Errorf("failed to hash password for %s: %w", u.user.Email, err)
		}

		if err := db.Create(&u.user).Error; err != nil {
			return fmt.Errorf("failed to create user %s: %w", u.user.Email, err)
		}

		// Assign role to user
		var role domain.Role
		if err := db.Where("name = ?", u.roleName).First(&role).Error; err != nil {
			log.Warn("Role not found", "name", u.roleName)
			continue
		}

		if err := db.Model(&u.user).Association("Roles").Append(&role); err != nil {
			log.Warn("Failed to assign role to user", "user", u.user.Email, "role", u.roleName)
		}

		log.Info("Created user",
			"email", u.user.Email,
			"username", u.user.Username,
			"role", u.roleName,
			"status", u.user.Status,
			"verified", u.user.Verified,
		)
	}

	// Print summary
	var userCount, roleCount, permCount int64
	db.Model(&domain.User{}).Count(&userCount)
	db.Model(&domain.Role{}).Count(&roleCount)
	db.Model(&domain.Permission{}).Count(&permCount)

	log.Info("Seeding completed",
		"users", userCount,
		"roles", roleCount,
		"permissions", permCount,
	)

	// Print login credentials
	fmt.Println("\n========================================")
	fmt.Println("🎉 Database seeded successfully!")
	fmt.Println("========================================")
	fmt.Println("\nTest Accounts:")
	fmt.Println("----------------------------------------")
	fmt.Println("Admin:")
	fmt.Println("  Email: admin@gocore.com")
	fmt.Println("  Password: Admin@123456")
	fmt.Println("\nModerator:")
	fmt.Println("  Email: moderator@gocore.com")
	fmt.Println("  Password: Mod@123456")
	fmt.Println("\nRegular User:")
	fmt.Println("  Email: user@gocore.com")
	fmt.Println("  Password: User@123456")
	fmt.Println("========================================\n")

	return nil
}

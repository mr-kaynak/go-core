package repository

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB initialises an isolated in-memory SQLite database with all
// identity module tables. Each call creates a distinct database so tests
// cannot pollute each other.
//
// Raw DDL is used instead of AutoMigrate because the domain model tags
// contain PostgreSQL-specific defaults (gen_random_uuid(), jsonb) that
// SQLite cannot parse.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			LogLevel: logger.Silent,
		},
	)

	// Use a unique shared-cache database name per test to ensure isolation
	// while still allowing GORM's connection pool to work correctly.
	dbName := fmt.Sprintf("file:memdb_%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	t.Cleanup(func() {
		sqlDB.Close()
	})

	// Enable foreign keys
	db.Exec("PRAGMA foreign_keys=ON")

	// Create tables with SQLite-compatible DDL
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			first_name TEXT DEFAULT '',
			last_name TEXT DEFAULT '',
			phone TEXT DEFAULT '',
			avatar_url TEXT DEFAULT '',
			status TEXT DEFAULT 'pending',
			verified INTEGER DEFAULT 0,
			last_login DATETIME,
			failed_login_attempts INTEGER DEFAULT 0,
			locked_until DATETIME,
			two_factor_secret TEXT DEFAULT '',
			two_factor_enabled INTEGER DEFAULT 0,
			two_factor_backup_codes TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at)`,

		`CREATE TABLE IF NOT EXISTS roles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT DEFAULT '',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_roles_deleted_at ON roles(deleted_at)`,

		`CREATE TABLE IF NOT EXISTS permissions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT DEFAULT '',
			category TEXT DEFAULT '',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_permissions_deleted_at ON permissions(deleted_at)`,
		`CREATE INDEX IF NOT EXISTS idx_permissions_category ON permissions(category)`,

		`CREATE TABLE IF NOT EXISTS user_roles (
			user_id TEXT NOT NULL,
			role_id TEXT NOT NULL,
			PRIMARY KEY (user_id, role_id)
		)`,

		`CREATE TABLE IF NOT EXISTS role_permissions (
			role_id TEXT NOT NULL,
			permission_id TEXT NOT NULL,
			created_at DATETIME,
			PRIMARY KEY (role_id, permission_id)
		)`,

		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			ip_address TEXT DEFAULT '',
			user_agent TEXT DEFAULT '',
			expires_at DATETIME NOT NULL,
			revoked INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id)`,

		`CREATE TABLE IF NOT EXISTS verification_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL,
			used INTEGER DEFAULT 0,
			used_at DATETIME,
			expires_at DATETIME NOT NULL,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_verification_tokens_user_id ON verification_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_verification_tokens_deleted_at ON verification_tokens(deleted_at)`,

		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			name TEXT NOT NULL,
			scopes TEXT DEFAULT '',
			expires_at DATETIME,
			last_used_at DATETIME,
			revoked INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_deleted_at ON api_keys(deleted_at)`,

		`CREATE TABLE IF NOT EXISTS api_key_roles (
			api_key_id TEXT NOT NULL,
			role_id TEXT NOT NULL,
			created_at DATETIME,
			PRIMARY KEY (api_key_id, role_id)
		)`,

		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			resource_id TEXT DEFAULT '',
			ip_address TEXT DEFAULT '',
			user_agent TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			created_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at)`,
	}

	for _, stmt := range ddl {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to execute DDL: %v\nstatement: %s", err, stmt)
		}
	}

	return db
}

// seedUser inserts a user directly into the database, bypassing the
// BeforeCreate hook (which would hash the password).
func seedUser(t *testing.T, db *gorm.DB, email, username string) *domain.User {
	t.Helper()

	user := &domain.User{
		ID:       uuid.New(),
		Email:    email,
		Username: username,
		Password: "hashed-password",
		Status:   domain.UserStatusActive,
		Verified: true,
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	if err := db.Exec(
		`INSERT INTO users (id, email, username, password, status, verified, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID.String(), user.Email, user.Username, user.Password,
		string(user.Status), true, now, now,
	).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	return user
}

// seedRole inserts a role directly into the database.
func seedRole(t *testing.T, db *gorm.DB, name string) *domain.Role {
	t.Helper()

	role := &domain.Role{
		ID:   uuid.New(),
		Name: name,
	}

	if err := db.Exec(
		`INSERT INTO roles (id, name, created_at, updated_at) VALUES (?, ?, datetime('now'), datetime('now'))`,
		role.ID.String(), role.Name,
	).Error; err != nil {
		t.Fatalf("failed to seed role: %v", err)
	}

	return role
}

// seedPermission inserts a permission directly into the database.
func seedPermission(t *testing.T, db *gorm.DB, name string) *domain.Permission {
	t.Helper()

	perm := &domain.Permission{
		ID:   uuid.New(),
		Name: name,
	}

	if err := db.Exec(
		`INSERT INTO permissions (id, name, created_at, updated_at) VALUES (?, ?, datetime('now'), datetime('now'))`,
		perm.ID.String(), perm.Name,
	).Error; err != nil {
		t.Fatalf("failed to seed permission: %v", err)
	}

	return perm
}

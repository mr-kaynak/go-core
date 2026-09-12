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
	"github.com/mr-kaynak/go-core/internal/test/sqliteschema"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB initializes an isolated in-memory SQLite database with all
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

	// Create tables with the shared SQLite-compatible DDL
	if err := sqliteschema.ApplyIdentity(db); err != nil {
		t.Fatalf("failed to apply identity schema: %v", err)
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

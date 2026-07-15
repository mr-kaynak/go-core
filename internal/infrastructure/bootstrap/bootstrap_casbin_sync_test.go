package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupSyncCasbinTestDB creates an isolated in-memory SQLite database with the
// minimal schema syncCasbin (and the Casbin gorm-adapter it drives) need:
// roles, permissions, role_permissions, users, and the casbin_rule table the
// adapter manages itself. This mirrors the identity repository package's
// existing setupTestDB pattern (raw DDL, since domain model tags use
// PostgreSQL-only defaults that SQLite cannot parse).
func setupSyncCasbinTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbName := "file:memdb_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

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

		`CREATE TABLE IF NOT EXISTS role_permissions (
			role_id TEXT NOT NULL,
			permission_id TEXT NOT NULL,
			created_at DATETIME,
			PRIMARY KEY (role_id, permission_id)
		)`,

		`CREATE TABLE IF NOT EXISTS user_roles (
			user_id TEXT NOT NULL,
			role_id TEXT NOT NULL,
			PRIMARY KEY (user_id, role_id)
		)`,
	}

	for _, stmt := range ddl {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to execute DDL: %v\nstatement: %s", err, stmt)
		}
	}

	return db
}

// TestSyncCasbinIsIdempotent proves the bug fix end-to-end at the syncCasbin
// level: running the Casbin sync step twice in a row against the same
// already-bootstrapped database must not error the second time. Before the
// fix, the system-admin role binding used AddRoleForUser, which returns a
// Conflict once the binding already exists from the first sync, and
// bootstrap.Run wrapped that as fatal — crashing every second+ app startup.
//
// This uses an in-memory SQLite database (via the identity repository and a
// real CasbinService backed by the gorm-adapter, which is DB-agnostic) so it
// requires no live PostgreSQL instance and runs in CI like the rest of the
// suite.
func TestSyncCasbinIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := setupSyncCasbinTestDB(t)

	casbinService, err := authorization.NewCasbinService(nil, db)
	if err != nil {
		t.Fatalf("failed to create casbin service: %v", err)
	}

	userRepo := identityRepo.NewUserRepository(db)

	b := NewBootstrap(db, userRepo, casbinService)

	// Seed roles, a permission, a role-permission assignment, and the
	// system admin user — i.e. the state that would exist after Bootstrap.Run's
	// transactional phase has already committed once.
	roleAdmin := uuid.New()
	roleUser := uuid.New()
	roleSystemAdmin := uuid.New()
	now := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	for _, r := range []struct{ id, name string }{
		{roleSystemAdmin.String(), "system_admin"},
		{roleAdmin.String(), "admin"},
		{roleUser.String(), "user"},
	} {
		if err := db.Exec(
			`INSERT INTO roles (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			r.id, r.name, now, now,
		).Error; err != nil {
			t.Fatalf("failed to seed role %s: %v", r.name, err)
		}
	}

	permID := uuid.New().String()
	if err := db.Exec(
		`INSERT INTO permissions (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		permID, "users.view", now, now,
	).Error; err != nil {
		t.Fatalf("failed to seed permission: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO role_permissions (role_id, permission_id, created_at) VALUES (?, ?, ?)`,
		roleAdmin.String(), permID, now,
	).Error; err != nil {
		t.Fatalf("failed to seed role_permission: %v", err)
	}

	adminUserID := uuid.New()
	if err := db.Exec(
		`INSERT INTO users (id, email, username, password, status, verified, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		adminUserID.String(), systemAdminEmail, "system_admin", "hashed", "active", 1, now, now,
	).Error; err != nil {
		t.Fatalf("failed to seed system admin user: %v", err)
	}

	// First sync: establishes the role hierarchy, policies, and the system
	// admin's role binding — mirroring the post-commit step of a fresh
	// Bootstrap.Run.
	if err := b.syncCasbin(ctx); err != nil {
		t.Fatalf("first syncCasbin call should succeed, got: %v", err)
	}

	roles, err := casbinService.GetRolesForUser(adminUserID, authorization.DomainDefault)
	if err != nil {
		t.Fatalf("GetRolesForUser failed: %v", err)
	}
	if len(roles) != 1 || roles[0] != "system_admin" {
		t.Fatalf("expected system admin to have exactly one role binding after first sync, got %v", roles)
	}

	// Second sync: this is the exact "restart against an already-bootstrapped
	// database" scenario that used to crash bootstrap fatally.
	if err := b.syncCasbin(ctx); err != nil {
		t.Fatalf("second syncCasbin call must be idempotent and succeed, got: %v", err)
	}

	roles, err = casbinService.GetRolesForUser(adminUserID, authorization.DomainDefault)
	if err != nil {
		t.Fatalf("GetRolesForUser failed after second sync: %v", err)
	}
	if len(roles) != 1 || roles[0] != "system_admin" {
		t.Fatalf("expected system admin to still have exactly one role binding after second sync, got %v", roles)
	}
}

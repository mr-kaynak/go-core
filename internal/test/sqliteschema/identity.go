// Package sqliteschema provides SQLite-compatible DDL for test databases.
//
// The identity domain models carry PostgreSQL-specific tags
// (gen_random_uuid(), jsonb) that SQLite cannot parse, so GORM AutoMigrate is
// not an option for them; tests share this hand-maintained DDL instead. Keep
// it in sync with coremigrations/sql when identity tables change.
package sqliteschema

import "gorm.io/gorm"

// IdentityDDL returns the SQLite-compatible schema statements for the
// identity module tables (users, roles, permissions, tokens, api keys, audit).
func IdentityDDL() []string {
	return []string{
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
}

// ApplyIdentity executes the identity DDL on the given database.
func ApplyIdentity(db *gorm.DB) error {
	for _, stmt := range IdentityDDL() {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

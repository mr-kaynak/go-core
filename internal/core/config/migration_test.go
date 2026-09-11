package config

import (
	"strings"
	"testing"
	"time"
)

// setDatabaseEnv sets what a migration job would actually be given: a database
// role, and nothing else.
func setDatabaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_HOST", "db.internal")
	t.Setenv("DB_NAME", "orders")
	t.Setenv("DB_USER", "orders_migrator")
	t.Setenv("DB_PASSWORD", "")
}

// The reason this function exists. A job that runs SQL must not need the
// application's signing keys and mail credentials to start, because the only
// way to satisfy that requirement is to give them to it.
func TestLoadMigrationNeedsNoApplicationSecrets(t *testing.T) {
	setDatabaseEnv(t)

	settings, err := LoadMigration()
	if err != nil {
		t.Fatalf("LoadMigration failed with only the database settings present: %v", err)
	}
	if settings.Database.Name != "orders" {
		t.Fatalf("database name = %q, want %q", settings.Database.Name, "orders")
	}

	// The same environment through the full loader, to show the difference is
	// real rather than an artifact of what happens to be set in this process.
	if _, err := Load(); err == nil {
		t.Fatal("Load must still refuse an environment with no JWT secret; " +
			"if it stopped refusing, the migration-scoped loader has no purpose")
	}
}

// The two loaders must agree about every setting they share. A migration job
// that took the lock with different bounds than the application waiting on it
// would be a second policy for one database.
func TestLoadMigrationAgreesWithLoad(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SECURITY_ENCRYPTION_KEY", "test-encryption-key-that-is-at-least-32-chars")
	t.Setenv("DB_MIGRATION_LOCK_WAIT", "45s")
	t.Setenv("DB_MIGRATION_LOCK_TIMEOUT", "9s")
	t.Setenv("DB_MIGRATION_STATEMENT_TIMEOUT", "11m")
	t.Setenv("DB_ALLOW_PENDING_MIGRATIONS", "true")
	t.Setenv("DATABASE_CONN_MAX_LIFETIME", "17m")

	full, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	scoped, err := LoadMigration()
	if err != nil {
		t.Fatalf("LoadMigration failed: %v", err)
	}

	if full.GetDSN() != scoped.GetDSN() {
		t.Fatalf("DSN differs between loaders:\n full:   %s\n scoped: %s", full.GetDSN(), scoped.GetDSN())
	}
	if full.Database != scoped.Database {
		t.Fatalf("database settings differ between loaders:\n full:   %+v\n scoped: %+v",
			full.Database, scoped.Database)
	}
	if scoped.Database.MigrationLockWait != 45*time.Second {
		t.Fatalf("migration lock wait = %v, want 45s — an env var that does not reach the "+
			"migration job is a bound nobody applies", scoped.Database.MigrationLockWait)
	}
	if scoped.Database.ConnMaxLifetime != 17*time.Minute {
		t.Fatalf("conn max lifetime = %v, want 17m; this key needs explicit duration "+
			"parsing and would be zero without it", scoped.Database.ConnMaxLifetime)
	}
}

// The guard belongs to the connection, not to the process that opens it. A
// migration carries the whole schema over the same wire.
func TestLoadMigrationRefusesUnencryptedTransportOutsideDevelopment(t *testing.T) {
	setDatabaseEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_SSL_MODE", "disable")

	if _, err := LoadMigration(); err == nil {
		t.Fatal("a production migration over an unencrypted connection must be refused")
	} else if !strings.Contains(err.Error(), "ssl_mode") {
		t.Fatalf("refusal should name the setting at fault, got: %v", err)
	}
}

// LoadMigration must not publish, because what it loaded is partial. A later
// Get().JWT.Secret would read an empty string rather than fail, and an empty
// string is a usable HMAC key — every token in the process would verify
// against a secret nobody set.
func TestLoadMigrationDoesNotPublishAPartialGlobal(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SECURITY_ENCRYPTION_KEY", "test-encryption-key-that-is-at-least-32-chars")

	full, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	t.Setenv("DB_NAME", "some_other_database")
	if _, err := LoadMigration(); err != nil {
		t.Fatalf("LoadMigration failed: %v", err)
	}

	if Get() != full {
		t.Fatal("LoadMigration replaced the published configuration with a partial one")
	}
}

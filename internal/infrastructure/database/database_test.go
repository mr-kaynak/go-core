package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newSQLiteDB(t *testing.T) *DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite gorm db: %v", err)
	}
	return &DB{DB: gdb}
}

func TestRunMigrationsReturnsErrorWhenGooseUpFails(t *testing.T) {
	db := newSQLiteDB(t)
	missingDir := filepath.Join(t.TempDir(), "missing")

	err := RunMigrations(db, missingDir)
	if err == nil {
		t.Fatalf("expected RunMigrations to fail for missing dir")
	}
	if !strings.Contains(err.Error(), "failed to run migrations") {
		t.Fatalf("expected wrapped migration error, got %v", err)
	}
}

func TestMigrationStatusReturnsErrorWhenStatusFails(t *testing.T) {
	db := newSQLiteDB(t)
	missingDir := filepath.Join(t.TempDir(), "missing")

	err := MigrationStatus(db, missingDir)
	if err == nil {
		t.Fatalf("expected MigrationStatus to fail for missing dir")
	}
}

func TestDatabaseHealthCheckSuccessAndFailure(t *testing.T) {
	db := newSQLiteDB(t)

	if err := db.HealthCheck(); err != nil {
		t.Fatalf("expected healthcheck success, got %v", err)
	}

	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	_ = sqlDB.Close()

	if err := db.HealthCheck(); err == nil {
		t.Fatalf("expected healthcheck failure after sql db close")
	}
}

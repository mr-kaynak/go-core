package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
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

// testModel is a minimal model for exercising GORM callbacks.
type testModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"column:name"`
}

func (testModel) TableName() string { return "test_metrics_models" }

func TestInitialize_InvalidDSN(t *testing.T) {
	_ = logger.Get()
	cfg := &config.Config{
		App: config.AppConfig{Env: "test", Debug: false},
		Database: config.DatabaseConfig{
			Host:            "invalid-nonexistent-host",
			Port:            5432,
			Name:            "testdb",
			User:            "user",
			Password:        "pass",
			SSLMode:         "disable",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: time.Minute,
			ConnMaxIdleTime: time.Minute,
		},
	}
	_, err := Initialize(cfg)
	if err == nil {
		t.Fatal("expected Initialize to fail with invalid DSN")
	}
	if !strings.Contains(err.Error(), "failed to connect to database") {
		t.Errorf("expected connection error, got %v", err)
	}
}

func TestClose_Success(t *testing.T) {
	db := newSQLiteDB(t)
	if err := db.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
}

func TestTransaction_Success(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec("SELECT 1").Error
	})
	if err != nil {
		t.Errorf("Transaction(success) = %v", err)
	}
}

func TestTransaction_Rollback(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	err := db.Transaction(func(tx *gorm.DB) error {
		_ = tx.Exec("SELECT 1")
		return gorm.ErrInvalidData
	})
	if err != gorm.ErrInvalidData {
		t.Errorf("Transaction(rollback) = %v, want ErrInvalidData", err)
	}
}

func TestGormLogAdapter_Printf(t *testing.T) {
	_ = logger.Get()
	adapter := &gormLogAdapter{logger: logger.Get()}
	adapter.Printf("test %s %d", "message", 42)
}

func TestNewGormLogger_LogLevels(t *testing.T) {
	_ = logger.Get()
	tests := []struct {
		name   string
		env    string
		debug  bool
	}{
		{"production_silent", "production", false},
		{"development_warn", "development", false},
		{"development_debug_info", "development", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				App: config.AppConfig{Env: tt.env, Debug: tt.debug},
			}
			lg := newGormLogger(cfg)
			if lg == nil {
				t.Fatal("newGormLogger returned nil")
			}
		})
	}
}

func TestRegisterMetricsCallbacks_RecordsQueries(t *testing.T) {
	_ = logger.Get()
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	registerMetricsCallbacks(db.DB)

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Trigger create callback (success).
	if err := db.DB.Create(&testModel{Name: "a"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	// Trigger query callback (success).
	var m testModel
	if err := db.DB.Where("name = ?", "a").First(&m).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	// Trigger update callback (success).
	if err := db.DB.Model(&m).Update("name", "b").Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	// Trigger delete callback (success).
	if err := db.DB.Delete(&m).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestRegisterMetricsCallbacks_NoPanicWhenStartTimeMissing(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	registerMetricsCallbacks(db.DB)
	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Run a query; after-callback may run without start time in edge cases.
	// Here we just ensure the callback path does not panic.
	_ = db.DB.Create(&testModel{Name: "x"}).Error
}

func TestRegisterMetricsCallbacks_HandlesInvalidStartTimeType(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	registerMetricsCallbacks(db.DB)
	// Override start time with wrong type so record() hits the !ok branch.
	_ = db.DB.Callback().Create().Before("gorm:create").Register("test:bad_start", func(tx *gorm.DB) {
		tx.Set(metricsStartTimeKey, "not-a-time")
	})
	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = db.DB.Create(&testModel{Name: "y"}).Error
}

func TestStartConnectionMetrics_ContextCanceled(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db.StartConnectionMetrics(ctx)
}

func TestStartConnectionMetrics_RespectsContextTimeout(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	db.StartConnectionMetrics(ctx)
}

func TestRunMigrationsReturnsErrorWhenGooseUpFails(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
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
	defer func() { _ = db.Close() }()
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

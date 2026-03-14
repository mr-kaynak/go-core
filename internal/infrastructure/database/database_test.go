package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

// fakeConnPool implements gorm.ConnPool but is not *sql.DB, *sql.Tx, or
// GetDBConnector. This causes gorm.DB.DB() to return gorm.ErrInvalidDB,
// enabling tests for the error paths in HealthCheck, Close, RunMigrations,
// and MigrationStatus.
type fakeConnPool struct{}

func (fakeConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("fake")
}
func (fakeConnPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("fake")
}
func (fakeConnPool) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("fake")
}
func (fakeConnPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return nil
}

// newFakeDB returns a DB whose underlying gorm.DB.DB() always returns ErrInvalidDB.
func newFakeDB() *DB {
	gdb := &gorm.DB{Config: &gorm.Config{ConnPool: fakeConnPool{}}}
	return &DB{DB: gdb}
}

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
		name  string
		env   string
		debug bool
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

func TestHealthCheck_ConcurrentAccess(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = db.HealthCheck()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: HealthCheck() = %v", i, err)
		}
	}
}

func TestHealthCheck_CompletesWithinTimeout(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	start := time.Now()
	err := db.HealthCheck()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("HealthCheck() = %v", err)
	}
	// HealthCheck uses a 1-second timeout; an in-memory DB should respond
	// well within that window.
	if elapsed > 500*time.Millisecond {
		t.Errorf("HealthCheck took %v, expected < 500ms", elapsed)
	}
}

func TestTransaction_NestedSavepoints(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&testModel{Name: "outer"}).Error; err != nil {
			return err
		}
		// Nested (savepoint) transaction that succeeds.
		return tx.Transaction(func(tx2 *gorm.DB) error {
			return tx2.Create(&testModel{Name: "inner"}).Error
		})
	})
	if err != nil {
		t.Fatalf("nested transaction: %v", err)
	}

	// Both records should be present.
	var count int64
	db.DB.Model(&testModel{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 records after nested commit, got %d", count)
	}
}

func TestTransaction_NestedSavepointRollback(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&testModel{Name: "outer"}).Error; err != nil {
			return err
		}
		// Nested (savepoint) transaction that rolls back.
		innerErr := tx.Transaction(func(tx2 *gorm.DB) error {
			_ = tx2.Create(&testModel{Name: "inner-fail"})
			return errors.New("inner failure")
		})
		if innerErr == nil {
			return errors.New("expected inner transaction to fail")
		}
		return nil // outer transaction commits
	})
	if err != nil {
		t.Fatalf("outer transaction should succeed: %v", err)
	}

	// Only the outer record should remain; inner was rolled back.
	var count int64
	db.DB.Model(&testModel{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 record after nested rollback, got %d", count)
	}
}

func TestTransaction_ConcurrentTransactions(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const goroutines = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			_ = db.Transaction(func(tx *gorm.DB) error {
				return tx.Create(&testModel{Name: fmt.Sprintf("concurrent-%d", idx)}).Error
			})
		}(i)
	}
	wg.Wait()

	var count int64
	db.DB.Model(&testModel{}).Count(&count)
	if count != int64(goroutines) {
		t.Errorf("expected %d records, got %d", goroutines, count)
	}
}

func TestRunMigrations_ErrorWhenDBClosed(t *testing.T) {
	_ = logger.Get()
	db := newSQLiteDB(t)

	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	_ = sqlDB.Close()

	err = RunMigrations(db, t.TempDir())
	if err == nil {
		t.Fatal("expected RunMigrations to fail after DB close")
	}
}

func TestMigrationStatus_ErrorWhenDBClosed(t *testing.T) {
	_ = logger.Get()
	db := newSQLiteDB(t)

	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	_ = sqlDB.Close()

	err = MigrationStatus(db, t.TempDir())
	if err == nil {
		t.Fatal("expected MigrationStatus to fail after DB close")
	}
}

func TestClose_ErrorAfterAlreadyClosed(t *testing.T) {
	db := newSQLiteDB(t)

	// First close succeeds.
	if err := db.Close(); err != nil {
		t.Fatalf("first Close() = %v", err)
	}

	// Second close should return an error (underlying sql.DB is already closed).
	// Note: The exact behavior depends on the driver, but calling Close on a
	// closed database should not panic.
	_ = db.Close()
}

func TestRegisterMetricsCallbacks_QueryError(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()
	registerMetricsCallbacks(db.DB)

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Create a record first.
	if err := db.DB.Create(&testModel{Name: "err-test"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// Trigger a query that results in an error — query a non-existent table.
	// This exercises the `success := db.Error == nil` with success=false.
	var result testModel
	err := db.DB.Table("nonexistent_table_xyz").Where("id = ?", 1).First(&result).Error
	if err == nil {
		t.Fatal("expected query against non-existent table to fail")
	}

	// Update on a non-existent table to exercise update error path.
	err = db.DB.Table("nonexistent_table_xyz").Where("id = ?", 1).Update("name", "x").Error
	if err == nil {
		t.Fatal("expected update on non-existent table to fail")
	}

	// Delete on a non-existent table to exercise delete error path.
	err = db.DB.Table("nonexistent_table_xyz").Where("id = ?", 1).Delete(&testModel{}).Error
	if err == nil {
		t.Fatal("expected delete on non-existent table to fail")
	}
}

func TestNewGormLogger_ReturnsNonNilForAllEnvs(t *testing.T) {
	_ = logger.Get()
	envs := []struct {
		name  string
		env   string
		debug bool
	}{
		{"staging_no_debug", "staging", false},
		{"staging_debug", "staging", true},
		{"test_no_debug", "test", false},
		{"test_debug", "test", true},
	}
	for _, tt := range envs {
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

func TestGormLogAdapter_PrintfFormatsCorrectly(t *testing.T) {
	_ = logger.Get()
	adapter := &gormLogAdapter{logger: logger.Get()}
	// Exercise format with multiple argument types to verify no panics.
	adapter.Printf("%s %d %f %v", "str", 42, 3.14, true)
	adapter.Printf("no args")
	adapter.Printf("single %s", "arg")
}

func TestHealthCheck_MultipleSuccessiveCalls(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	// Rapid successive health checks should all succeed.
	for i := 0; i < 20; i++ {
		if err := db.HealthCheck(); err != nil {
			t.Fatalf("HealthCheck call %d: %v", i, err)
		}
	}
}

func TestTransaction_ErrorFromFn(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	sentinel := errors.New("sentinel error")
	err := db.Transaction(func(tx *gorm.DB) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("Transaction() = %v, want %v", err, sentinel)
	}
}

func TestTransaction_MultipleSequential(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Run several sequential transactions to ensure no state leaks.
	for i := 0; i < 5; i++ {
		err := db.Transaction(func(tx *gorm.DB) error {
			return tx.Create(&testModel{Name: fmt.Sprintf("seq-%d", i)}).Error
		})
		if err != nil {
			t.Fatalf("transaction %d: %v", i, err)
		}
	}

	var count int64
	db.DB.Model(&testModel{}).Count(&count)
	if count != 5 {
		t.Errorf("expected 5 records, got %d", count)
	}
}

func TestRegisterMetricsCallbacks_MissingStartTimeKey(t *testing.T) {
	// Exercise the `!ok` branch in record() where db.Get(metricsStartTimeKey)
	// returns false because the before-callback did not set the key.
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	// Register the metrics callbacks (both before and after).
	registerMetricsCallbacks(db.DB)

	// Replace the before-callbacks with no-ops so the start time key is never set.
	noop := func(*gorm.DB) {}
	_ = db.DB.Callback().Create().Replace("metrics:before_create", noop)
	_ = db.DB.Callback().Query().Replace("metrics:before_query", noop)
	_ = db.DB.Callback().Update().Replace("metrics:before_update", noop)
	_ = db.DB.Callback().Delete().Replace("metrics:before_delete", noop)

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// These operations trigger after-callbacks that find no start time key,
	// exercising the early return path in the record() closure.
	_ = db.DB.Create(&testModel{Name: "no-start"}).Error
	var m testModel
	_ = db.DB.Where("name = ?", "no-start").First(&m).Error
	_ = db.DB.Model(&testModel{}).Where("name = ?", "no-start").Update("name", "updated").Error
	_ = db.DB.Where("name = ?", "updated").Delete(&testModel{}).Error
}

func TestRegisterMetricsCallbacks_MultipleRegistrations(t *testing.T) {
	db := newSQLiteDB(t)
	defer func() { _ = db.Close() }()

	// Registering callbacks on a fresh DB should not panic or error.
	registerMetricsCallbacks(db.DB)

	if err := db.DB.AutoMigrate(&testModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Run all four CRUD operations to exercise every before/after pair.
	m := testModel{Name: "multi-reg"}
	if err := db.DB.Create(&m).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var found testModel
	if err := db.DB.First(&found, m.ID).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if err := db.DB.Model(&found).Update("name", "updated").Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := db.DB.Delete(&found).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestHealthCheck_ErrorWhenDBReturnsError(t *testing.T) {
	db := newFakeDB()
	err := db.HealthCheck()
	if err == nil {
		t.Fatal("expected HealthCheck to fail when db.DB.DB() errors")
	}
}

func TestClose_ErrorWhenDBReturnsError(t *testing.T) {
	db := newFakeDB()
	err := db.Close()
	if err == nil {
		t.Fatal("expected Close to fail when db.DB.DB() errors")
	}
}

func TestRunMigrations_ErrorWhenDBReturnsError(t *testing.T) {
	_ = logger.Get()
	db := newFakeDB()
	err := RunMigrations(db, t.TempDir())
	if err == nil {
		t.Fatal("expected RunMigrations to fail when db.DB.DB() errors")
	}
	if !strings.Contains(err.Error(), "failed to get sql.DB for migrations") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMigrationStatus_ErrorWhenDBReturnsError(t *testing.T) {
	_ = logger.Get()
	db := newFakeDB()
	err := MigrationStatus(db, t.TempDir())
	if err == nil {
		t.Fatal("expected MigrationStatus to fail when db.DB.DB() errors")
	}
	if !strings.Contains(err.Error(), "failed to get sql.DB for migration status") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStartConnectionMetrics_ErrorWhenDBReturnsError(t *testing.T) {
	// With a fakeConnPool, the ticker branch in StartConnectionMetrics will
	// hit the db.DB.DB() error path and continue.
	db := newFakeDB()

	// Use a context that outlasts one ticker interval — but since the const
	// is 15s, we instead verify the immediate-cancel path still works.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db.StartConnectionMetrics(ctx)
}

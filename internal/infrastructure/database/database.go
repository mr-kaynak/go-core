package database

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

// DB wraps the GORM database instance
type DB struct {
	*gorm.DB
}

// Initialize creates and configures the database connection
func Initialize(cfg *config.Config) (*DB, error) {
	log := logger.Get()
	log.Info("Connecting to database...",
		"host", cfg.Database.Host,
		"port", cfg.Database.Port,
		"database", cfg.Database.Name,
	)

	// Build DSN
	dsn := cfg.GetDSN()

	// Configure GORM
	gormConfig := &gorm.Config{
		Logger:                                   newGormLogger(cfg),
		SkipDefaultTransaction:                   true,
		PrepareStmt:                              true,
		DisableForeignKeyConstraintWhenMigrating: false,
	}

	// Open database connection
	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Get underlying SQL database
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.Database.ConnMaxIdleTime)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Database connection established successfully")

	// Register OpenTelemetry tracing plugin
	if err := db.Use(tracing.NewPlugin()); err != nil {
		log.Warn("Failed to register GORM tracing plugin", "error", err)
	}

	// Register GORM callbacks for query metrics
	registerMetricsCallbacks(db)

	return &DB{DB: db}, nil
}

// newGormLogger creates a new GORM logger
func newGormLogger(cfg *config.Config) gormlogger.Interface {
	logLevel := gormlogger.Warn
	if cfg.IsDevelopment() && cfg.App.Debug {
		logLevel = gormlogger.Info
	}

	return gormlogger.New(
		&gormLogAdapter{logger: logger.Get()},
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

// gormLogAdapter adapts our logger to GORM's logger interface
type gormLogAdapter struct {
	logger *logger.Logger
}

func (l *gormLogAdapter) Printf(format string, args ...interface{}) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}

// RunMigrations runs all pending goose migrations
func RunMigrations(db *DB, migrationsDir string) error {
	log := logger.Get()
	log.Info("Running database migrations...", "dir", migrationsDir)

	sqlDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB for migrations: %w", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.Up(sqlDB, migrationsDir); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info("Database migrations completed successfully")
	return nil
}

// MigrationStatus prints the status of all migrations
func MigrationStatus(db *DB, migrationsDir string) error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB for migration status: %w", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	return goose.Status(sqlDB, migrationsDir)
}

// Close closes the underlying database connection pool.
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Transaction executes a function within a database transaction
func (db *DB) Transaction(fn func(*gorm.DB) error) error {
	return db.DB.Transaction(fn)
}

// HealthCheck performs a health check on the database
func (db *DB) HealthCheck() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	return sqlDB.PingContext(ctx)
}

// metricsStartTimeKey is the GORM setting key for storing query start time.
const metricsStartTimeKey = "metrics:start_time"

// registerMetricsCallbacks registers GORM callbacks that record query duration and status.
func registerMetricsCallbacks(db *gorm.DB) {
	setStart := func(db *gorm.DB) { db.Set(metricsStartTimeKey, time.Now()) }

	_ = db.Callback().Create().Before("gorm:create").Register("metrics:before_create", setStart)
	_ = db.Callback().Query().Before("gorm:query").Register("metrics:before_query", setStart)
	_ = db.Callback().Update().Before("gorm:update").Register("metrics:before_update", setStart)
	_ = db.Callback().Delete().Before("gorm:delete").Register("metrics:before_delete", setStart)

	record := func(operation string) func(*gorm.DB) {
		return func(db *gorm.DB) {
			v, ok := db.Get(metricsStartTimeKey)
			if !ok {
				return
			}
			start, ok := v.(time.Time)
			if !ok {
				return
			}
			table := db.Statement.Table
			success := db.Error == nil
			metrics.GetMetrics().RecordDBQuery(operation, table, success, time.Since(start))
		}
	}

	_ = db.Callback().Create().After("gorm:create").Register("metrics:after_create", record("create"))
	_ = db.Callback().Query().After("gorm:query").Register("metrics:after_query", record("select"))
	_ = db.Callback().Update().After("gorm:update").Register("metrics:after_update", record("update"))
	_ = db.Callback().Delete().After("gorm:delete").Register("metrics:after_delete", record("delete"))
}

// StartConnectionMetrics periodically reports DB connection pool stats to Prometheus.
// It blocks until ctx is canceled; call it in a goroutine.
func (db *DB) StartConnectionMetrics(ctx context.Context) {
	const connectionMetricsInterval = 15
	ticker := time.NewTicker(connectionMetricsInterval * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sqlDB, err := db.DB.DB()
			if err != nil {
				continue
			}
			stats := sqlDB.Stats()
			metrics.GetMetrics().UpdateDBConnections(stats.OpenConnections, stats.Idle)
		}
	}
}

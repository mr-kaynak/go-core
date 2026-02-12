package database

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
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

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Database connection established successfully")

	return &DB{DB: db}, nil
}

// newGormLogger creates a new GORM logger
func newGormLogger(cfg *config.Config) gormlogger.Interface {
	logLevel := gormlogger.Silent

	if cfg.IsDevelopment() {
		logLevel = gormlogger.Info
		if cfg.App.Debug {
			logLevel = gormlogger.Info
		}
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

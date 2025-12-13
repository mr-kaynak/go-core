package database

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	messagingDomain "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"
	identityDomain "github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
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

	// Auto-migrate models if in development
	if cfg.IsDevelopment() {
		if err := autoMigrate(db); err != nil {
			log.Warn("Failed to auto-migrate database", "error", err)
		}
	}

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

// autoMigrate runs auto-migration for all models
func autoMigrate(db *gorm.DB) error {
	// Import identity models
	if err := db.AutoMigrate(
		&identityDomain.User{},
		&identityDomain.Role{},
		&identityDomain.Permission{},
		&identityDomain.RefreshToken{},
		&identityDomain.VerificationToken{},
	); err != nil {
		return fmt.Errorf("failed to migrate identity models: %w", err)
	}

	// Import notification models
	if err := db.AutoMigrate(
		&notificationDomain.Notification{},
		&notificationDomain.EmailLog{},
		&notificationDomain.NotificationTemplate{},
		&notificationDomain.NotificationPreference{},
		&notificationDomain.ExtendedNotificationTemplate{},
		&notificationDomain.TemplateLanguage{},
		&notificationDomain.TemplateVariable{},
		&notificationDomain.TemplateCategory{},
	); err != nil {
		return fmt.Errorf("failed to migrate notification models: %w", err)
	}

	// Import messaging models (outbox pattern)
	if err := db.AutoMigrate(
		&messagingDomain.OutboxMessage{},
		&messagingDomain.OutboxDeadLetter{},
		&messagingDomain.OutboxProcessingLog{},
	); err != nil {
		return fmt.Errorf("failed to migrate messaging models: %w", err)
	}

	return nil
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

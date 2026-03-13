// @title Go-Core API
// @version 1.0.0
// @description Enterprise-grade Go boilerplate with REST API, gRPC services, and notification system

// @contact.name Go-Core Team
// @contact.url https://github.com/mr-kaynak/go-core

// @license.name MIT

// @host localhost:3000
// @BasePath /api/v1

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description JWT Bearer token. Format: "Bearer {token}"

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/infrastructure/bootstrap"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cleanup"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/listener"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/server"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

const shutdownTimeout = 30

func main() {
	if err := run(); err != nil {
		logger.Get().Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Warning: .env file not found or couldn't be loaded: %v\n", err)
	}

	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize logger
	if logErr := logger.Initialize(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output); logErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", logErr)
	}

	// Initialize validation
	validation.Init()

	log := logger.Get()
	log.Info("Starting Go-Core API Server",
		"version", cfg.App.Version,
		"environment", cfg.App.Env,
		"port", cfg.App.Port,
	)

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Run database migrations
	if migErr := database.RunMigrations(db, "platform/migrations"); migErr != nil {
		return fmt.Errorf("failed to run database migrations: %w", migErr)
	}

	// Initialize Casbin service (once, shared between bootstrap and server)
	casbinService, err := authorization.NewCasbinService(cfg, db.DB)
	if err != nil {
		return fmt.Errorf("failed to initialize Casbin service: %w", err)
	}

	// Run bootstrap initialization
	if bsErr := runBootstrap(cfg, db, log, casbinService); bsErr != nil {
		return fmt.Errorf("failed to run bootstrap: %w", bsErr)
	}

	// Initialize OpenTelemetry tracing
	tracingSvc, err := tracing.NewTracingService(cfg)
	if err != nil {
		log.Error("Failed to initialize tracing", "error", err)
	} else {
		log.Info("OpenTelemetry tracing initialized", "endpoint", cfg.OTEL.Endpoint)
	}

	// Initialize Redis (graceful — log error and continue without Redis)
	var redisClient *cache.RedisClient
	rc, redisErr := cache.NewRedisClient(cfg)
	if redisErr != nil {
		log.Warn("Redis not available, running without Redis features", "error", redisErr)
	} else {
		redisClient = rc
	}

	// Initialize outbox listener (LISTEN/NOTIFY)
	outboxListener := listener.NewOutboxListener(cfg.GetDSN())
	outboxListener.Start()

	// Initialize RabbitMQ (graceful — log error and continue without RabbitMQ)
	var rabbitmqService *rabbitmq.RabbitMQService
	outboxRepo := messagingRepo.NewOutboxRepository(db.DB)
	rmqSvc, rmqErr := rabbitmq.NewRabbitMQService(cfg, outboxRepo, outboxListener.SignalCh())
	if rmqErr != nil {
		log.Warn("RabbitMQ not available, running without messaging features", "error", rmqErr)
	} else {
		rabbitmqService = rmqSvc
	}

	// Start identity cleanup goroutine
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go cleanup.RunIdentityCleanup(cleanupCtx, db.DB, log)

	// Start DB connection pool metrics reporter
	go db.StartConnectionMetrics(cleanupCtx)

	// Create Fiber server
	srv, err := server.New(cfg, db, redisClient, rabbitmqService, casbinService)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start admin server (metrics/diagnostics) on internal port
	go func() {
		log.Info("Admin server is running", "port", cfg.Metrics.Port)
		if err := srv.ListenAdmin(); err != nil {
			log.Error("Admin server failed", "error", err)
		}
	}()

	// Start server in goroutine
	listenErr := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%d", cfg.App.Port)
		log.Info("Server is running", "address", addr)
		listenErr <- srv.Listen(addr)
	}()

	// Wait for interrupt signal or server error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	select {
	case err := <-listenErr:
		log.Error("Server failed to start", "error", err)
	case <-quit:
		log.Info("Shutting down server...")
	}

	gracefulShutdown(log, srv, rabbitmqService, outboxListener, tracingSvc, redisClient, db)
	return nil
}

func gracefulShutdown(
	log *logger.Logger,
	srv *server.AppServer,
	rabbitmqService *rabbitmq.RabbitMQService,
	outboxListener *listener.OutboxListener,
	tracingSvc *tracing.TracingService,
	redisClient *cache.RedisClient,
	db *database.DB,
) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout*time.Second)
	defer cancel()

	// Shutdown order (reverse of startup):
	// 1. Stop accepting new work (SSE, notifications, HTTP server)
	// 2. Drain messaging (RabbitMQ, outbox)
	// 3. Close infrastructure (Redis, DB)
	// 4. Flush telemetry (tracing)
	// 5. Close logger last
	srv.StopSSE(ctx)
	srv.StopNotifications(ctx)

	if shutdownErr := srv.ShutdownAdmin(); shutdownErr != nil {
		log.Error("Admin server forced to shutdown", "error", shutdownErr)
	}

	if shutdownErr := srv.ShutdownWithContext(ctx); shutdownErr != nil {
		log.Error("Server forced to shutdown", "error", shutdownErr)
	}

	if rabbitmqService != nil {
		if closeErr := rabbitmqService.Close(); closeErr != nil {
			log.Error("Failed to close RabbitMQ connection", "error", closeErr)
		}
	}

	if outboxListener != nil {
		outboxListener.Close()
	}

	if redisClient != nil {
		if closeErr := redisClient.Close(); closeErr != nil {
			log.Error("Failed to close Redis connection", "error", closeErr)
		}
	}

	if db != nil {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Failed to close database connection", "error", closeErr)
		}
	}

	if tracingSvc != nil {
		if traceErr := tracingSvc.Shutdown(ctx); traceErr != nil {
			log.Error("Failed to shutdown tracing", "error", traceErr)
		}
	}

	if closeErr := logger.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
	}

	log.Info("Server shutdown complete")
}

// runBootstrap initializes the system with default data
func runBootstrap(cfg *config.Config, db *database.DB, log *logger.Logger, casbinService *authorization.CasbinService) error {
	log.Info("Running system bootstrap")

	// Create repositories
	userRepo := repository.NewUserRepository(db.DB)

	// Create and run bootstrap (roles, permissions, admin user)
	bs := bootstrap.NewBootstrap(db.DB, userRepo, casbinService)
	if err := bs.Run(); err != nil {
		return fmt.Errorf("failed to run bootstrap: %w", err)
	}

	// Seed notification template categories and system templates
	if err := bootstrap.SeedTemplates(db.DB); err != nil {
		return fmt.Errorf("failed to seed templates: %w", err)
	}

	log.Info("Bootstrap completed successfully")
	return nil
}

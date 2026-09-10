package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/grpc"
	"github.com/mr-kaynak/go-core/internal/grpc/services"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cache"
	"github.com/mr-kaynak/go-core/internal/infrastructure/cleanup"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	emailInfra "github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/listener"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	"github.com/mr-kaynak/go-core/internal/modules/identity"
	"github.com/mr-kaynak/go-core/internal/modules/notification"
)

func main() {
	if err := run(); err != nil {
		logger.Get().Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load .env file for local development
	if err := godotenv.Load(); err != nil {
		// Plain stderr, NOT logger.Get(): the configured logger is initialized
		// below from cfg, and logger init is first-configuration-wins — a
		// lazy default here would permanently discard the configured logging
		// settings in environment-only deployments (e.g. the Docker image).
		fmt.Fprintf(os.Stderr, "Warning: .env file not found or couldn't be loaded: %v\n", err)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Wire error docs URL into RFC 7807 type URIs
	if cfg.App.ErrorDocsURL != "" {
		errors.SetErrorDocsURL(cfg.App.ErrorDocsURL)
	}

	// gRPC-specific defaults
	if cfg.App.Name == "go-core" {
		cfg.App.Name = "Go-Core gRPC"
	}
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = "go-core-grpc"
	}
	if cfg.Tracing.ServiceName == "" {
		cfg.Tracing.ServiceName = "go-core-grpc"
	}

	// Initialize logger with config values
	if logErr := logger.Initialize(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output); logErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", logErr)
	}
	log := logger.Get()
	log.Info("Starting gRPC server",
		"version", cfg.App.Version, "env", cfg.App.Env)

	// Initialize metrics
	metrics.InitMetrics("go_core")
	metricsService := metrics.GetMetrics()
	metricsService.SetAppInfo(cfg.App.Version, cfg.App.Env, "grpc")

	// Initialize tracing
	tracingService, err := tracing.NewTracingService(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize tracing: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := tracingService.Shutdown(ctx); shutdownErr != nil {
			log.Error("Failed to shutdown tracing", "error", shutdownErr)
		}
	}()

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Preflight, migrate if enabled, then confirm the schema is serveable.
	// The check runs either way: with DB_AUTO_MIGRATE=false there is no
	// migration step to carry it, and starting against a legacy or orphaned
	// schema has to be refused here rather than discovered at the first write.
	if err := app.PrepareSchema(context.Background(), cfg); err != nil {
		// Refusing here is expected operation, not a crash — a legacy database
		// stops startup deliberately — so the pool opened above is released
		// rather than left to the process exit, matching cmd/api's error path.
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Failed to close database connection", "error", closeErr)
		}
		return fmt.Errorf("database schema: %w", err)
	}

	// Initialize email service
	emailSvc, err := emailInfra.NewEmailService(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize email service: %w", err)
	}

	// Initialize Redis according to redis.mode: "required" (default) fails
	// startup so token revocation is never silently skipped; "optional" and
	// "disabled" must be explicit choices. HTTP and gRPC share this policy.
	redisClient, redisErr := cache.NewRedisClientWithPolicy(cfg)
	if redisErr != nil {
		return redisErr
	}

	// Initialize identity services using shared factory
	_, enhancedEmailSvc := notification.WireEnhancedEmail(cfg, db.DB)
	identitySvcs := identity.WireServices(cfg, db.DB, emailSvc, enhancedEmailSvc)
	identitySvcs.SetBlacklist(redisClient)

	// Create gRPC server
	grpcServer, err := grpc.NewServer(cfg, tracingService)
	if err != nil {
		return fmt.Errorf("failed to create gRPC server: %w", err)
	}

	// Set token validator for gRPC auth interceptors
	grpcServer.SetTokenValidator(identitySvcs.TokenService)

	// Initialize outbox listener (LISTEN/NOTIFY)
	outboxListener := listener.NewOutboxListener(cfg.GetDSN())
	outboxListener.Start()

	// Initialize RabbitMQ. An unreachable broker no longer disables messaging:
	// the service always starts, outbox writes go to the database, and the
	// relay retries the connection in the background.
	outboxRepo := messagingRepo.NewOutboxRepository(db.DB)
	rabbitmqService, rmqErr := rabbitmq.NewRabbitMQService(cfg, outboxRepo, outboxListener.SignalCh())
	if rmqErr != nil {
		return fmt.Errorf("failed to initialize RabbitMQ service: %w", rmqErr)
	}

	// Initialize event dispatcher for streaming
	eventDispatcher := events.NewEventDispatcher(rabbitmqService)
	identitySvcs.SetEventPublisher(eventDispatcher)

	// Register gRPC services
	authServiceServer := services.NewAuthServiceServer(
		identitySvcs.AuthService, identitySvcs.UserRepo, identitySvcs.TokenService, cfg,
	)
	userServiceServer := services.NewUserServiceServer(
		identitySvcs.UserService, eventDispatcher,
	)

	pb.RegisterAuthServiceServer(grpcServer.GetServer(), authServiceServer)
	pb.RegisterUserServiceServer(grpcServer.GetServer(), userServiceServer)

	// Start identity cleanup goroutine
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go cleanup.RunIdentityCleanup(cleanupCtx, db.DB, log)

	// Start gRPC server
	if startErr := grpcServer.Start(); startErr != nil {
		cleanupCancel()
		return fmt.Errorf("failed to start gRPC server: %w", startErr)
	}

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	<-quit

	log.Info("Shutting down gRPC server...")
	cleanupCancel()
	gracefulShutdown(log, grpcServer, rabbitmqService, outboxListener, redisClient, db)

	return nil
}

func gracefulShutdown(
	log *logger.Logger,
	grpcServer *grpc.Server,
	rabbitmqService *rabbitmq.RabbitMQService,
	outboxListener *listener.OutboxListener,
	redisClient *cache.RedisClient,
	db *database.DB,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if rabbitmqService != nil {
		if closeErr := rabbitmqService.Close(); closeErr != nil {
			log.Error("Failed to close RabbitMQ connection", "error", closeErr)
		}
	}

	if outboxListener != nil {
		outboxListener.Close()
	}

	shutdownDone := make(chan struct{})
	go func() {
		grpcServer.Stop()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		log.Info("Server shutdown completed")
	case <-ctx.Done():
		log.Error("Server shutdown timed out; forcing stop")
		grpcServer.GetServer().Stop()
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

	if closeErr := logger.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", closeErr)
	}
}

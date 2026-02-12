package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/mr-kaynak/go-core/api/proto"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/grpc"
	"github.com/mr-kaynak/go-core/internal/grpc/services"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	emailInfra "github.com/mr-kaynak/go-core/internal/infrastructure/email"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/events"
	"github.com/mr-kaynak/go-core/internal/infrastructure/messaging/rabbitmq"
	messagingRepo "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/repository"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	notificationService "github.com/mr-kaynak/go-core/internal/modules/notification/service"
	"github.com/spf13/viper"
)

const (
	defaultGRPCPort     = 50051
	defaultDBPort       = 5432
	defaultMaxOpenConns = 25
	defaultMetricsPort  = 9091
	identityCleanupTick = 6 * time.Hour
	revokedKeyRetention = 7 * 24 * time.Hour
)

func main() {
	if err := run(); err != nil {
		logger.Get().Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg := loadConfig()

	// Initialize logger
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
		if shutdownErr := tracingService.Shutdown(context.Background()); shutdownErr != nil {
			log.Error("Failed to shutdown tracing", "error", shutdownErr)
		}
	}()

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Run database migrations
	if migErr := database.RunMigrations(db, "platform/migrations"); migErr != nil {
		return fmt.Errorf("failed to run database migrations: %w", migErr)
	}

	// Initialize repositories
	userRepository := identityRepo.NewUserRepository(db.DB)
	verificationRepo := identityRepo.NewVerificationTokenRepository(db.DB)

	// Initialize email service
	emailSvc, err := emailInfra.NewEmailService(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize email service: %w", err)
	}

	// Initialize template service and enhanced email service
	templateRepo := notificationRepository.NewTemplateRepository(db.DB)
	templateService := notificationService.NewTemplateService(templateRepo)
	enhancedEmailService, err := notificationService.NewEnhancedEmailService(
		cfg, templateService,
	)
	if err != nil {
		log.Error("Failed to initialize enhanced email service", "error", err)
		// Continue with fallback to regular email service
		enhancedEmailService = nil
	}

	// Initialize services
	tokenService := identityService.NewTokenService(cfg, userRepository)
	authSvc := identityService.NewAuthService(
		cfg, db.DB, userRepository, tokenService,
		verificationRepo, emailSvc, enhancedEmailService,
	)

	// Create gRPC server
	grpcServer, err := grpc.NewServer(cfg, tracingService)
	if err != nil {
		return fmt.Errorf("failed to create gRPC server: %w", err)
	}

	// Set token validator for gRPC auth interceptors
	grpcServer.SetTokenValidator(tokenService)

	// Initialize RabbitMQ (graceful — log error and continue without RabbitMQ)
	var rabbitmqService *rabbitmq.RabbitMQService
	outboxRepo := messagingRepo.NewOutboxRepository(db.DB)
	rmqSvc, rmqErr := rabbitmq.NewRabbitMQService(cfg, outboxRepo)
	if rmqErr != nil {
		log.Warn("RabbitMQ not available, running without messaging features", "error", rmqErr)
	} else {
		rabbitmqService = rmqSvc
	}

	// Initialize event dispatcher for streaming
	eventDispatcher := events.NewEventDispatcher(rabbitmqService)

	// Register gRPC services
	authServiceServer := services.NewAuthServiceServer(
		authSvc, userRepository, tokenService, cfg,
	)
	userServiceServer := services.NewUserServiceServer(
		cfg, userRepository, eventDispatcher,
	)

	pb.RegisterAuthServiceServer(grpcServer.GetServer(), authServiceServer)
	pb.RegisterUserServiceServer(grpcServer.GetServer(), userServiceServer)

	// Start identity cleanup goroutine
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	go runIdentityCleanup(cleanupCtx, db, log)

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

	// Stop identity cleanup goroutine
	cleanupCancel()

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Close RabbitMQ connection
	if rabbitmqService != nil {
		if closeErr := rabbitmqService.Close(); closeErr != nil {
			log.Error("Failed to close RabbitMQ connection", "error", closeErr)
		}
	}

	// Stop gRPC server and wait for shutdown completion or timeout.
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

	return nil
}

func loadConfig() *config.Config {
	// Set config file
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		logger.Get().Warn("Failed to read config file, using defaults", "error", err)
	}

	// Set automatic environment variable binding
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("APP_NAME", "Go-Core gRPC")
	viper.SetDefault("APP_ENV", "development")
	viper.SetDefault("APP_VERSION", "1.0.0")
	viper.SetDefault("APP_DEBUG", true)
	viper.SetDefault("GRPC_PORT", defaultGRPCPort)
	viper.SetDefault("GRPC_REFLECTION_ENABLED", false)

	// Database defaults
	viper.SetDefault("DB_HOST", "localhost")
	viper.SetDefault("DB_PORT", defaultDBPort)
	viper.SetDefault("DB_NAME", "go_core")
	viper.SetDefault("DB_USER", "postgres")
	viper.SetDefault("DB_PASSWORD", "postgres")
	viper.SetDefault("DB_SSL_MODE", "disable")
	viper.SetDefault("DB_MAX_OPEN_CONNS", defaultMaxOpenConns)
	viper.SetDefault("DB_MAX_IDLE_CONNS", 5)
	viper.SetDefault("DB_CONN_MAX_LIFETIME", "1h")

	// JWT defaults
	viper.SetDefault("JWT_SECRET", "your-super-secret-jwt-key-change-in-production")
	viper.SetDefault("JWT_EXPIRY", "15m")
	viper.SetDefault("JWT_REFRESH_EXPIRY", "168h")
	viper.SetDefault("JWT_ISSUER", "go-core-grpc")

	// Metrics defaults
	viper.SetDefault("METRICS_ENABLED", true)
	viper.SetDefault("METRICS_PATH", "/metrics")
	viper.SetDefault("METRICS_PORT", defaultMetricsPort)

	// Tracing defaults
	viper.SetDefault("TRACING_ENABLED", true)
	viper.SetDefault("TRACING_SERVICE_NAME", "go-core-grpc")
	viper.SetDefault("TRACING_EXPORTER", "jaeger")
	viper.SetDefault("JAEGER_ENDPOINT", "http://localhost:14268/api/traces")
	viper.SetDefault("OTLP_ENDPOINT", "localhost:4317")

	// Create config object
	cfg := &config.Config{
		App: config.AppConfig{
			Name:    viper.GetString("APP_NAME"),
			Env:     viper.GetString("APP_ENV"),
			Version: viper.GetString("APP_VERSION"),
			Debug:   viper.GetBool("APP_DEBUG"),
		},
		GRPC: config.GRPCConfig{
			Port:              viper.GetInt("GRPC_PORT"),
			ReflectionEnabled: viper.GetBool("GRPC_REFLECTION_ENABLED"),
		},
		Database: config.DatabaseConfig{
			Host:            viper.GetString("DB_HOST"),
			Port:            viper.GetInt("DB_PORT"),
			Name:            viper.GetString("DB_NAME"),
			User:            viper.GetString("DB_USER"),
			Password:        viper.GetString("DB_PASSWORD"),
			SSLMode:         viper.GetString("DB_SSL_MODE"),
			MaxOpenConns:    viper.GetInt("DB_MAX_OPEN_CONNS"),
			MaxIdleConns:    viper.GetInt("DB_MAX_IDLE_CONNS"),
			ConnMaxLifetime: viper.GetDuration("DB_CONN_MAX_LIFETIME"),
		},
		JWT: config.JWTConfig{
			Secret:        viper.GetString("JWT_SECRET"),
			Expiry:        viper.GetDuration("JWT_EXPIRY"),
			RefreshExpiry: viper.GetDuration("JWT_REFRESH_EXPIRY"),
			Issuer:        viper.GetString("JWT_ISSUER"),
		},
		Metrics: config.MetricsConfig{
			Enabled: viper.GetBool("METRICS_ENABLED"),
			Path:    viper.GetString("METRICS_PATH"),
			Port:    viper.GetInt("METRICS_PORT"),
		},
		Tracing: config.TracingConfig{
			Enabled:        viper.GetBool("TRACING_ENABLED"),
			ServiceName:    viper.GetString("TRACING_SERVICE_NAME"),
			Exporter:       viper.GetString("TRACING_EXPORTER"),
			JaegerEndpoint: viper.GetString("JAEGER_ENDPOINT"),
			OTLPEndpoint:   viper.GetString("OTLP_ENDPOINT"),
		},
	}

	return cfg
}

// runIdentityCleanup periodically cleans up expired tokens and revoked API keys
func runIdentityCleanup(ctx context.Context, db *database.DB, log *logger.Logger) {
	ticker := time.NewTicker(identityCleanupTick)
	defer ticker.Stop()

	userRepo := identityRepo.NewUserRepository(db.DB)
	verificationRepo := identityRepo.NewVerificationTokenRepository(db.DB)
	apiKeyRepo := identityRepo.NewAPIKeyRepository(db.DB)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := userRepo.CleanExpiredRefreshTokens(); err != nil {
				log.Error("Failed to clean expired refresh tokens", "error", err)
			}
			if err := verificationRepo.DeleteExpiredTokens(); err != nil {
				log.Error("Failed to clean expired verification tokens", "error", err)
			}
			if err := apiKeyRepo.CleanupRevokedKeys(revokedKeyRetention); err != nil {
				log.Error("Failed to clean revoked API keys", "error", err)
			}
		}
	}
}

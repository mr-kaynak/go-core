package main

import (
	"context"
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
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	identityRepo "github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	identityService "github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/spf13/viper"
)

func main() {
	// Load configuration
	cfg := loadConfig()

	// Initialize logger
	log := logger.Get()
	log.Info("Starting gRPC server", "version", cfg.App.Version, "env", cfg.App.Env)

	// Initialize metrics
	metricsService := metrics.GetMetrics()
	metricsService.SetAppInfo(cfg.App.Version, cfg.App.Env, "grpc")

	// Initialize tracing
	tracingService, err := tracing.NewTracingService(cfg)
	if err != nil {
		log.Error("Failed to initialize tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := tracingService.Shutdown(context.Background()); err != nil {
			log.Error("Failed to shutdown tracing", "error", err)
		}
	}()

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		log.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}

	// Auto-migrate database models
	if err := db.AutoMigrate(); err != nil {
		log.Error("Failed to migrate database", "error", err)
		os.Exit(1)
	}

	// Initialize repositories
	userRepository := identityRepo.NewUserRepository(db.DB)
	verificationRepo := identityRepo.NewVerificationTokenRepository(db.DB)

	// Initialize email service
	emailSvc, err := emailInfra.NewEmailService(cfg)
	if err != nil {
		log.Error("Failed to initialize email service", "error", err)
		os.Exit(1)
	}

	// Initialize services
	tokenService := identityService.NewTokenService(cfg)
	authSvc := identityService.NewAuthService(cfg, userRepository, tokenService, verificationRepo, emailSvc)

	// Create gRPC server
	grpcServer, err := grpc.NewServer(cfg, tracingService)
	if err != nil {
		log.Error("Failed to create gRPC server", "error", err)
		os.Exit(1)
	}

	// Register gRPC services
	authServiceServer := services.NewAuthServiceServer(authSvc, userRepository)
	userServiceServer := services.NewUserServiceServer(userRepository)

	pb.RegisterAuthServiceServer(grpcServer.GetServer(), authServiceServer)
	pb.RegisterUserServiceServer(grpcServer.GetServer(), userServiceServer)

	// Start gRPC server
	if err := grpcServer.Start(); err != nil {
		log.Error("Failed to start gRPC server", "error", err)
		os.Exit(1)
	}

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down gRPC server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop gRPC server
	grpcServer.Stop()

	// Wait for shutdown to complete or timeout
	select {
	case <-ctx.Done():
		log.Error("Server shutdown timed out")
	default:
		log.Info("Server shutdown completed")
	}
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
	viper.SetDefault("GRPC_PORT", 50051)
	viper.SetDefault("GRPC_REFLECTION_ENABLED", true)

	// Database defaults
	viper.SetDefault("DB_HOST", "localhost")
	viper.SetDefault("DB_PORT", 5432)
	viper.SetDefault("DB_NAME", "go_core")
	viper.SetDefault("DB_USER", "postgres")
	viper.SetDefault("DB_PASSWORD", "postgres")
	viper.SetDefault("DB_SSL_MODE", "disable")
	viper.SetDefault("DB_MAX_OPEN_CONNS", 25)
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
	viper.SetDefault("METRICS_PORT", 9091)

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

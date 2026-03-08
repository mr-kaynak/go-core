package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/metrics"
	"github.com/mr-kaynak/go-core/internal/infrastructure/tracing"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

const defaultDeadlineSeconds = 30

// Server represents a gRPC server
type Server struct {
	server         *grpc.Server
	config         *config.Config
	logger         *logger.Logger
	healthServer   *health.Server
	tracingService *tracing.TracingService
}

// ServiceRegistrar allows modules to register their own gRPC services.
type ServiceRegistrar interface {
	RegisterGRPC(server *grpc.Server)
}

// NewServer creates a new gRPC server
func NewServer(cfg *config.Config, tracingService *tracing.TracingService) (*Server, error) {
	// Create server options
	const grpcMaxMsgSize = 10 * 1024 * 1024 // 10MB
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(grpcMaxMsgSize),
		grpc.MaxSendMsgSize(grpcMaxMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Second,
			MaxConnectionAge:      30 * time.Second,
			MaxConnectionAgeGrace: 5 * time.Second,
			Time:                  5 * time.Second,
			Timeout:               1 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithTracerProvider(tracingService.GetProvider()),
		)),
		grpc.ChainUnaryInterceptor(
			RecoveryInterceptor(),
			DefaultDeadlineInterceptor(defaultDeadlineSeconds*time.Second),
			RateLimitInterceptor(float64(cfg.RateLimit.PerMinute), cfg.RateLimit.Burst),
			LoggingInterceptor(),
			MetricsInterceptor(),
			AuthInterceptor(),
			ErrorInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			StreamRecoveryInterceptor(),
			StreamDefaultDeadlineInterceptor(5*time.Minute),
			StreamRateLimitInterceptor(float64(cfg.RateLimit.PerMinute), cfg.RateLimit.Burst),
			StreamLoggingInterceptor(),
			StreamMetricsInterceptor(),
			StreamAuthInterceptor(),
		),
	}

	// Add TLS if configured (required in production and staging)
	if cfg.IsProduction() || cfg.IsStaging() {
		certFile := cfg.GRPC.TLSCertFile
		if certFile == "" {
			certFile = "cert/server.crt"
		}
		keyFile := cfg.GRPC.TLSKeyFile
		if keyFile == "" {
			keyFile = "cert/server.key"
		}
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("gRPC requires TLS in %s: failed to load certificates (cert=%s, key=%s): %w", cfg.App.Env, certFile, keyFile, err)
		}
		opts = append(opts, grpc.Creds(creds))
	} else {
		opts = append(opts, grpc.Creds(insecure.NewCredentials()))
	}

	// Create gRPC server
	grpcServer := grpc.NewServer(opts...)

	// Create health server
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	// Enable reflection in development
	if cfg.IsDevelopment() && cfg.GRPC.ReflectionEnabled {
		reflection.Register(grpcServer)
	}

	return &Server{
		server:         grpcServer,
		config:         cfg,
		logger:         logger.Get().WithFields(logger.Fields{"service": "grpc"}),
		healthServer:   healthServer,
		tracingService: tracingService,
	}, nil
}

// SetTokenValidator sets the token validator for JWT validation
func (s *Server) SetTokenValidator(validator TokenValidator) {
	SetTokenValidator(validator)
	s.logger.Info("Token validator set for gRPC authentication")
}

// RegisterServices registers all gRPC services
func (s *Server) RegisterServices(services ...interface{}) {
	if s == nil || s.server == nil {
		return
	}

	registeredCount := 0
	for idx, svc := range services {
		if svc == nil {
			s.logger.Warn("Skipping nil gRPC service registrar", "index", idx)
			continue
		}

		registrar, ok := svc.(ServiceRegistrar)
		if !ok {
			s.logger.Warn("Skipping unsupported gRPC service registrar",
				"index", idx,
				"type", fmt.Sprintf("%T", svc),
			)
			continue
		}

		registrar.RegisterGRPC(s.server)
		registeredCount++
	}

	s.logger.Info("gRPC services registered", "registered", registeredCount, "provided", len(services))
}

// Start starts the gRPC server
func (s *Server) Start() error {
	port := s.config.GRPC.Port
	if port == 0 {
		port = 50051
		s.logger.Warn("gRPC port not configured, using default", "port", port)
	}

	address := fmt.Sprintf(":%d", port)

	// Create listener
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	s.logger.Info("gRPC server starting", "address", address, "reflection", s.config.GRPC.ReflectionEnabled)

	// Set health status to serving
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Start serving
	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Error("gRPC server failed", "error", err)
		}
	}()

	// Update metrics
	metrics.GetMetrics().SetAppInfo(
		s.config.App.Version,
		s.config.App.Env,
		"grpc",
	)

	s.logger.Info("gRPC server started successfully", "port", port)
	return nil
}

// Stop gracefully stops the gRPC server with a timeout fallback.
func (s *Server) Stop() {
	s.logger.Info("Shutting down gRPC server...")

	// Set health status to not serving
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	// Graceful shutdown with timeout to avoid hanging on long-lived streams
	done := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("gRPC server stopped gracefully")
	case <-time.After(defaultDeadlineSeconds * time.Second):
		s.logger.Warn("gRPC graceful shutdown timed out, forcing stop")
		s.server.Stop()
	}
}

// GetServer returns the underlying gRPC server
func (s *Server) GetServer() *grpc.Server {
	return s.server
}

// SetHealthStatus sets the health status for a service
func (s *Server) SetHealthStatus(service string, serving bool) {
	status := grpc_health_v1.HealthCheckResponse_NOT_SERVING
	if serving {
		status = grpc_health_v1.HealthCheckResponse_SERVING
	}
	s.healthServer.SetServingStatus(service, status)
}

// HealthCheck performs a health check
func (s *Server) HealthCheck(ctx context.Context) error {
	if s == nil || s.healthServer == nil {
		return fmt.Errorf("gRPC health server is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("context is required for gRPC health check")
	}

	resp, err := s.healthServer.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("gRPC health check failed: %w", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("gRPC health status is %s", resp.GetStatus().String())
	}

	return nil
}

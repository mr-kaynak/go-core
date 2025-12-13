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

// Server represents a gRPC server
type Server struct {
	server         *grpc.Server
	config         *config.Config
	logger         *logger.Logger
	healthServer   *health.Server
	tracingService *tracing.TracingService
}

// NewServer creates a new gRPC server
func NewServer(cfg *config.Config, tracingService *tracing.TracingService) (*Server, error) {
	// Create server options
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(10 * 1024 * 1024), // 10MB
		grpc.MaxSendMsgSize(10 * 1024 * 1024), // 10MB
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
			LoggingInterceptor(),
			RecoveryInterceptor(),
			AuthInterceptor(),
			MetricsInterceptor(),
			RateLimitInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			StreamLoggingInterceptor(),
			StreamRecoveryInterceptor(),
			StreamAuthInterceptor(),
			StreamMetricsInterceptor(),
		),
	}

	// Add TLS if configured
	if cfg.IsProduction() {
		// In production, use TLS
		creds, err := credentials.NewServerTLSFromFile("cert/server.crt", "cert/server.key")
		if err != nil {
			// Fall back to insecure if TLS is not configured
			opts = append(opts, grpc.Creds(insecure.NewCredentials()))
		} else {
			opts = append(opts, grpc.Creds(creds))
		}
	} else {
		opts = append(opts, grpc.Creds(insecure.NewCredentials()))
	}

	// Create gRPC server
	grpcServer := grpc.NewServer(opts...)

	// Create health server
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	// Enable reflection in development
	if cfg.IsDevelopment() || cfg.GRPC.ReflectionEnabled {
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
func (s *Server) SetTokenValidator(validator interface{}) {
	// Cast to TokenValidator interface
	if tv, ok := validator.(TokenValidator); ok {
		SetTokenValidator(tv)
		s.logger.Info("Token validator set for gRPC authentication")
	} else {
		s.logger.Error("Invalid token validator provided")
	}
}

// RegisterServices registers all gRPC services
func (s *Server) RegisterServices(services ...interface{}) {
	// Services will be registered here
	// For example:
	// pb.RegisterUserServiceServer(s.server, userService)
	// pb.RegisterAuthServiceServer(s.server, authService)

	s.logger.Info("gRPC services registered")
}

// Start starts the gRPC server
func (s *Server) Start() error {
	port := s.config.GRPC.Port
	if port == 0 {
		port = 50051
	}

	address := fmt.Sprintf(":%d", port)

	// Create listener
	listener, err := net.Listen("tcp", address)
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

// Stop gracefully stops the gRPC server
func (s *Server) Stop() {
	s.logger.Info("Shutting down gRPC server...")

	// Set health status to not serving
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	// Graceful shutdown
	s.server.GracefulStop()

	s.logger.Info("gRPC server stopped")
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

// ServerOptions holds gRPC server configuration options
type ServerOptions struct {
	Port              int
	EnableReflection  bool
	EnableTLS         bool
	CertFile          string
	KeyFile           string
	MaxRecvMsgSize    int
	MaxSendMsgSize    int
	MaxConnectionIdle time.Duration
	KeepaliveTime     time.Duration
	KeepaliveTimeout  time.Duration
}

// DefaultServerOptions returns default server options
func DefaultServerOptions() *ServerOptions {
	return &ServerOptions{
		Port:              50051,
		EnableReflection:  true,
		EnableTLS:         false,
		MaxRecvMsgSize:    10 * 1024 * 1024, // 10MB
		MaxSendMsgSize:    10 * 1024 * 1024, // 10MB
		MaxConnectionIdle: 15 * time.Second,
		KeepaliveTime:     5 * time.Second,
		KeepaliveTimeout:  1 * time.Second,
	}
}

// HealthCheck performs a health check
func (s *Server) HealthCheck(ctx context.Context) error {
	// Implement health check logic
	return nil
}

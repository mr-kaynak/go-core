package grpc

import (
	"context"
	"testing"

	"github.com/mr-kaynak/go-core/internal/core/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
)

type serviceRegistrarStub struct {
	called bool
}

func (s *serviceRegistrarStub) RegisterGRPC(_ *grpc.Server) {
	s.called = true
}

func TestRegisterServices_UsesRegistrarInterface(t *testing.T) {
	srv := &Server{
		server: grpc.NewServer(),
		logger: logger.Get(),
	}
	stub := &serviceRegistrarStub{}

	srv.RegisterServices(stub, struct{}{}, nil)

	if !stub.called {
		t.Fatalf("expected registrar stub to be called")
	}
}

func TestHealthCheck_ReturnsErrorWhenNotServing(t *testing.T) {
	srv := &Server{
		healthServer: health.NewServer(),
		logger:       logger.Get(),
	}
	srv.SetHealthStatus("", false)

	if err := srv.HealthCheck(context.Background()); err == nil {
		t.Fatalf("expected health check error when server is not serving")
	}
}

func TestHealthCheck_ReturnsNilWhenServing(t *testing.T) {
	srv := &Server{
		healthServer: health.NewServer(),
		logger:       logger.Get(),
	}
	srv.SetHealthStatus("", true)

	if err := srv.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected no health check error, got %v", err)
	}
}

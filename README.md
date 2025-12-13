# 🚀 Go-Core: Production-Ready Enterprise Boilerplate

A staff-level Go boilerplate featuring DDD, Modular Monolith architecture, built with Fiber framework. This project provides enterprise-grade infrastructure with authentication, authorization, messaging, and email capabilities out of the box.

## 🎯 Features

### Core Technologies
- **Web Framework**: Fiber v3 (High-performance, Express-inspired)
- **Authorization**: Casbin (RBAC/ABAC support)
- **Authentication**: JWT with Refresh Tokens
- **Database**: PostgreSQL with GORM v2
- **Cache**: Redis
- **Message Queue**: RabbitMQ (with Transactional Outbox Pattern)
- **RPC**: gRPC for synchronous inter-service communication
- **Email**: SMTP integration

### Production Features
- **Error Handling**: RFC 7807 Problem Details standard
- **Distributed Tracing**: OpenTelemetry integration
- **Metrics**: Prometheus metrics exposure
- **Logging**: Structured logging with trace ID correlation
- **Health Checks**: Kubernetes-ready liveness and readiness probes
- **Rate Limiting**: Per-user and per-IP rate limiting
- **Circuit Breaker**: For external service resilience
- **Graceful Shutdown**: Proper cleanup of all connections
- **Configuration**: Environment-based configuration with validation
- **Testing**: Integration tests with Testcontainers

## 📁 Project Structure

```
go-core/
├── cmd/
│   ├── api/              # HTTP API server entrypoint
│   ├── worker/           # RabbitMQ consumer workers
│   └── grpc/             # gRPC server entrypoint
│
├── internal/
│   ├── core/             # Core business logic & shared code
│   │   ├── config/       # Configuration management
│   │   ├── errors/       # Error definitions (RFC 7807)
│   │   ├── logger/       # Structured logging with trace ID
│   │   ├── metrics/      # Prometheus metrics
│   │   ├── tracing/      # OpenTelemetry setup
│   │   └── validation/   # Input validation rules
│   │
│   ├── infrastructure/   # External services & adapters
│   │   ├── cache/        # Redis client & caching logic
│   │   ├── database/     # Database connection & base repository
│   │   ├── email/        # SMTP email service
│   │   ├── mq/           # RabbitMQ client & patterns
│   │   └── storage/      # File storage abstraction
│   │
│   ├── modules/          # Bounded Contexts (DDD)
│   │   ├── identity/     # Authentication & User management
│   │   │   ├── api/      # HTTP handlers
│   │   │   ├── grpc/     # gRPC service implementation
│   │   │   ├── domain/   # Entities & value objects
│   │   │   ├── service/  # Business logic
│   │   │   ├── repository/ # Data access layer
│   │   │   └── dto/      # Data transfer objects
│   │   │
│   │   └── notification/ # Email & notification system
│   │       ├── api/      # HTTP handlers
│   │       ├── consumer/ # RabbitMQ message consumers
│   │       ├── domain/   # Notification entities
│   │       ├── service/  # Notification logic
│   │       └── outbox/   # Transactional outbox implementation
│   │
│   ├── middleware/       # Fiber middleware stack
│   │   ├── auth/         # JWT validation middleware
│   │   ├── casbin/       # Authorization middleware
│   │   ├── cors/         # CORS configuration
│   │   ├── ratelimit/    # Rate limiting middleware
│   │   └── trace/        # Request tracing middleware
│   │
│   └── pkg/              # Reusable utility packages
│       ├── httputil/     # HTTP utilities & helpers
│       ├── retry/        # Retry logic with exponential backoff
│       └── shutdown/     # Graceful shutdown utilities
│
├── platform/             # Platform & deployment configurations
│   ├── migrations/       # Database migration files
│   ├── docker/          # Dockerfile & related configs
│   └── k8s/             # Kubernetes manifests
│
├── scripts/             # Automation & utility scripts
│   ├── init-project.sh  # Project initialization script
│   └── generate/        # Code generation scripts
│
├── test/                # Test suites
│   ├── integration/     # Integration tests with Testcontainers
│   └── e2e/            # End-to-end test scenarios
│
├── api/                 # API definitions
│   ├── openapi/        # OpenAPI 3.0 specifications
│   └── proto/          # Protocol Buffer definitions
│
├── configs/            # Environment configurations
│   ├── dev.yaml        # Development configuration
│   ├── staging.yaml    # Staging configuration
│   └── prod.yaml       # Production configuration
│
├── Makefile            # Build & task automation
├── docker-compose.yml  # Local development environment
├── .golangci.yml      # Go linter configuration
├── .env.example       # Environment variables template
└── go.mod             # Go module definition
```

## 🚀 Quick Start

### Prerequisites
- Go 1.21+
- Docker & Docker Compose
- Make
- PostgreSQL (or use Docker)
- Redis (or use Docker)
- RabbitMQ (or use Docker)

### Create New Project

```bash
# Clone the boilerplate
git clone https://github.com/yourusername/go-core.git my-project
cd my-project

# Initialize with your module name
make init NAME=github.com/mycompany/my-project

# Start development environment
make dev
```

### Development

```bash
# Install dependencies
go mod download

# Start infrastructure services
make docker-up

# Run database migrations
make migrate

# Start API server (hot reload enabled)
make run

# Start worker processes
make run-worker

# Start gRPC server
make run-grpc
```

### Testing

```bash
# Run unit tests
make test

# Run integration tests (requires Docker)
make test-integration

# Run with coverage
make test-coverage

# Run linter
make lint
```

## 📚 Module Architecture

### Identity Module
Handles authentication, authorization, and user management:
- JWT-based authentication with refresh tokens
- Casbin RBAC/ABAC authorization
- User CRUD operations
- Password reset flow
- Session management

### Notification Module
Manages all notification-related operations:
- Email sending via SMTP
- Template management
- Notification preferences
- Event-driven notifications via RabbitMQ
- Transactional outbox pattern for reliability

## 🔧 Configuration

### Environment Variables

```env
# Application
APP_NAME=go-core
APP_ENV=development
APP_PORT=3000
APP_VERSION=1.0.0

# Database
DB_HOST=localhost
DB_PORT=5432
DB_NAME=go_core
DB_USER=postgres
DB_PASSWORD=postgres
DB_SSL_MODE=disable

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# RabbitMQ
RABBITMQ_URL=amqp://guest:guest@localhost:5672/

# JWT
JWT_SECRET=your-secret-key
JWT_EXPIRY=15m
JWT_REFRESH_EXPIRY=7d

# Email (SMTP)
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASSWORD=your-password
SMTP_FROM=noreply@yourdomain.com

# Tracing
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
OTEL_SERVICE_NAME=go-core

# Metrics
METRICS_PORT=9090
```

## 🛠️ Makefile Commands

```bash
make init NAME=<module>  # Initialize new project with module name
make dev                 # Start full development environment
make build               # Build production binaries
make test                # Run all tests
make lint                # Run linter checks
make migrate             # Run database migrations
make seed                # Seed database with test data
make docker-build        # Build Docker images
make docker-up           # Start Docker services
make docker-down         # Stop Docker services
make clean               # Clean build artifacts
```

## 📊 API Documentation

### REST API
- OpenAPI specification: `/api/openapi/spec.yaml`
- Swagger UI available at: `http://localhost:3000/swagger`

### gRPC API
- Protocol Buffers: `/api/proto/*.proto`
- gRPC reflection enabled for development

### Health Endpoints
- `GET /livez` - Liveness probe
- `GET /readyz` - Readiness probe (checks DB, Redis, RabbitMQ)
- `GET /metrics` - Prometheus metrics

## 🔐 Security Features

- JWT authentication with refresh tokens
- Casbin-based fine-grained authorization
- Rate limiting per user/IP
- Input validation and sanitization
- SQL injection prevention via GORM
- XSS protection headers
- CORS configuration
- Security headers middleware

## 📈 Observability

### Logging
- Structured JSON logging
- Trace ID correlation
- Log levels: debug, info, warn, error
- Request/response logging middleware

### Metrics
- HTTP request duration
- Request count by status code
- Database query performance
- RabbitMQ message processing metrics
- Custom business metrics

### Tracing
- OpenTelemetry integration
- Distributed tracing support
- Trace ID propagation
- External service call tracing

## 🧪 Testing Strategy

### Unit Tests
- Domain logic testing
- Service layer testing
- Mock external dependencies

### Integration Tests
- Testcontainers for real dependencies
- API endpoint testing
- Database integration tests
- Message queue integration tests

### E2E Tests
- Complete user flows
- Multi-service scenarios
- Performance testing

## 🚢 Deployment

### Docker
```bash
# Build image
docker build -t go-core:latest .

# Run container
docker run -p 3000:3000 --env-file .env go-core:latest
```

### Kubernetes
```bash
# Apply manifests
kubectl apply -f platform/k8s/

# Check deployment
kubectl get pods -n go-core
```

## 📝 Best Practices

1. **Error Handling**: Use RFC 7807 Problem Details for all API errors
2. **Transactions**: Use transactional outbox for event publishing
3. **Dependency Injection**: Constructor-based DI for testability
4. **Configuration**: Environment-based, validated at startup
5. **Migrations**: Version-controlled, reversible database migrations
6. **Testing**: Minimum 80% code coverage
7. **Documentation**: OpenAPI for REST, protobuf for gRPC

## 🤝 Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 🙏 Acknowledgments

Built with these excellent Go libraries:
- [Fiber](https://gofiber.io/) - Web framework
- [GORM](https://gorm.io/) - ORM library
- [Casbin](https://casbin.org/) - Authorization library
- [Testcontainers](https://golang.testcontainers.org/) - Integration testing
- [OpenTelemetry](https://opentelemetry.io/) - Observability

---

**Made with ❤️ for the Go community**
# go-core

Enterprise-grade Go application skeleton with production-ready infrastructure for building scalable web services.

## Tech Stack

| Layer | Technology |
|-------|-----------|
| HTTP Framework | Fiber v2 |
| RPC | gRPC + Protocol Buffers |
| Database | PostgreSQL 16 + GORM |
| Migrations | Goose v3 |
| Message Queue | RabbitMQ (AMQP 0.9.1) with transactional outbox pattern |
| Cache & Sessions | Redis 7 (go-redis v9) with circuit breaker |
| Auth | JWT (HS256) access + refresh tokens |
| Authorization | Casbin v2 RBAC with role hierarchy and domain support |
| 2FA | TOTP (RFC 6238) with backup codes |
| Monitoring | Prometheus metrics + OpenTelemetry traces + Jaeger |
| Notifications | Email (SMTP), Push (FCM), Webhooks (HMAC-SHA256), In-App (SSE) |
| Storage | S3-compatible (MinIO) + local filesystem |
| API Docs | Swagger/OpenAPI 3.0 via swag (Scalar UI at `/docs`) |
| Linting | golangci-lint v2 |
| CI/CD | GitHub Actions (lint + test with PostgreSQL service) |

## Project Structure

```
cmd/
├── api/main.go              # HTTP server entry point (port 3000)
├── grpc/main.go             # gRPC server entry point (port 50051)
└── migrate/main.go          # Migration CLI

internal/
├── core/                    # Framework essentials
│   ├── config/              # Viper-based configuration (env vars + yaml)
│   ├── logger/              # Structured logging (slog) with sensitive data redaction
│   ├── errors/              # RFC 7807 ProblemDetail errors with typed error codes
│   ├── crypto/              # Cryptographic utilities
│   └── validation/          # Tag-based struct validation (custom: password, username, phone)
│
├── infrastructure/          # Cross-cutting concerns
│   ├── database/            # GORM setup, connection pool, metrics hooks
│   ├── cache/               # Redis: token blacklist (fail-closed), rate limiter (Lua), session cache, SSE bridge
│   ├── messaging/           # RabbitMQ: outbox pattern, PostgreSQL LISTEN/NOTIFY, event dispatcher
│   ├── authorization/       # Casbin: RBAC engine, permission mapping, role hierarchy
│   ├── server/              # Fiber server setup, middleware stack, route registration
│   ├── bootstrap/           # System init: default roles, permissions, admin user (single transaction)
│   ├── metrics/             # Prometheus: HTTP, gRPC, DB, cache, auth, business metrics
│   ├── tracing/             # OpenTelemetry: OTLP exporter, W3C propagation
│   ├── email/               # SMTP email with Go templates
│   ├── push/                # Firebase Cloud Messaging (FCM v1 API)
│   ├── webhook/             # Webhook delivery with HMAC signing + SSRF protection
│   ├── storage/             # S3 + local filesystem (interface-based)
│   ├── circuitbreaker/      # Circuit breaker pattern
│   └── cleanup/             # Background cleanup tasks
│
├── modules/                 # Business modules (each follows domain/repository/service/api layers)
│   ├── identity/            # Users, auth, roles, permissions, API keys, 2FA, audit log
│   ├── notification/        # Multi-channel notifications, SSE streaming, templates
│   ├── blog/                # Posts, categories, tags, comments, engagement, media, SEO, feeds
│   └── user/                # User domain events
│
├── api/                     # Shared HTTP layer
│   ├── middleware/           # Authorization middleware (Casbin enforcement)
│   └── response/            # Paginated responses (offset + cursor-based)
│
├── middleware/auth/         # Authentication middleware (JWT + API key dual support)
├── grpc/                    # gRPC server, services, interceptors
└── test/                    # Test helpers

configs/                     # Prometheus, Jaeger, Casbin model/policy, Grafana dashboards
platform/migrations/         # Goose SQL migrations (00001-00010)
api/proto/                   # Protocol Buffer definitions
docs/                        # Auto-generated Swagger/OpenAPI specs
```

## Architecture Patterns

### Module Structure

Every module follows a consistent 4-layer architecture:

```
modules/<name>/
├── domain/        # Entities, value objects, enums, domain events
├── repository/    # Interface definitions + GORM implementations
├── service/       # Business logic, orchestration, event publishing
└── api/           # HTTP handlers with Swagger annotations + route registration
```

### Dependency Injection

Explicit constructor-based DI — no container, no magic. Optional dependencies use setter methods:

```go
// Constructor with required dependencies
svc := service.NewAuthService(cfg, db, userRepo, tokenService, verificationRepo, emailSvc)

// Optional dependencies via setters (graceful degradation)
if redisClient != nil {
    tokenService.SetBlacklist(cache.NewTokenBlacklist(redisClient))
}
if sseService != nil {
    notificationService.SetSSEService(sseService)
}
```

### Repository Pattern

All repositories are interface-based with transaction support:

```go
type UserRepository interface {
    WithTx(tx *gorm.DB) UserRepository  // Returns new instance wrapping transaction
    Create(user *domain.User) error
    Update(user *domain.User) error
    GetByID(id uuid.UUID) (*domain.User, error)
    // ...
}
```

### Error Handling

RFC 7807 ProblemDetail format with typed error codes:

```go
errors.NewBadRequest("invalid input")
errors.NewNotFound("user", userID.String())
errors.NewConflict("email already exists")
errors.NewRateLimitExceeded(maxRequests)
// Codes: AUTH_001-007, USER_001-004, VAL_001-004, DB_001-005, BLOG_001-010, etc.
```

### Graceful Degradation

Redis, RabbitMQ, and other non-critical services are optional. The app starts and runs in degraded mode if they're unavailable. Token blacklist uses fail-closed semantics (returns blacklisted if Redis is down).

## Adding a New Module

### 1. Define Domain Models

Create `internal/modules/<name>/domain/` with entities:

```go
package domain

type Order struct {
    ID          uuid.UUID      `json:"id" gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
    UserID      uuid.UUID      `json:"user_id" gorm:"type:uuid;not null;index"`
    Status      OrderStatus    `json:"status" gorm:"type:varchar(20);not null;default:'pending'"`
    TotalAmount float64        `json:"total_amount" gorm:"type:decimal(10,2);not null"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type OrderStatus string

const (
    OrderStatusPending   OrderStatus = "pending"
    OrderStatusConfirmed OrderStatus = "confirmed"
    OrderStatusShipped   OrderStatus = "shipped"
    OrderStatusDelivered OrderStatus = "delivered"
    OrderStatusCancelled OrderStatus = "cancelled"
)
```

### 2. Define Repository Interface + Implementation

Create `internal/modules/<name>/repository/`:

```go
// order_repository.go (interface)
package repository

type OrderRepository interface {
    WithTx(tx *gorm.DB) OrderRepository
    Create(order *domain.Order) error
    GetByID(id uuid.UUID) (*domain.Order, error)
    ListByUserID(userID uuid.UUID, page, limit int) ([]*domain.Order, int64, error)
}

// order_repository_impl.go (implementation)
type orderRepositoryImpl struct {
    db *gorm.DB
}

func NewOrderRepository(db *gorm.DB) OrderRepository {
    return &orderRepositoryImpl{db: db}
}

func (r *orderRepositoryImpl) WithTx(tx *gorm.DB) OrderRepository {
    if tx == nil {
        return r
    }
    return &orderRepositoryImpl{db: tx}
}
```

### 3. Create Service Layer

Create `internal/modules/<name>/service/`:

```go
package service

type OrderService struct {
    db        *gorm.DB
    orderRepo repository.OrderRepository
    // Optional dependencies
    sseService *notification.SSEService
    rmq        *rabbitmq.RabbitMQService
}

func NewOrderService(db *gorm.DB, orderRepo repository.OrderRepository) *OrderService {
    return &OrderService{db: db, orderRepo: orderRepo}
}

func (s *OrderService) SetSSEService(svc *notification.SSEService) { s.sseService = svc }
func (s *OrderService) SetRabbitMQ(rmq *rabbitmq.RabbitMQService)  { s.rmq = rmq }
```

### 4. Create HTTP Handlers

Create `internal/modules/<name>/api/`:

```go
package api

type OrderHandler struct {
    orderService *service.OrderService
}

func NewOrderHandler(svc *service.OrderService) *OrderHandler {
    return &OrderHandler{orderService: svc}
}

// RegisterRoutes registers all order routes
func (h *OrderHandler) RegisterRoutes(router fiber.Router, authMw fiber.Handler) {
    orders := router.Group("/orders")
    orders.Use(authMw)
    orders.GET("/", h.ListOrders)
    orders.POST("/", h.CreateOrder)
    orders.GET("/:id", h.GetOrder)
    orders.PUT("/:id", h.UpdateOrder)
}

// @Summary List user orders
// @Tags Orders
// @Security Bearer
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Success 200 {object} response.PaginatedResponse[OrderResponse]
// @Failure 401 {object} errors.ProblemDetail
// @Router /api/v1/orders [get]
func (h *OrderHandler) ListOrders(c *fiber.Ctx) error {
    userID := c.Locals("userID").(uuid.UUID)
    page := c.QueryInt("page", 1)
    limit := response.SanitizeLimit(c.QueryInt("limit", 20), 20)
    // ...
}
```

### 5. Wire Into Server

Add module setup in `internal/infrastructure/server/server.go`:

```go
func setupOrderRoutes(api fiber.Router, db *database.Database, deps sharedDeps) {
    orderRepo := repository.NewOrderRepository(db.DB)
    orderService := service.NewOrderService(db.DB, orderRepo)

    // Wire optional dependencies
    if deps.sseService != nil {
        orderService.SetSSEService(deps.sseService)
    }
    if deps.rmq != nil {
        orderService.SetRabbitMQ(deps.rmq)
    }

    handler := api.NewOrderHandler(orderService)
    handler.RegisterRoutes(api, deps.authMw)
}
```

### 6. Add Database Migration

```bash
make migrate-create NAME=order_module
```

This creates `platform/migrations/NNNNN_order_module.sql`. Write the `-- +goose Up` and `-- +goose Down` sections.

### 7. Add Casbin Permissions (if needed)

Register permissions in `internal/infrastructure/authorization/permission_mapping.go`:

```go
"orders.view":   {Resource: "orders", Action: ActionRead},
"orders.create": {Resource: "orders", Action: ActionCreate},
"orders.update": {Resource: "orders", Action: ActionUpdate},
"orders.delete": {Resource: "orders", Action: ActionDelete},
```

Assign default permissions in `internal/infrastructure/bootstrap/bootstrap.go`.

### 8. Generate Swagger Docs

Add swag annotations to your handlers (see step 4), then run:

```bash
make swagger
```

This regenerates `docs/swagger.json`, `docs/swagger.yaml`, and `docs/docs.go`. The Scalar UI is served at `/docs`.

## Using RabbitMQ (Event Publishing)

### Publishing Events via Outbox

The outbox pattern ensures messages are published reliably within a database transaction:

```go
import "github.com/mr-kaynak/go-core/internal/infrastructure/messaging/domain"

// Inside a service method with database transaction
outboxMsg := &domain.OutboxMessage{
    Type:          "order.created",
    Payload:       payloadJSON,
    CorrelationID: correlationID,
}

// Save to outbox table within the same transaction
err := tx.Create(outboxMsg).Error
// PostgreSQL LISTEN/NOTIFY triggers processing → message sent to RabbitMQ
```

### Consuming Events

Register handlers in RabbitMQ service:

```go
rmq.RegisterHandler("order.created", func(msg *messaging.Message) error {
    // Process the event (send notification, update analytics, etc.)
    return nil
})
```

### Domain Events via Event Dispatcher

For in-process + RabbitMQ hybrid publishing:

```go
dispatcher.Dispatch(ctx, &events.DomainEvent{
    Type:          "order.shipped",
    AggregateID:   orderID.String(),
    CorrelationID: traceID,
    Data:          eventData,
})
```

## Using Casbin (Authorization)

### How It Works

Casbin enforces RBAC policies with role hierarchy (`system_admin > admin > user`):

- **Subjects**: User IDs or `role:{roleName}`
- **Domains**: Tenant boundaries (default: `"default"`)
- **Objects**: API resource paths (supports `keyMatch2` wildcards)
- **Actions**: `create`, `read`, `update`, `delete`, `list`, `manage`, `export`

### Permission Mapping

Permissions are mapped in `internal/infrastructure/authorization/permission_mapping.go`. Each database permission name maps to a Casbin `(resource, action)` pair.

### Authorization Middleware

The middleware at `internal/api/middleware/authorization.go` automatically:
1. Maps HTTP methods to Casbin actions (GET→read, POST→create, PUT→update, DELETE→delete)
2. Enforces policies against the request path
3. Allows own-resource access as fallback

### Adding New Permissions

1. Add mapping in `permission_mapping.go`
2. Assign to roles in `bootstrap.go`
3. Run the app — bootstrap syncs to Casbin on startup

## Using Redis

Redis powers several subsystems with graceful degradation:

| Feature | Key Pattern | Module |
|---------|------------|--------|
| Token Blacklist | `blacklist:{tokenHash}`, `blacklist:user:{userID}` | Auth (fail-closed) |
| Rate Limiting | `ratelimit:{key}` (Lua script for atomic increment) | Middleware |
| Session Cache | `session:{userID}` | Auth |
| SSE Bridge | Redis pub/sub channels | Notifications (cross-instance) |
| View Cooldown | `view:{postID}:{ip}` | Blog engagement |
| Settings Cache | `settings:blog` | Blog |

## Key Make Targets

```bash
# Development
make run                  # Run API server (hot reload with Air if installed)
make run-grpc             # Run gRPC server
make dev                  # Start Docker services + API server
make dev-full             # Start Docker services + API + gRPC servers

# Build
make build                # Build all binaries to ./bin/
make build-api            # Build API server only
make build-grpc           # Build gRPC server only

# Testing
make test                 # Run all tests
make test-coverage        # Run tests with coverage report
make test-integration     # Run integration tests
make test-e2e             # Run end-to-end tests

# Code Quality
make lint                 # Run golangci-lint
make fmt                  # Format code with gofmt
make vet                  # Run go vet

# Database
make migrate              # Run pending migrations
make migrate-down         # Rollback last migration
make migrate-status       # Show migration status
make migrate-create NAME=description  # Create new migration file
make migrate-reset        # Reset database (down all + up all)

# Docker
make docker-up            # Start all infrastructure services
make docker-down          # Stop all services
make docker-logs          # Tail service logs
make docker-build         # Build Docker images
make docker-clean         # Remove containers, volumes, images

# Code Generation
make swagger              # Regenerate Swagger/OpenAPI docs
make proto                # Generate gRPC code from .proto files

# Setup
make install-tools        # Install dev tools (Air, golangci-lint, swag, protoc plugins)
make seed                 # Seed database with sample data
```

## Configuration

All configuration is via environment variables (see `.env.example`). Key sections:

- **App**: `APP_NAME`, `APP_ENV` (development/staging/production), `APP_PORT`
- **Database**: `DATABASE_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`, `DATABASE_SSL_MODE`
- **Redis**: `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`
- **RabbitMQ**: `RABBITMQ_URL`, `RABBITMQ_EXCHANGE`, `RABBITMQ_QUEUE_PREFIX`
- **JWT**: `JWT_SECRET` (min 32 chars), `JWT_EXPIRY`, `JWT_REFRESH_SECRET`, `JWT_REFRESH_EXPIRY`
- **Casbin**: `CASBIN_MODEL_PATH`, `CASBIN_POLICY_PATH`
- **OTEL**: `OTEL_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_TRACES_ENABLED`
- **Storage**: `STORAGE_TYPE` (local/s3), S3 credentials
- **Security**: `SECURITY_BCRYPT_COST`, `SECURITY_ENCRYPTION_KEY` (min 32 chars)

Production/staging enforces: SSL mode, encryption key rotation, HTTPS.

## Testing Conventions

- Test files: `*_test.go` alongside source files
- Use `testify` for assertions where appropriate
- Repository tests use real database (PostgreSQL service in CI)
- Service tests mock repository interfaces
- Handler tests use `httptest` with Fiber's `app.Test()`
- CI runs with `-race` flag for race condition detection

## Coding Conventions

- **UUIDs** for all primary keys (`gen_random_uuid()`)
- **Soft deletes** via `gorm.DeletedAt` (with index)
- **Pagination**: Max 100 items per page, use `response.SanitizeLimit()`
- **Validation**: Tag-based struct validation, validate at handler level
- **Logging**: Structured fields, never log sensitive data (auto-redacted)
- **Metrics**: Record in service layer, namespace `go_core`
- **Errors**: Always return `*errors.ProblemDetail`, never raw strings
- **Transactions**: Use `db.Transaction(fn)` for multi-step operations, repositories support `WithTx(tx)`

## Rules

- Delete planning/task markdown files when work is complete
- Do not create unnecessary documentation files

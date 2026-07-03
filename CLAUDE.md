# go-core

Enterprise-grade Go application skeleton with production-ready infrastructure for building scalable web services.

## Tech Stack

| Layer | Technology |
|-------|-----------|
| HTTP Framework | Fiber v3 |
| RPC | gRPC + Protocol Buffers |
| Database | PostgreSQL 16 + GORM |
| Migrations | Goose v3 |
| Message Queue | RabbitMQ (AMQP 0.9.1) with transactional outbox pattern |
| Cache & Sessions | Redis 7 (go-redis v9) with circuit breaker |
| Auth | JWT (HS256) access + refresh tokens |
| Authorization | Casbin v2 RBAC with role hierarchy and domain support |
| 2FA | TOTP (RFC 6238) with backup codes |
| CAPTCHA | Cloudflare Turnstile / Google reCAPTCHA (pluggable verifier) |
| Monitoring | Prometheus metrics + OpenTelemetry traces + Jaeger |
| Notifications | Email (SMTP), Push (FCM v1), Webhooks (HMAC-SHA256), In-App (SSE) |
| Storage | S3-compatible (MinIO) + local filesystem |
| API Docs | OpenAPI 3.1 via swag + Scalar upgrade, served at `/docs` |
| Linting | golangci-lint v2 |
| CI/CD | GitHub Actions: lint + test (no DB service) + weekly govulncheck |

## Project Structure

```
cmd/
├── api/main.go              # HTTP server entry point (port 3000)
├── grpc/main.go             # gRPC server entry point (port 50051)
└── migrate/main.go          # Migration CLI

internal/
├── core/                    # Framework essentials
│   ├── config/              # Viper-based configuration (env vars + defaults)
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
│   ├── bootstrap/           # System init: roles, permissions, admin user (DB in one tx; Casbin synced post-commit)
│   ├── captcha/             # CAPTCHA verification (Turnstile / reCAPTCHA behind a Verifier interface)
│   ├── metrics/             # Prometheus: HTTP, gRPC, DB, cache, auth, business metrics
│   ├── tracing/             # OpenTelemetry: OTLP exporter, W3C propagation
│   ├── email/               # SMTP email with Go templates
│   ├── push/                # Firebase Cloud Messaging (FCM v1 API, service-account credentials file)
│   ├── webhook/             # Webhook delivery with HMAC signing + SSRF protection
│   ├── storage/             # S3 + local filesystem (interface-based)
│   ├── circuitbreaker/      # Circuit breaker pattern
│   └── cleanup/             # Background cleanup tasks
│
├── modules/                 # Business modules (each follows domain/repository/service/api layers)
│   ├── identity/            # Users, auth, roles, permissions, API keys, 2FA, audit log (+ wire.go DI factory)
│   ├── notification/        # Multi-channel notifications, SSE streaming, templates
│   ├── blog/                # Posts, categories, tags, comments, engagement, media, SEO, feeds
│   └── user/                # User domain events
│
├── api/                     # Shared HTTP layer
│   ├── middleware/          # Authorization middleware (Casbin enforcement)
│   └── response/            # Paginated responses (offset + cursor-based)
│
├── middleware/auth/         # Authentication middleware (JWT + API key dual support)
├── grpc/                    # gRPC server, services, interceptors
└── test/                    # Shared test helpers (single package; no integration/e2e dirs yet)

configs/                     # Prometheus, Jaeger, Casbin model/policy, Grafana dashboards
platform/migrations/         # Goose SQL migrations
api/proto/                   # Protocol Buffer definitions
docs/                        # Auto-generated OpenAPI 3.1 specs (openapi.json, openapi.yaml, docs.go)
```

## Invariants — do not break these

- **Token blacklist is fail-closed.** If Redis is configured but down, `IsBlacklisted` reports true and all tokens are rejected. Security over availability. Both entry points (`cmd/api`, `cmd/grpc`) must wire the blacklist via `identitySvcs.SetBlacklist(redisClient)`.
- **Outbox rows must be written inside the business transaction.** Use `rmq.PublishMessage(ctx, tx, msg)` with the open tx, or carry it through the event dispatcher with `events.ContextWithTx(ctx, tx)`. A nil tx means the event can be lost on a crash between commit and insert.
- **Notification processing is claim-gated.** Every path into `processNotification` goes through `ClaimNotificationForProcessing` (atomic `pending/failed → processing` transition). Never mark a notification `processing` before dispatching it — the claim will fail and the row will be stranded.
- **Casbin writes never run inside DB transactions.** The enforcer's adapter uses its own connection; mixing them causes split-brain on rollback. Bootstrap commits the DB tx first, then runs idempotent Casbin sync (`syncCasbin`).
- **gRPC unary and streaming rate limiters share one bucket** (`SharedRateLimitInterceptors`), and `RateLimit.PerMinute` must be divided by 60 before being used as a token-bucket rate.
- **New config keys must be bound explicitly.** Viper's `AutomaticEnv` does NOT populate struct fields through `Unmarshal` unless the key has a `mustBindEnv` binding or a `SetDefault`. When adding a config field, add a `mustBindEnv("section.key", "ENV_NAME")` line in `config.go` and a row in `.env.example` — otherwise the env var is silently dead.
- **Soft-delete unique indexes must be partial** (`WHERE deleted_at IS NULL`), or deleted rows permanently reserve unique values. See migration `00012` for the pattern.

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

The identity module additionally has a `wire.go` at the module root — a DI factory exposing a `Services` struct (`identity.WireServices(...)`) that entry points and `server.go` use for consistent wiring. Optional dependencies are attached via `Services.SetBlacklist(...)`, `SetSessionCacheWithTTL(...)`, `SetEventPublisher(...)`.

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

Redis, RabbitMQ, and other non-critical services are optional. The app starts and runs in degraded mode if they're unavailable:

| Service | If unavailable |
|---------|---------------|
| Redis | Token blacklist fail-closed (tokens rejected) when configured-but-down; blacklist skipped entirely if never wired; rate limiting and session cache disabled; SSE bridge disabled |
| RabbitMQ | Events accumulate in the outbox table; published when the connection recovers |
| Jaeger/OTEL | Tracing disabled, no functional impact |
| FCM | Push notifications skipped |
| S3/MinIO | Falls back to local storage if configured |

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

Add module setup in `internal/infrastructure/server/server.go` (note: name the router
parameter `router`, not `api` — `api` collides with the handler package alias):

```go
func setupOrderRoutes(router fiber.Router, db *database.Database, deps sharedDeps) {
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
    handler.RegisterRoutes(router, deps.authMw)
}
```

### 6. Add Database Migration

```bash
make migrate-create NAME=order_module
```

This creates `platform/migrations/NNNNN_order_module.sql`. Write the `-- +goose Up` and `-- +goose Down` sections. Unique indexes on soft-deletable tables must be partial: `CREATE UNIQUE INDEX ... WHERE deleted_at IS NULL`.

### 7. Add Casbin Permissions (if needed)

Register permissions in `internal/infrastructure/authorization/permission_mapping.go`:

```go
"orders.view":   {Resource: "orders", Action: ActionRead},
"orders.create": {Resource: "orders", Action: ActionCreate},
"orders.update": {Resource: "orders", Action: ActionUpdate},
"orders.delete": {Resource: "orders", Action: ActionDelete},
```

Assign default permissions in `internal/infrastructure/bootstrap/bootstrap.go`.

### 8. Generate OpenAPI Docs

Add swag annotations to your handlers (see step 4), then run:

```bash
make swagger
```

This runs swag, upgrades the output to OpenAPI 3.1 via `npx @scalar/cli` (requires Node.js), writes `docs/openapi.json` + `docs/openapi.yaml` + `docs/docs.go`, and deletes the swagger 2.0 intermediates. The Scalar UI is served at `/docs`.

## Using RabbitMQ (Event Publishing)

### Publishing Events via Outbox

The outbox pattern ensures messages are published reliably. Always pass the open business transaction so the outbox insert commits atomically with the business write:

```go
// Inside a service method, within db.Transaction(func(tx *gorm.DB) error { ... })
msg := &rabbitmq.Message{
    ID:            uuid.New().String(),
    Type:          "order.created",
    Source:        "order-service",
    Timestamp:     time.Now(),
    Data:          map[string]interface{}{"order_id": orderID.String()},
    CorrelationID: correlationID,
}
if err := s.rmq.PublishMessage(ctx, tx, msg); err != nil {
    return err
}
// PostgreSQL LISTEN/NOTIFY wakes the outbox processor → message sent to RabbitMQ
```

### Consuming Events

Declare a queue and subscribe with a handler (there is no `RegisterHandler` API):

```go
if err := rmq.DeclareQueue("orders.process", []string{"order.created"}); err != nil {
    return err
}
if err := rmq.Subscribe("orders.process", func(msg *rabbitmq.Message) error {
    // Process the event; returning an error nacks the message for retry
    return nil
}); err != nil {
    return err
}
```

### Domain Events via Event Dispatcher

For in-process + RabbitMQ hybrid publishing. When called inside a transaction, carry it in the context so the outbox row is atomic with the business write:

```go
txCtx := events.ContextWithTx(ctx, tx)
dispatcher.Dispatch(txCtx, &events.DomainEvent{
    Type:          "order.shipped",
    AggregateID:   orderID.String(),
    CorrelationID: traceID,
    Data:          eventData,
})
```

Local handlers and channel subscribers fire immediately (before commit) — they must not assume the business data is visible yet.

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
1. Maps HTTP methods to Casbin actions (GET→read, POST→create, PUT/PATCH→update, DELETE→delete)
2. Enforces policies against the request path
3. Allows own-resource access as fallback

### Adding New Permissions

1. Add mapping in `permission_mapping.go`
2. Assign to roles in `bootstrap.go`
3. Run the app — bootstrap syncs to Casbin on startup (post-commit, idempotent)

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

## Using CAPTCHA

`internal/infrastructure/captcha/` provides a `Verifier` interface with Cloudflare Turnstile and Google reCAPTCHA implementations. Configuration: `CAPTCHA_ENABLED`, `CAPTCHA_PROVIDER` (`turnstile`/`recaptcha`), `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY`, `CAPTCHA_TIMEOUT`. Wire the verifier into handlers that need bot protection (registration, login, comments).

## Key Make Targets

```bash
# Development
make run                  # Run API server (hot reload with Air if installed)
make run-grpc             # Run gRPC server
make dev                  # Start Docker services + API server
make dev-full             # Start Docker services + API + gRPC servers

# Build
make build                # Build all binaries (api + grpc + migrate) to ./bin/
make build-api            # Build API server only
make build-grpc           # Build gRPC server only

# Testing
make test                 # Run all tests (-race -cover -short)
make test-coverage        # Run tests with HTML coverage report (no -race)
make test-integration     # Placeholder — test/integration/ is not scaffolded yet
make test-e2e             # Placeholder — test/e2e/ is not scaffolded yet

# Code Quality
make lint                 # Run golangci-lint
make fmt                  # Format code with gofmt
make vet                  # Run go vet

# Database
make migrate              # Run pending migrations
make migrate-down         # Rollback last migration
make migrate-status       # Show migration status
make migrate-create NAME=description  # Create new migration file
make migrate-reset        # Roll back ALL migrations (down only — run `make migrate` to re-apply)
make migrate-redo         # Roll back and re-apply last migration

# Docker
make docker-up            # Start all infrastructure services (uses docker-compose v1 CLI)
make docker-down          # Stop all services
make docker-logs          # Tail service logs
make docker-build         # Build Docker images
make docker-clean         # Remove containers, volumes, images

# Code Generation
make swagger              # Regenerate OpenAPI 3.1 docs (requires Node.js for @scalar/cli)
make proto                # Generate gRPC code from .proto files

# Setup
make install-tools        # Install dev tools (Air, golangci-lint, swag, protoc plugins)
```

Note: The `seed` and `seed-clean` Makefile targets were removed because `cmd/seed` does not exist. Database seeding must be done manually.

## Configuration

All configuration is via environment variables. **`.env.example` is the authoritative list** — every variable there is bound and functional; when adding a new one, bind it with `mustBindEnv` in `internal/core/config/config.go` (see Invariants). Key sections:

- **App**: `APP_NAME`, `APP_ENV` (development/staging/production), `APP_PORT`, `APP_ERROR_DOCS_URL`
- **Database**: `DATABASE_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`, `DATABASE_SSL_MODE`
- **Redis**: `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`
- **RabbitMQ**: `RABBITMQ_URL`, `RABBITMQ_EXCHANGE`, `RABBITMQ_QUEUE_PREFIX`
- **JWT**: `JWT_SECRET` (min 32 chars), `JWT_EXPIRY`, `JWT_REFRESH_SECRET`, `JWT_REFRESH_EXPIRY`
- **Casbin**: `CASBIN_MODEL_PATH`, `CASBIN_POLICY_PATH`
- **OTEL**: `OTEL_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_TRACES_ENABLED`, `OTEL_METRICS_ENABLED`
- **Storage**: `STORAGE_TYPE` (local/s3), S3 credentials
- **Push**: `FCM_ENABLED`, `FCM_CREDENTIALS_FILE` (service-account JSON), `FCM_PROJECT_ID`
- **CAPTCHA**: `CAPTCHA_ENABLED`, `CAPTCHA_PROVIDER`, `CAPTCHA_SITE_KEY`, `CAPTCHA_SECRET_KEY`
- **Security**: `SECURITY_BCRYPT_COST`, `SECURITY_ENCRYPTION_KEY` (min 32 chars)

Production/staging enforces: SSL mode, encryption key rotation, HTTPS, gRPC TLS.

## Testing Conventions

- Test files: `*_test.go` alongside source files
- Use `testify` for assertions where appropriate
- Service tests mock repository interfaces; handler tests use `httptest` with Fiber's `app.Test()`
- **CI has no database service** — tests requiring PostgreSQL must skip gracefully when no DB is available and be verified locally (`make docker-up` + `make test`)
- CI runs `go test -race` and **enforces a minimum 50% total coverage** — the build fails below the threshold
- A separate `security.yml` workflow runs `govulncheck` on push/PR and weekly; a new CVE in a dependency can block PRs

## Coding Conventions

- **UUIDs** for all primary keys (`gen_random_uuid()`)
- **Soft deletes** via `gorm.DeletedAt` (with index); unique indexes must be partial (`WHERE deleted_at IS NULL`)
- **Pagination**: Max 100 items per page, use `response.SanitizeLimit()`
- **Validation**: Tag-based struct validation, validate at handler level
- **Logging**: Structured fields, never log sensitive data (auto-redacted)
- **Metrics**: Record in service layer, namespace `go_core`
- **Errors**: Always return `*errors.ProblemDetail`, never raw strings
- **Transactions**: Use `db.Transaction(fn)` for multi-step operations, repositories support `WithTx(tx)`; outbox writes and multi-row invariants belong inside the transaction

## Rules

- Delete planning/task markdown files when work is complete; never commit them
- Do not create unnecessary documentation files
- Conversation with the maintainer is in Turkish; all code, comments, commits, and docs are in English

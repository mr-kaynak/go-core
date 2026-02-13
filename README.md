# Go-Core

Production-ready enterprise Go application skeleton. Provides core features and scaffolding for any Go project, built for production from day one.

## Features

### Core Stack

- **Fiber v2** — High-performance HTTP framework
- **PostgreSQL + GORM** — Relational database with ORM
- **Redis** — Caching, session storage, distributed rate limiting, pub/sub
- **RabbitMQ** — Async messaging with transactional outbox pattern
- **gRPC** — Synchronous inter-service communication with streaming support
- **JWT + Casbin RBAC** — Authentication and fine-grained role-based access control

### Production Features

- **RFC 7807 Problem Details** — Standardized error responses
- **OpenTelemetry + Jaeger** — Distributed tracing with trace ID correlation
- **Prometheus + Grafana** — Metrics collection and dashboards
- **SSE Real-Time** — Server-Sent Events with channel subscriptions, heartbeat, and multi-instance bridge
- **2FA / TOTP** — Two-factor authentication with QR code setup
- **API Keys** — Service-to-service authentication with scoped keys
- **File Storage** — Local filesystem and S3/MinIO with presigned URLs
- **FCM Push Notifications** — Firebase Cloud Messaging integration
- **Webhooks** — Outbound webhook delivery with signing and retries
- **Email Templates** — Template engine with categories, rendering, bulk operations, import/export
- **Audit Logging** — Structured audit trail for security events
- **Circuit Breaker** — Resilience pattern for external service calls
- **Transactional Outbox** — Reliable event publishing with guaranteed delivery

## Architecture

Domain-Driven Design with Modular Monolith structure. Each module owns its domain, services, repositories, handlers, and DTOs.

```
go-core/
├── cmd/
│   ├── api/                            # REST API server entry point
│   ├── grpc/                           # gRPC server entry point
│   └── migrate/                        # Database migration CLI
│
├── internal/
│   ├── core/
│   │   ├── config/                     # Viper-based configuration
│   │   ├── logger/                     # Structured JSON logging
│   │   ├── errors/                     # RFC 7807 ProblemDetail errors
│   │   ├── validation/                 # Request validation rules
│   │   ├── crypto/                     # Encryption & hashing utilities
│   │   ├── metrics/                    # Metrics collection helpers
│   │   └── tracing/                    # OpenTelemetry setup
│   │
│   ├── middleware/
│   │   ├── auth/                       # JWT authentication
│   │   ├── cors/                       # CORS configuration
│   │   ├── casbin/                     # Casbin authorization enforcement
│   │   ├── ratelimit/                  # Per-IP / per-user rate limiting
│   │   └── trace/                      # Request tracing
│   │
│   ├── infrastructure/
│   │   ├── server/                     # Fiber server setup & route registration
│   │   ├── database/                   # PostgreSQL connection & base repository
│   │   ├── cache/                      # Redis client, token blacklist, session, pub/sub, SSE bridge
│   │   ├── messaging/                  # RabbitMQ client, event dispatcher, outbox listener
│   │   ├── email/                      # SMTP email service
│   │   ├── push/                       # FCM push notifications
│   │   ├── webhook/                    # Webhook delivery service
│   │   ├── storage/                    # Local + S3/MinIO file storage
│   │   ├── authorization/              # Casbin RBAC service
│   │   ├── metrics/                    # Prometheus metrics middleware
│   │   ├── tracing/                    # Jaeger/OTLP exporter
│   │   ├── circuitbreaker/             # Circuit breaker pattern
│   │   └── bootstrap/                  # Dependency injection & app bootstrap
│   │
│   ├── api/middleware/                  # API-layer middleware (authz, tracing)
│   │
│   ├── grpc/
│   │   ├── server.go                   # gRPC server factory
│   │   ├── interceptors.go             # Logging, recovery, auth, metrics, rate limit
│   │   └── services/                   # AuthService, UserService implementations
│   │
│   ├── modules/
│   │   ├── identity/
│   │   │   ├── api/                    # Auth, role, permission, 2FA, API key, policy, upload handlers
│   │   │   ├── service/                # Auth, token, role, API key, audit services
│   │   │   ├── repository/             # User, role, permission, API key, audit log repos
│   │   │   ├── domain/                 # User, role, API key, audit log entities
│   │   │   └── dto/                    # Request/response DTOs
│   │   │
│   │   └── notification/
│   │       ├── api/                    # Notification, SSE, template handlers
│   │       ├── service/                # Notification, SSE, template, email, connection, heartbeat, broadcaster
│   │       ├── repository/             # Notification, template repos
│   │       ├── domain/                 # Notification, template, SSE event entities
│   │       ├── streaming/              # SSE client & message types
│   │       ├── consumer/               # RabbitMQ message consumers
│   │       └── outbox/                 # Transactional outbox implementation
│   │
│   └── pkg/                            # Internal utilities (httputil, shutdown, retry)
│
├── api/proto/                          # Protobuf definitions (auth.proto, user.proto)
├── platform/migrations/                # Goose SQL migration files
├── configs/                            # Casbin model/policy, Prometheus, Grafana dashboards
├── scripts/                            # Project init & code generation scripts
├── docs/                               # Auto-generated Swagger/Scalar docs
├── .github/workflows/                  # CI pipeline (lint + test)
├── Dockerfile                          # Multi-target build (api, grpc, migrate)
├── docker-compose.yml                  # Development services
└── docker-compose.prod.yml             # Production deployment
```

## Quick Start

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- Make
- PostgreSQL

### Setup

```bash
# Clone and enter the project
git clone <repo-url> && cd go-core

# Initialize with your module name
make init NAME=github.com/yourcompany/myproject

# Start infrastructure services (Redis, RabbitMQ, Jaeger, Prometheus, Grafana, MailHog)
make docker-up

# Run database migrations
make migrate

# Start API server with hot reload
make run
```

Or use the single-command dev environment:

```bash
make dev
```

The API will be available at `http://localhost:3000`. API documentation is served at `http://localhost:3000/docs`.

## Development Commands

### Build

| Command | Description |
|---------|-------------|
| `make build` | Build all binaries (api + grpc) |
| `make build-api` | Build API server binary only |
| `make build-grpc` | Build gRPC server binary only |
| `make clean` | Clean build cache and binaries |

### Run

| Command | Description |
|---------|-------------|
| `make run` | Run API server with hot reload (Air) |
| `make run-api` | Run API server directly |
| `make run-grpc` | Run gRPC server |
| `make dev` | Start Docker services + API server |
| `make dev-full` | Start Docker services + API + gRPC |
| `make stop` | Stop all running services |

### Test & Lint

| Command | Description |
|---------|-------------|
| `make test` | Run all tests with coverage |
| `make test-coverage` | Generate HTML coverage report |
| `make test-integration` | Run integration tests (Testcontainers) |
| `make test-e2e` | Run end-to-end tests |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code |
| `make vet` | Run go vet |

### Database

| Command | Description |
|---------|-------------|
| `make migrate` | Run all pending migrations |
| `make migrate-down` | Roll back the last migration |
| `make migrate-status` | Show migration status |
| `make migrate-reset` | Roll back all migrations |
| `make migrate-redo` | Roll back and re-apply last migration |
| `make migrate-create NAME=...` | Create a new migration file |
| `make seed` | Seed database with test data |
| `make seed-clean` | Drop all tables and reseed |

### Docker

| Command | Description |
|---------|-------------|
| `make docker-up` | Start all dev services |
| `make docker-down` | Stop all services |
| `make docker-logs` | Tail docker-compose logs |
| `make docker-clean` | Stop services and remove volumes |
| `make docker-build` | Build all Docker images (api, grpc, migrate) |
| `make docker-build-api` | Build API image only |
| `make docker-push` | Build and push all images to GHCR |
| `make docker-push-api` | Build and push API image to GHCR |

### Code Generation

| Command | Description |
|---------|-------------|
| `make proto` | Generate gRPC code from proto files |
| `make swagger` | Generate Swagger/Scalar documentation |
| `make install-tools` | Install dev tools (Air, golangci-lint, swag, protoc plugins) |

## API Endpoints

### Health & Status (Public)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | API status |
| GET | `/livez` | Liveness probe |
| GET | `/readyz` | Readiness probe (checks DB, Redis, RabbitMQ) |
| GET | `/metrics` | Prometheus metrics |
| GET | `/docs/*` | Scalar API documentation |

### Authentication (Public)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/register` | Register new user |
| POST | `/api/v1/auth/login` | Login with email/password |
| POST | `/api/v1/auth/refresh` | Refresh access token |
| GET | `/api/v1/auth/verify-email` | Verify email with token |
| POST | `/api/v1/auth/resend-verification` | Resend verification email |
| POST | `/api/v1/auth/request-password-reset` | Request password reset |
| POST | `/api/v1/auth/reset-password` | Reset password with token |
| GET | `/api/v1/auth/validate-reset-token` | Validate password reset token |

### Authentication (Protected)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/logout` | Logout (invalidate tokens) |
| POST | `/api/v1/auth/2fa/enable` | Enable 2FA / get QR code |
| POST | `/api/v1/auth/2fa/verify` | Verify TOTP to complete 2FA setup |
| POST | `/api/v1/auth/2fa/disable` | Disable 2FA |

### Users

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/users/profile` | Bearer | Get current user profile |
| POST | `/api/v1/users/avatar` | Bearer | Upload user avatar |
| GET | `/api/v1/admin/users` | Admin | List all users |

### Roles

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/roles` | Bearer | List all roles |
| GET | `/api/v1/roles/:id` | Bearer | Get role by ID |
| POST | `/api/v1/roles` | Admin | Create role |
| PUT | `/api/v1/roles/:id` | Admin | Update role |
| DELETE | `/api/v1/roles/:id` | Admin | Delete role |
| POST | `/api/v1/roles/:id/inherit/:parent_id` | Admin | Set role hierarchy |
| DELETE | `/api/v1/roles/:id/inherit/:parent_id` | Admin | Remove role hierarchy |

### Permissions

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/permissions` | Bearer | List all permissions |
| GET | `/api/v1/permissions/:id` | Bearer | Get permission by ID |
| POST | `/api/v1/permissions` | Admin | Create permission |
| PUT | `/api/v1/permissions/:id` | Admin | Update permission |
| DELETE | `/api/v1/permissions/:id` | Admin | Delete permission |
| GET | `/api/v1/roles/:id/permissions` | Admin | Get role permissions |
| POST | `/api/v1/roles/:id/permissions` | Admin | Add permission to role |
| DELETE | `/api/v1/roles/:id/permissions/:permission_id` | Admin | Remove permission from role |

### API Keys

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/api-keys` | Create API key |
| GET | `/api/v1/api-keys` | List user's API keys |
| DELETE | `/api/v1/api-keys/:id` | Revoke API key |

### Policies (Admin)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/policies` | Add Casbin policy |
| DELETE | `/api/v1/policies` | Remove Casbin policy |
| GET | `/api/v1/policies/reload` | Reload policies from DB |
| POST | `/api/v1/policies/save` | Save policies to DB |
| POST | `/api/v1/policies/users/:user_id/roles` | Add role to user |
| DELETE | `/api/v1/policies/users/:user_id/roles` | Remove role from user |
| GET | `/api/v1/policies/users/:user_id/roles` | Get user roles |
| GET | `/api/v1/policies/users/:user_id/permissions` | Get user permissions |
| GET | `/api/v1/policies/roles/:role/users` | Get users for role |
| POST | `/api/v1/policies/resource-groups` | Add resource to group |
| DELETE | `/api/v1/policies/resource-groups` | Remove resource from group |
| POST | `/api/v1/policies/check` | Check permission |

### Notifications

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/notifications` | List user notifications |
| POST | `/api/v1/notifications` | Create notification |
| PUT | `/api/v1/notifications/:id/read` | Mark as read |
| GET | `/api/v1/notifications/preferences` | Get notification preferences |
| PUT | `/api/v1/notifications/preferences` | Update notification preferences |

### SSE Streaming

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/notifications/stream` | Bearer | Stream notifications via SSE |
| POST | `/api/v1/notifications/stream/subscribe` | Bearer | Subscribe to channels |
| POST | `/api/v1/notifications/stream/unsubscribe` | Bearer | Unsubscribe from channels |
| POST | `/api/v1/notifications/stream/ack` | Bearer | Acknowledge message |
| GET | `/admin/sse/stats` | Admin | SSE statistics |
| GET | `/admin/sse/connections` | Admin | List SSE connections |
| POST | `/admin/sse/broadcast` | Admin | Broadcast message |
| DELETE | `/admin/sse/connections/:clientId` | Admin | Disconnect client |

### Templates

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/templates` | List templates |
| POST | `/api/v1/templates` | Create template |
| POST | `/api/v1/templates/render` | Render template |
| POST | `/api/v1/templates/preview` | Preview template |
| GET | `/api/v1/templates/categories` | List categories |
| POST | `/api/v1/templates/categories` | Create category |
| GET | `/api/v1/templates/most-used` | Get most used templates |
| POST | `/api/v1/templates/system/init` | Initialize system templates |
| POST | `/api/v1/templates/bulk-update` | Bulk update templates |
| GET | `/api/v1/templates/export` | Export templates |
| POST | `/api/v1/templates/import` | Import templates |
| GET | `/api/v1/templates/:id` | Get template by ID |
| PUT | `/api/v1/templates/:id` | Update template |
| DELETE | `/api/v1/templates/:id` | Delete template |
| POST | `/api/v1/templates/:id/clone` | Clone template |

### File Upload

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/files/upload` | Upload file |
| DELETE | `/api/v1/files/*` | Delete file |

### gRPC Services

**AuthService** (port 50051):
- `Register`, `Login`, `RefreshToken`, `Logout`, `VerifyToken`
- `RequestPasswordReset`, `ResetPassword`, `ChangePassword`, `GetPermissions`

**UserService** (port 50051):
- `GetUser`, `ListUsers`, `CreateUser`, `UpdateUser`, `DeleteUser`
- `GetUserByEmail`, `VerifyUser`, `StreamUserEvents` (server streaming)

gRPC reflection is enabled in development for tools like grpcurl.

## Module Details

### Identity Module

Handles all user-facing authentication and authorization:

- **Authentication** — Register, login, logout, email verification, password reset with token-based flows
- **Two-Factor Auth** — TOTP-based 2FA with QR code provisioning, enable/verify/disable lifecycle
- **API Keys** — Scoped API key generation for programmatic access, list and revoke
- **Roles & Permissions** — CRUD for roles and permissions, role hierarchy (inheritance), role-permission assignments
- **Policies** — Casbin policy management, user-role binding, permission checking, resource groups
- **Audit Logging** — Structured audit trail for auth events and security actions
- **File Upload** — Avatar upload, general file upload with local and S3/MinIO backends

### Notification Module

Multi-channel notification system:

- **Email** — SMTP-based delivery via template engine, MailHog for dev testing
- **Push Notifications** — FCM integration for mobile push
- **Webhooks** — Outbound webhook delivery with HMAC signing, timeout, and retry
- **In-App (SSE)** — Real-time Server-Sent Events with channel subscriptions, heartbeat, acknowledgment, and Redis pub/sub bridge for multi-instance deployments
- **Templates** — Category-organized notification templates with rendering, preview, clone, bulk update, import/export
- **Preferences** — Per-user notification channel preferences

## Middleware Stack

Applied in order on every request:

1. **Request ID** — Unique ID for request tracing
2. **Prometheus Metrics** — HTTP request duration, count, status codes
3. **Logger** — Structured request logging (timestamp, status, latency, IP, method, path)
4. **Recovery** — Panic recovery with stack traces in development
5. **Security Headers (Helmet)** — X-Content-Type-Options, X-Frame-Options, etc.
6. **HSTS** — Strict-Transport-Security in production
7. **CORS** — Configurable origins, methods, headers, credentials
8. **Compression** — Brotli/gzip response compression
9. **CSRF Protection** — Optional, token-based CSRF via `X-CSRF-Token` header
10. **Rate Limiting** — Per-IP, Redis-backed distributed rate limiting
11. **JWT Authentication** — Bearer token validation, claims extraction (selective)
12. **Casbin Authorization** — RBAC policy enforcement on admin routes

## Infrastructure Services

### Development (docker-compose.yml)

| Service | Port | Description |
|---------|------|-------------|
| Redis | 6379 | Cache, sessions, rate limiting, pub/sub |
| RabbitMQ | 5672 / 15672 | Message broker / Management UI |
| Jaeger | 16686 / 14317 | Tracing UI / OTLP gRPC collector |
| Prometheus | 9091 | Metrics collection |
| Grafana | 3001 | Dashboards (admin/admin) |
| MailHog | 1025 / 8025 | SMTP trap / Web UI |

### Production (docker-compose.prod.yml)

Adds the application containers:

| Service | Description |
|---------|-------------|
| migrate | Runs migrations before API starts |
| api | REST API + gRPC server (ports 3000, 50051, 9090) |
| redis | Redis 7 Alpine |
| rabbitmq | RabbitMQ 3.12 with management |
| jaeger | Distributed tracing |
| prometheus | Metrics scraping |
| grafana | Dashboard visualization |
| mailhog | Email testing |

All production ports are bound to `127.0.0.1` only. The API container waits for migration completion and healthy dependencies before starting.

## Configuration

Environment-based configuration using Viper. Copy `.env.example` to `.env` and adjust values.

| Category | Key Variables |
|----------|--------------|
| Application | `APP_NAME`, `APP_ENV`, `APP_PORT`, `APP_DEBUG` |
| Database | `DATABASE_HOST`, `DATABASE_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD` |
| Redis | `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, `REDIS_DB` |
| RabbitMQ | `RABBITMQ_URL`, `RABBITMQ_EXCHANGE`, `RABBITMQ_QUEUE_PREFIX` |
| JWT | `JWT_SECRET`, `JWT_EXPIRY`, `JWT_REFRESH_SECRET`, `JWT_REFRESH_EXPIRY` |
| Email | `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM_EMAIL` |
| Casbin | `CASBIN_MODEL_PATH`, `CASBIN_POLICY_PATH` |
| Tracing | `OTEL_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_TRACES_ENABLED` |
| Metrics | `METRICS_PORT`, `METRICS_PATH` |
| Storage | `STORAGE_TYPE` (local/s3), `STORAGE_S3_ENDPOINT`, `STORAGE_S3_BUCKET` |
| Push | `FCM_ENABLED`, `FCM_SERVER_KEY`, `FCM_PROJECT_ID` |
| Webhook | `WEBHOOK_ENABLED`, `WEBHOOK_SECRET`, `WEBHOOK_TIMEOUT`, `WEBHOOK_MAX_RETRIES` |
| Security | `SECURITY_BCRYPT_COST`, `SECURITY_API_KEY_HEADER`, `SECURITY_ENCRYPTION_KEY` |
| Rate Limit | `RATE_LIMIT_PER_MINUTE`, `RATE_LIMIT_BURST` |
| CORS | `CORS_ALLOWED_ORIGINS`, `CORS_ALLOWED_METHODS`, `CORS_ALLOWED_HEADERS` |
| gRPC | `GRPC_PORT`, `GRPC_REFLECTION_ENABLED` |

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on push and PR to `main`:

- **Lint** — golangci-lint v2.9.0
- **Test** — `go build`, `go vet`, `go test -race` with coverage against PostgreSQL 16

## Deployment

### Docker Build

Multi-target Dockerfile builds any entry point:

```bash
# Build API image
docker buildx build --build-arg TARGET=api -t go-core-api .

# Build gRPC image
docker buildx build --build-arg TARGET=grpc -t go-core-grpc .

# Build migration image
docker buildx build --build-arg TARGET=migrate -t go-core-migrate .
```

### Production Deploy

```bash
# Build and push all images to GHCR
make docker-push

# Deploy with production compose
docker compose -f docker-compose.prod.yml up -d
```

Images are pushed to `ghcr.io/mr-kaynak/go-core-{api,grpc,migrate}`.

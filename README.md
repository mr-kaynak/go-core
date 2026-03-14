```
  ██████╗  ██████╗        ██████╗ ██████╗ ██████╗ ███████╗
 ██╔════╝ ██╔═══██╗      ██╔════╝██╔═══██╗██╔══██╗██╔════╝
 ██║  ███╗██║   ██║█████╗██║     ██║   ██║██████╔╝█████╗
 ██║   ██║██║   ██║╚════╝██║     ██║   ██║██╔══██╗██╔══╝
 ╚██████╔╝╚██████╔╝      ╚██████╗╚██████╔╝██║  ██║███████╗
  ╚═════╝  ╚═════╝        ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝
```

# Go-Core

[![CI](https://github.com/mr-kaynak/go-core/actions/workflows/ci.yml/badge.svg)](https://github.com/mr-kaynak/go-core/actions/workflows/ci.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/mr-kaynak/go-core)](https://goreportcard.com/report/github.com/mr-kaynak/go-core) [![codecov](https://codecov.io/gh/mr-kaynak/go-core/branch/main/graph/badge.svg)](https://codecov.io/gh/mr-kaynak/go-core) [![Go Version](https://img.shields.io/github/go-mod/go-version/mr-kaynak/go-core)](https://go.dev/) [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![Go Reference](https://pkg.go.dev/badge/github.com/mr-kaynak/go-core.svg)](https://pkg.go.dev/github.com/mr-kaynak/go-core)

Production-ready enterprise Go application skeleton. Provides core features and scaffolding for any Go project, built for production from day one.

## Features

### Core Stack

- **Fiber v3** — High-performance HTTP framework
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
│   │   ├── crypto/                     # Encryption & hashing utilities
│   │   ├── errors/                     # RFC 7807 ProblemDetail errors
│   │   ├── logger/                     # Structured JSON logging
│   │   └── validation/                 # Request validation rules
│   │
│   ├── middleware/
│   │   └── auth/                       # JWT + API key authentication
│   │
│   ├── infrastructure/
│   │   ├── authorization/              # Casbin RBAC service
│   │   ├── bootstrap/                  # Dependency injection & app bootstrap
│   │   ├── cache/                      # Redis client, token blacklist, session, pub/sub, SSE bridge
│   │   ├── captcha/                    # CAPTCHA verification
│   │   ├── circuitbreaker/             # Circuit breaker pattern
│   │   ├── cleanup/                    # Background cleanup tasks
│   │   ├── database/                   # PostgreSQL connection & base repository
│   │   ├── email/                      # SMTP email service
│   │   ├── messaging/                  # RabbitMQ client, event dispatcher, outbox listener
│   │   ├── metrics/                    # Prometheus metrics middleware
│   │   ├── push/                       # FCM push notifications
│   │   ├── server/                     # Fiber server setup & route registration
│   │   ├── storage/                    # Local + S3/MinIO file storage
│   │   ├── tracing/                    # Jaeger/OTLP exporter
│   │   └── webhook/                    # Webhook delivery service
│   │
│   ├── api/
│   │   ├── helpers/                    # Shared handler utilities
│   │   ├── middleware/                 # API-layer middleware (authz, tracing)
│   │   └── response/                   # Paginated response helpers
│   │
│   ├── grpc/
│   │   ├── server.go                   # gRPC server factory
│   │   ├── interceptors.go             # Logging, recovery, auth, metrics, rate limit
│   │   └── services/                   # AuthService, UserService implementations
│   │
│   ├── modules/
│   │   ├── identity/
│   │   │   ├── api/                    # Auth, role, permission, 2FA, API key, policy, upload handlers
│   │   │   ├── domain/                 # User, role, API key, audit log entities
│   │   │   ├── repository/             # User, role, permission, API key, audit log repos
│   │   │   └── service/                # Auth, token, role, API key, audit services
│   │   │
│   │   ├── blog/
│   │   │   ├── api/                    # Post, comment, category, tag, engagement, media, feed, SEO, admin handlers
│   │   │   ├── domain/                 # Post, comment, category, tag, media, revision, engagement, SSE event entities
│   │   │   ├── repository/             # Post, comment, category, tag, engagement repos
│   │   │   └── service/                # Post, comment, category, engagement, media, content, feed, SEO, slug, read-time services
│   │   │
│   │   ├── notification/
│   │   │   ├── api/                    # Notification, SSE, template handlers
│   │   │   ├── domain/                 # Notification, template, SSE event entities
│   │   │   ├── repository/             # Notification, template repos
│   │   │   ├── service/                # Notification, SSE, template, email, connection, heartbeat, broadcaster
│   │   │   └── streaming/              # SSE client & message types
│   │   │
│   │   └── user/
│   │       └── domain/                 # User domain events
│   │
│   └── test/                           # Test helpers
│
├── api/proto/                          # Protobuf definitions (auth.proto, user.proto)
├── platform/migrations/                # Goose SQL migration files
├── configs/                            # Casbin model/policy, Prometheus, Grafana dashboards
├── docs/                               # Auto-generated Swagger/Scalar docs
├── .github/workflows/                  # CI pipeline (lint + test)
├── Dockerfile                          # Multi-target build (api, grpc, migrate)
├── docker-compose.yml                  # Development services
└── docker-compose.prod.yml             # Production deployment
```

## Quick Start

### Prerequisites

- Go 1.26+
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

### Try It Out

```bash
# Register a new user
curl -X POST http://localhost:3000/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "username": "testuser", "password": "SecurePass123!"}'

# Login
curl -X POST http://localhost:3000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "password": "SecurePass123!"}'

# Authenticated request (use token from login response)
curl http://localhost:3000/api/v1/users/me \
  -H "Authorization: Bearer YOUR_TOKEN_HERE"
```

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

## API Documentation

162+ REST endpoints and gRPC services are fully documented with OpenAPI 3.1. Run the server and visit [`/docs`](http://localhost:3000/docs) for the interactive Scalar UI, or check the raw spec at `docs/openapi.json`.

## How It Works

### Bootstrap & First Run

When the application starts for the first time, the bootstrap system runs inside a single database transaction:

1. **Default roles** are created: `system_admin`, `admin`, `user` with a built-in hierarchy (`system_admin` inherits all `admin` permissions, `admin` inherits all `user` permissions)
2. **Default permissions** are created and assigned to each role (e.g., `users.view`, `users.manage`, `blog.create`, `admin.access`)
3. **Casbin policies** are synced — role-permission mappings are loaded into the in-memory Casbin enforcer
4. **System admin user** is created with a generated secure password printed to stdout — this is the only time the password is visible, and it must be changed on first login

All of this is idempotent — restarting the app won't duplicate roles or users.

### Authentication Flow

```
┌─────────┐     POST /auth/register     ┌──────────┐     Verification Email
│  Client  │ ──────────────────────────► │  Server  │ ──────────────────────► SMTP
│          │                             │          │
│          │     POST /auth/login        │          │     ┌──────────────┐
│          │ ──────────────────────────► │          │ ──► │ Redis Cache  │ (session)
│          │ ◄────── access + refresh ── │          │     └──────────────┘
│          │                             │          │
│          │     Authorization: Bearer   │          │     ┌──────────────┐
│          │ ──────────────────────────► │  JWT     │ ──► │ Blacklist    │ (fail-closed)
│          │                             │  Verify  │     └──────────────┘
└─────────┘                             └──────────┘
```

- **Register** → password hashed (bcrypt) → verification token created → email sent via SMTP → user event dispatched to RabbitMQ
- **Login** → credentials verified → 2FA checked (if enabled) → access token (short-lived, 15m) + refresh token (long-lived, 7d) issued → session cached in Redis
- **Token Refresh** → old refresh token validated and blacklisted → new token pair issued
- **Logout** → all user tokens blacklisted in Redis with TTL matching token expiry
- **Token Blacklist** uses **fail-closed** semantics: if Redis is down, all tokens are treated as blacklisted (security over availability)

### Two-Factor Authentication (2FA)

TOTP-based (RFC 6238) with backup codes:

1. `POST /auth/2fa/enable` → generates TOTP secret + QR code URI + 8 one-time backup codes
2. User scans QR with authenticator app (Google Authenticator, Authy, etc.)
3. `POST /auth/2fa/verify` → verifies TOTP code to confirm setup
4. On subsequent logins, after password validation, server returns a `2fa_required` response — client must provide the TOTP code to complete login
5. Backup codes can be used if the authenticator device is lost (each code is single-use)

### Authorization (Casbin RBAC)

Role-based access control with hierarchy and domain support:

```
system_admin (inherits admin)
  └── admin (inherits user)
       └── user (base role)
```

- **Subjects**: User UUIDs or `role:{roleName}`
- **Objects**: API resource paths with wildcard matching (`/api/v1/users/*`)
- **Actions**: `create`, `read`, `update`, `delete`, `list`, `manage`, `export`

The authorization middleware automatically maps HTTP methods to Casbin actions (`GET→read`, `POST→create`, `PUT→update`, `DELETE→delete`) and enforces policies against the request path. Users can always access their own resources as a fallback.

Policies are stored in PostgreSQL and loaded into memory on startup. Changes via the policy API take effect immediately without restart.

### Event System & Outbox Pattern

Reliable event publishing using the transactional outbox pattern:

```
┌────────────┐    DB Transaction    ┌────────────┐    LISTEN/NOTIFY    ┌────────────┐
│  Service   │ ──────────────────► │ PostgreSQL │ ──────────────────► │  Outbox    │
│            │  write entity +     │            │                     │  Processor │
│            │  outbox message     │            │                     │            │
└────────────┘                     └────────────┘                     └─────┬──────┘
                                                                           │
                                                                    publish to exchange
                                                                           │
                                                                    ┌──────▼──────┐
                                                                    │  RabbitMQ   │
                                                                    │  Exchange   │
                                                                    └─────────────┘
```

1. Business operation and outbox message are saved in the **same database transaction** — no distributed transaction needed
2. PostgreSQL `LISTEN/NOTIFY` triggers the outbox processor immediately
3. Outbox processor publishes the message to RabbitMQ and marks it as processed
4. A fallback polling interval catches any missed notifications
5. Failed messages are retried with exponential backoff up to a configurable max retry count

This guarantees **at-least-once delivery** even if RabbitMQ is temporarily unavailable.

### Graceful Degradation

Non-critical services are optional. The app starts and runs in degraded mode:

| Service | If unavailable |
|---------|---------------|
| Redis | Token blacklist uses fail-closed (all tokens rejected), rate limiting disabled, session cache falls back to DB, SSE bridge disabled |
| RabbitMQ | Events queued in outbox table, processed when connection recovers |
| Jaeger/OTEL | Tracing disabled, no impact on functionality |
| FCM | Push notifications silently skipped |
| S3/MinIO | File uploads fall back to local storage (if configured) |

### Real-Time Notifications (SSE)

Server-Sent Events with multi-instance support:

- Clients connect to `GET /notifications/stream` with JWT auth
- Subscribe to channels (e.g., `user:{id}`, `post:{id}`, `admin`)
- Messages are delivered with acknowledgment support
- Heartbeat keeps connections alive (configurable interval)
- **Multi-instance**: Redis pub/sub bridges SSE across multiple API server instances — a notification published on instance A reaches clients connected to instance B
- Connection manager tracks active clients with metrics (connected count, message throughput)

## Modules

### Identity

Handles authentication, authorization, user management, and audit logging. See [Authentication Flow](#authentication-flow) and [Authorization](#authorization-casbin-rbac) above for details.

Key features: register/login/logout, email verification, password reset, 2FA (TOTP), API keys, role & permission CRUD, Casbin policy management, avatar upload, structured audit trail.

### Blog

Full-featured content management system:

- **Posts** — Plate.js/Slate JSON content with HTML serialization, draft/published/archived workflow, slug generation with Turkish character support, read-time estimation
- **Revisions** — Full version history with content snapshots and diff support
- **Comments** — Threaded with guest support, moderation queue (pending/approved/rejected), XSS sanitization
- **Engagement** — Like toggle, view tracking with cooldown dedup, share tracking by platform, trending algorithm with configurable weights
- **Categories** — Hierarchical tree with recursive CTE-based cycle detection
- **Tags** — Many-to-many with auto-create and popularity ranking
- **Media** — S3 presigned uploads with post ownership enforcement
- **SEO** — JSON-LD schema.org markup, OpenGraph/Twitter meta, canonical URLs
- **Feeds** — RSS 2.0, Atom 1.0, XML sitemap with in-memory caching
- **Real-Time** — SSE broadcasting for new posts, comments, likes, engagement updates

### Notifications

Multi-channel notification system with worker pool:

- **Channels**: Email (SMTP), Push (FCM), Webhooks (HMAC-SHA256 signed), In-App (SSE)
- **Templates**: Category-organized with rendering, preview, clone, bulk update, import/export
- **Preferences**: Per-user channel preferences
- **Processing**: Configurable worker pool with RabbitMQ consumer support for horizontal scaling

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
| Blog | `BLOG_AUTO_APPROVE_COMMENTS`, `BLOG_VIEW_COOLDOWN`, `BLOG_MAX_MEDIA_SIZE`, `BLOG_POSTS_PER_PAGE`, `BLOG_SITE_URL` |
| Security | `SECURITY_BCRYPT_COST`, `SECURITY_API_KEY_HEADER`, `SECURITY_ENCRYPTION_KEY` |
| Rate Limit | `RATE_LIMIT_PER_MINUTE`, `RATE_LIMIT_BURST` |
| CORS | `CORS_ALLOWED_ORIGINS`, `CORS_ALLOWED_METHODS`, `CORS_ALLOWED_HEADERS` |
| gRPC | `GRPC_PORT`, `GRPC_REFLECTION_ENABLED` |

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on push and PR to `main`:

- **Lint** — golangci-lint (latest v2)
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

Images are pushed to `ghcr.io/<owner>/go-core-{api,grpc,migrate}`. If you forked this repository, update the image registry path in `docker-compose.prod.yml` and the `Makefile` to match your GitHub username or organization.

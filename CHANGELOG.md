# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Identity-aware HTTP rate limiting with endpoint classes: a strict per-IP `auth` class (`RATE_LIMIT_AUTH_PER_MINUTE`, default 10/min) for login/register/reset endpoints, per-user and per-API-key buckets for authenticated traffic with IP fallback for anonymous requests, and `X-RateLimit-*` / `Retry-After` response headers
- Interactive project initializer (`make init` / `scripts/init-project.sh`): rewrites the module path and all imports, Docker/GHCR image names, RabbitMQ exchange, metrics namespace, and OpenAPI titles; generates a fresh README and a `.env` with random secrets; optionally resets git history — with built-in `go build` verification
- Tag-triggered release workflow (`release.yml`): test gate, `linux/amd64` + `linux/arm64` binaries, GHCR images, GitHub Release with generated notes
- `DB_AUTO_MIGRATE` flag gating startup migrations (disabled in the production compose, which uses the dedicated migrate container)
- Integration test exercising the real `server.New` wiring and middleware chain (#37)

### Changed
- gRPC keepalive tuned for long-lived streams: `MaxConnectionAge` 30s → 30m, with matching idle/grace/ping windows
- Makefile targets use the `docker compose` v2 CLI (the v1 `docker-compose` binary is EOL)
- Resource DELETE endpoints standardized on `204 No Content`; revocations returning context stay `200` (#36)
- Business modules fully decoupled — zero cross-module imports, narrow consumer-side interfaces with adapters at the composition root (#35)
- Cache layer adopts the shared circuit breaker with bounded half-open probing; Redis pub/sub subscriptions count toward the breaker (#36)
- Casbin bootstrap sync runs idempotently after the DB transaction commits, never inside it (#34)
- CLAUDE.md rewritten as an agent operating manual with explicit invariants; README/CONTRIBUTING/SECURITY/.env.example synced with code reality (#34)

### Fixed
- gRPC entry point now wires the Redis token blacklist — revoked tokens are rejected on gRPC, not just HTTP (#34)
- Notification double delivery eliminated via an atomic claim gate (`pending/failed → processing`) covering concurrent schedulers, pods, and RabbitMQ redeliveries (#34)
- Domain-event outbox rows commit atomically with the business transaction via `events.ContextWithTx` (#34)
- gRPC rate limiting was ~120× too permissive: per-minute quota used as a per-second rate, and unary/stream interceptors kept separate budgets (#34)
- RabbitMQ publisher no longer hangs until its context deadline when the connection is replaced mid-publish (#35)
- SSE client no longer holds its mutex across blocking channel sends, removing a deadlock class and the send-on-closed-channel panic (#35)
- Concurrent registrations with the same email/username return `409 Conflict` instead of an opaque `500` (#35)
- Soft-deleted blog posts no longer reserve their slugs forever (partial unique indexes, migration `00016`) (#35)
- Cursor pagination rejects unsupported sort fields with `400` instead of silently returning wrong pages (#35)
- RFC 7807 `type` URIs use the configured `APP_ERROR_DOCS_URL` instead of a placeholder domain (#35)
- Login persists the refresh token and user counters atomically (#36)
- SMTP startup probe is bounded by the dial timeout and can no longer hang startup (#36)

[Unreleased]: https://github.com/mr-kaynak/go-core/commits/main

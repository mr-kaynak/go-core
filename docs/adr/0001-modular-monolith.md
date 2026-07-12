# ADR-0001: Modular Monolith with DDD-Flavored Modules

- **Status:** Accepted
- **Date:** 2026-07-12

## Context

go-core is a reusable application skeleton, not a product. It is stamped into
new projects via `scripts/init-project.sh` (or the curl bootstrap in
`scripts/create.sh`), which rewrites the module path and project identity and
hands the result to a team that may be two people or twenty. The architecture
we bake in here is the architecture every downstream project starts with, so
it has to be cheap to operate on day one and must not paint teams into a
corner on day five hundred.

The skeleton ships substantial built-in functionality — users/auth/RBAC/2FA,
multi-channel notifications, a blog with engagement and feeds — plus heavy
shared infrastructure: PostgreSQL/GORM, Redis, RabbitMQ with a transactional
outbox, Casbin, Prometheus/OTEL (`internal/infrastructure/`). It exposes two
network surfaces, HTTP (Fiber) and gRPC, and a migration CLI, built from one
codebase as three binaries: `cmd/api/main.go`, `cmd/grpc/main.go`,
`cmd/migrate/main.go`.

We had to choose how to organize business functionality: separate services,
a single flat layered application, or a monolith of self-contained modules.

## Decision

go-core is a **modular monolith**. Business functionality lives in
self-contained modules under `internal/modules/` — `identity`, `blog`,
`notification`, and `user` — each following the same internal layering:

```
modules/<name>/
├── domain/        # entities, value objects, enums, domain events
├── repository/    # interfaces + GORM implementations, WithTx(tx) support
├── service/       # business logic, orchestration, event publishing
└── api/           # HTTP handlers + route registration
```

(`user` is currently domain-only — user domain event types in
`internal/modules/user/domain/event.go`; `notification` adds a `streaming/`
layer for SSE.)

Rules that make the modules "modular" rather than merely foldered:

1. **No cross-module imports.** No package under `internal/modules/<a>/`
   imports from `internal/modules/<b>/`. This holds today across all four
   modules and is the primary boundary invariant.
2. **Composition happens at the edges.** Entry points and
   `internal/infrastructure/server/server.go` wire modules together.
   The identity module exposes a DI factory,
   `internal/modules/identity/wire.go` (`identity.WireServices(...)` returning
   a `Services` struct with setters like `SetBlacklist`), used identically by
   the HTTP server (`server.go`) and the gRPC server (`cmd/grpc/main.go`).
   `internal/modules/notification/wire.go` plays the same role for email
   services.
3. **Cross-module needs go through adapters or events, not imports.** When
   identity needs notification behavior, `server.go` attaches small adapter
   types (e.g. `userLanguageResolverAdapter`,
   `notificationPrefCreatorAdapter`) that satisfy interfaces identity defines
   itself. Asynchronous integration goes through the RabbitMQ outbox and the
   event dispatcher in `internal/infrastructure/messaging/`.
4. **Shared concerns are infrastructure, not module code.** Config, logging,
   errors, validation (`internal/core/`) and DB/cache/messaging/authz
   (`internal/infrastructure/`) are dependencies modules point *down* to;
   modules never depend sideways.

The HTTP and gRPC binaries are two deployables of the same monolith: they
share the modules, the database, and the wiring conventions — they differ
only in transport.

## Alternatives Considered

### (a) Microservices (identity-service, blog-service, notification-service…)

*Pros:* independent deployment and scaling per capability; hard boundaries
enforced by the network; per-service technology freedom; failure isolation.

*Cons:* every stamped project would start with N deployables, service
discovery, inter-service auth, distributed transactions or sagas where the
outbox currently suffices, and N times the CI/observability surface. The
skeleton's graceful-degradation design (Redis/RabbitMQ optional, see
`CLAUDE.md`) exists so a new project runs with `make dev` and one Postgres;
microservices would make the operational *floor* higher than the ceiling
most downstream projects ever need, front-loading cost for scaling problems
that do not exist yet in greenfield projects. **Rejected.**

### (b) Flat layered monolith (`handlers/`, `services/`, `models/`)

*Pros:* simplest possible structure; least indirection; no wiring factories;
familiar to every Go developer.

*Cons:* boundaries decay by default — a flat `services/` package invites any
service to call any other, and `models/` becomes a shared mud of structs.
With the amount of functionality this skeleton already ships (identity alone
spans users, auth, roles, API keys, 2FA, audit), a flat layout would be
hundreds of files per layer with no ownership seams. Critically, it destroys
the extraction path: there is no unit you can later lift out into a service,
because nothing is self-contained. **Rejected.**

## Consequences

Positive:

- **One deploy story, low ops floor.** A stamped project runs as one HTTP
  binary (plus optional gRPC), one database, one migration stream
  (`platform/migrations/`). Cross-module writes can share a transaction and
  the outbox (`rmq.PublishMessage(ctx, tx, msg)`) — no distributed
  consistency machinery.
- **Copy-paste extensibility.** New modules follow a mechanical recipe
  (documented in `CLAUDE.md`, "Adding a New Module"): domain → repository →
  service → api → wire in `server.go`. Uniform shape keeps review cheap.
- **Extraction escape hatch.** Because a module owns its domain, repository
  interfaces, and services, and communicates outward only via
  composition-root adapters and events, promoting one to a separate service
  is a bounded refactor: replace the adapter/event seams with RPC and move
  the tables — not an archaeology project.
- **Both transports reuse business logic.** `identity.WireServices` gives
  HTTP and gRPC identical service instances and invariants (e.g. both wire
  the fail-closed token blacklist).

Negative / accepted risks:

- **Boundaries are convention, not compilation.** Go will happily compile a
  direct `modules/blog` → `modules/identity` import; nothing but review (and
  a greppable rule: no `internal/modules/<other>` imports) prevents erosion.
  A lint rule (e.g. depguard) would harden this and is worth adding.
- **Shared database schema.** All modules migrate through one Goose stream
  into one PostgreSQL schema; a module can, in principle, query another's
  tables, and extraction later requires untangling data ownership.
- **Single failure domain and shared scaling.** A CPU-hungry blog feed and
  latency-sensitive auth share a process; you scale the whole binary, not
  the hot module.
- **Composition root grows.** `server.go` centralizes wiring and adapter
  glue (~1,100 lines today) and grows with every module — the tax for
  keeping modules import-clean.

We accept these because the skeleton's job is to give new projects the
cheapest architecture that preserves optionality — which this is.

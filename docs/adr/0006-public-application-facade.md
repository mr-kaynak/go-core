# ADR-0006: Public application facade and consumer module contract

- **Status:** Accepted
- **Date:** 2026-09-15

## Context

go-core was distributed by copying: `scripts/create.sh` clones the repository,
deletes `.git`, and rewrites the module identity. Every consumer owns a full
copy of the core sources, so a core fix never reaches consumers without manual
porting, and the entire implementation lives under `internal/` — impossible to
import as a dependency (the 2026-09-08 technical review, findings B04/B05).

Phase B replaces copy-distribution with **versioned-dependency distribution**:
a consumer application depends on `github.com/mr-kaynak/go-core` like any Go
module, keeps its own module path and domain code, and receives core fixes
through dependency updates.

## Decision

### Public surface (deliberately narrow)

Three root packages are public; everything else stays `internal/`:

| Package | Surface |
|---|---|
| `app` | `LoadConfig`, `Config` (alias), `New`, `Run`, `Shutdown`, `FiberApp` (testing), `WithModules`, `Module`/`ModuleContext`/`Permission`/`EventPublisher` (aliases), `ContextWithTx`, `Action` constants |
| `identity` | `Principal`, `FromContext` — the auth-middleware locals contract, now public API |
| `coremigrations` | `FS()` — core SQL migrations as an embedded filesystem, so a module consumer can migrate an empty database without a source checkout |

The contract types physically live in `internal/platform/modcontract` (a
dependency-neutral package) and are re-exported through public aliases;
`internal/infrastructure/server` consumes `modcontract` directly, which is what
breaks the app↔server import cycle. Go's `internal` rule restricts import
*paths*, not types, so public aliases to internal types are usable by
consumers while the implementation stays closed.

### Module contract (v0)

`Module` = `Name()` + `Permissions()` + `Register(*ModuleContext)`.

- **Permissions** carry Casbin object **path patterns** (`Objects []string`),
  plural because keyMatch2's `/api/v1/orders/*` does not match the bare
  collection path. Registration is atomic per batch: unique names, unique
  `(object, action)` pairs across the instance-scoped `PermissionRegistry`
  (core + all modules), closed action set. Module permissions enter the
  registry before bootstrap derives DB permission rows.
- **Register** wires routes/services only — no goroutines, no external
  connections. This keeps the failure contract simple: a failing Register
  aborts construction, and `app.New` releases every resource it started, in
  reverse order. A half-registered application never starts.
- **Events**: `EventPublisher.Dispatch` is outbox-backed; `AggregateID` is
  preserved in the persisted record; wrap the context with `app.ContextWithTx`
  for transactional atomicity. Local handlers still run before commit —
  documented, not changed, in this phase.

### Lifecycle and process-global ownership

- `app.Run()` returns a fatal error from **either** listener (API or admin)
  after graceful shutdown. This is a deliberate fix: the old `cmd/api` main
  returned `nil` after a listen failure and only logged admin-listener errors.
- Logger, config global, and the otel tracer provider follow one rule:
  *the first successful owner installs; an App only closes what it owns; a
  failed `New` never touches process globals.* The tracer provider is created
  deferred and published globally only at `New`'s success commit-point; the
  owning App's `Shutdown` flushes (never stops) a globally-published provider
  because global consumers (e.g. redisotel) outlive it. Final teardown belongs
  to `cmd/*` main. Config's global uses last-successful-load-wins (equivalent
  guarantee: race-free, never a torn or partially-validated config).
- `cmd/api` is the facade's first consumer (dogfooding): its main is now
  `LoadConfig` → `New` → `Run`.

### Permission runtime synchronization

The database is the source of truth for the **managed policy subset**
(role policies derivable from registry mappings, excluding the reserved
code-seeded defaults — one shared constant guarantees seeding and resync can
never diverge). Grant writes DB first, then all policy objects (failure →
best-effort rollback + error); revoke removes policies first (fail-closed,
absent-as-success), then the DB row. Grant/revoke/resync are serialized per
instance; resync is two-way (adds missing, removes stale — including deleted
roles' policies) and runs at bootstrap and on demand. Retries converge without
resync (idempotent DB grant, absent-tolerant removes). Guarantees apply to the
mutating instance's enforcer only: **Phase B supports a single serving
instance**; multi-replica policy propagation is Phase C.

## Recorded compromises (exit paths tracked in the Phase B plan)

1. **Fiber and GORM appear in the public contract** (`ModuleContext.Router/
   Auth/Authz/DB`, `FiberApp`). An abstraction layer would delay delivery for
   speculative flexibility; before v1, fiber-touching types consolidate under a
   transport package and any break lands in v1.
2. **`app.Config` aliases the internal config type** — every field is
   effectively public. Supported configuration is what `.env.example`
   documents; a narrowed public config schema lands before v1.
3. **Module migrations are absent by design** — they arrive in Phase C with
   core/app history separation, as a separate optional interface
   (`Migrations() fs.FS` via type assertion), never by widening `Module`.
4. **Single migration history continues** (00001–00016, embedded); history
   separation is Phase C.

## Consequences

- A consumer compiles against `app`/`identity`/`coremigrations` only; the
  repo's CI enforces this with an import-boundary test over `examples/` and a
  throwaway external-module compile gate on every PR.
- Core fixes reach consumers as dependency updates; the Phase C release
  pipeline (tags, release manifest, upgrade tests) builds on this surface.
- The K1b acceptance test pins the end-to-end contract: a consumer module's
  own permission drives real enforcement (login → runtime grant → 200 on
  collection+item, registered sibling 403, runtime revoke → 403) with no core
  source edits and no restarts.

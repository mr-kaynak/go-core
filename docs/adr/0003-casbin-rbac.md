# ADR-0003: Casbin for RBAC Authorization

- **Status:** Accepted
- **Date:** 2026-07-12

## Context

go-core needs endpoint-level authorization on top of JWT authentication: multiple
roles with a strict hierarchy (`system_admin` > `admin` > `user`), a tenant/domain
slot for future multi-tenancy, and rules that match wildcard API paths
(`/api/v1/users/*`) rather than one rule per route. Permissions are business data —
admins grant and revoke them at runtime through the identity module
(`internal/modules/identity/service/permission_service.go`), so the source of truth
for *who has what* lives in PostgreSQL (`roles`, `permissions`, `role_permissions`).
The enforcement engine, however, must answer "may subject S do action A on object O"
on every request without a database round-trip.

We needed a policy engine that: evaluates in-process and in-memory, expresses role
inheritance and path wildcards declaratively, persists policy durably, and stays
small enough that a single Go service can own it end to end.

## Decision

Use **Casbin v2** (`github.com/casbin/casbin/v2`) with a domain-aware RBAC model and
the GORM adapter (`gorm-adapter/v3`), wrapped in
`internal/infrastructure/authorization/casbin_service.go`.

**Model.** The model is embedded as a string in `getModelText()` (mirrored for
reference in `configs/casbin_model.conf`). Requests are `(sub, dom, obj, act)`;
policies add an effect column (`eft`) so explicit `deny` overrides `allow`
(`some(where (p.eft == allow)) && !some(where (p.eft == deny))`). The matcher
supports a `*` object wildcard, resource groups (`g2`), `keyMatch2` path wildcards,
and a `manage` super-action that satisfies any requested action. Three grouping
relations exist: `g` (user→role per domain), `g2` (resource grouping per domain),
`g3` (role inheritance, domain-agnostic). Note: `configs/casbin_policy.csv` and the
`CASBIN_MODEL_PATH`/`CASBIN_POLICY_PATH` config keys are not consumed by the
enforcer — policies live in the database via the adapter, not in the CSV.

**Persistence.** The GORM adapter stores policies in a `casbin_rule` table using the
application's own PostgreSQL, with `EnableAutoSave(true)` so every policy mutation
is written through immediately. The service guards the enforcer with a `sync.RWMutex`;
`EnforceWithRoles` runs a user check plus one check per JWT role under a single read
lock, because Casbin `g` user→role links are only maintained for a subset of users —
roles come from token claims and are checked directly against `role:`-prefixed
policies.

**Permission mapping.** Database permission names map to Casbin `(resource, action)`
pairs in `internal/infrastructure/authorization/permission_mapping.go`
(e.g. `"users.view"` → `(/api/v1/users/*, read)`). Each entry must produce a unique
pair — duplicate pairs collide, so removing one permission would silently revoke the
other. The mapping is consumed in two places: bootstrap's `syncPermissionsToCasbin`
(full hydration from `role_permissions`) and the identity permission service's
best-effort `syncPermissionToCasbin` on runtime grant/revoke.

**HTTP enforcement.** `internal/api/middleware/authorization.go` maps HTTP methods
to actions (GET/HEAD/OPTIONS→`read`, POST→`create`, PUT/PATCH→`update`,
DELETE→`delete`, anything else→`manage`, which effectively denies unless an
admin-level policy exists), enforces against the request path in the server-derived
`default` domain (never a client-supplied header), and on denial falls back to an
own-resource check for read-only requests (`isUserOwnResource`, exact path-segment
matching on `/users/{id}` and `/users/me`).

**Transaction separation (invariant).** Casbin writes never run inside application
DB transactions. The adapter uses its own connection, so a Casbin write issued
"inside" a GORM transaction commits independently — on rollback the business rows
disappear but the policy rows survive (split-brain). Bootstrap
(`internal/infrastructure/bootstrap/bootstrap.go`) therefore runs in two phases:
all role/permission/admin-user writes in a single DB transaction, then, post-commit,
`syncCasbin()` adds the `g3` role inheritance chain (`role:system_admin` →
`role:admin` → `role:user`), replays `role_permissions` into policies, and binds the
system admin user to `role:system_admin`. Every sync step is idempotent, so a crash
between commit and sync self-heals on the next startup.

**gRPC.** The gRPC path authenticates JWTs and injects `userID`/`roles` into the
context (`internal/grpc/interceptors.go`) but does **not** enforce Casbin. Services
apply coarse checks instead — `requireAdmin` / `requireSelfOrAdmin` in
`internal/grpc/services/user_service.go`. This is a deliberate simplification: the
gRPC surface is small and admin-oriented; Casbin objects are HTTP paths, which do
not map cleanly onto RPC method names.

## Alternatives Considered

**Hand-rolled role checks in middleware.** `if hasRole("admin")` sprinkled across
handlers is the fastest start, but rules scatter and drift: adding a role means
auditing every handler, hierarchy is re-implemented ad hoc, and there is no single
place to answer "what can this role do". The gRPC services show exactly this pattern
at small scale — acceptable for two services, unmaintainable for the whole HTTP API.

**OPA/Rego.** Strictly more expressive (ABAC, context-aware policies) and
battle-tested, but it brings a second policy language (Rego), a separate evaluation
engine to embed or deploy as a sidecar, and a policy-bundle distribution story. For
a skeleton whose authorization needs are RBAC + hierarchy + path wildcards, that is
deployment and cognitive weight without a payoff.

**Pure DB-driven permission checks per request.** Joining
`users → roles → role_permissions → permissions` on every request keeps one source
of truth but puts the database on the hot path of every API call; making it fast
requires a cache, and cache invalidation on grant/revoke re-creates the sync problem
Casbin's in-memory model already solves — without giving us deny-overrides,
wildcards, or inheritance for free.

## Consequences

**Positive**

- One declarative model covers hierarchy (`g3`), tenancy (`dom`), wildcards
  (`keyMatch2`), and deny-overrides; per-request checks are in-memory and lock-cheap.
- The HTTP middleware is generic: new routes are covered by existing resource
  wildcards, and new permissions only require a `permission_mapping.go` entry plus a
  bootstrap role assignment (per CLAUDE.md's "Adding New Permissions").
- Policies persist in the same PostgreSQL as everything else — no extra
  infrastructure, and bootstrap can rebuild Casbin state from `role_permissions`
  idempotently on every startup.

**Negative**

- Policy lives in two places: `role_permissions` (source of truth) and `casbin_rule`
  (enforcement copy). The runtime grant/revoke sync is best-effort (failures are
  logged, not surfaced), so a failed sync leaves drift until the next bootstrap
  replay. The two-phase bootstrap is a discipline every contributor must keep — a
  Casbin write inside a DB transaction reintroduces split-brain.
- The enforcer is per-process memory with no Casbin watcher wired: a policy change
  made through one instance persists to the DB but other instances serve stale
  policy until `ReloadPolicy()` or restart. Single-instance deployments are
  unaffected; horizontal scaling needs a watcher or reload strategy.
- The `permission_mapping.go` uniqueness invariant (`(resource, action)` pairs must
  not collide) is enforced by convention and tests, not by the type system.
- Two authorization idioms coexist (Casbin on HTTP, role constants on gRPC), and the
  Casbin matcher DSL plus `g`/`g2`/`g3` semantics are a real learning curve compared
  to a plain `if` check.

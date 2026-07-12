# ADR-0005: Explicit constructor-based dependency injection with optional-dependency setters

- **Status:** Accepted
- **Date:** 2026-07-12

## Context

go-core is a reusable skeleton: it is stamped into new projects
(`scripts/init-project.sh`) and then maintained by teams who did not write it.
The dependency-injection approach is the first thing every newcomer touches —
adding a module means constructing repositories, services, and handlers and
wiring them into the server. Whatever mechanism we choose must be readable by
someone seeing the codebase for the first time, debuggable with plain Go
tooling, and free of build-step or framework prerequisites.

Two properties of this codebase shape the decision:

1. **Graceful degradation is a core feature.** Redis, RabbitMQ, SMTP, and
   OTEL are all optional at runtime. `cmd/api/main.go` logs a warning and
   continues when Redis is unreachable (`NewRedisClient` error → nil client);
   services must tolerate absent infrastructure rather than fail to construct.
   A DI mechanism that treats every dependency as required fights this design.

2. **Two entry points share one service graph.** Both the HTTP server
   (`internal/infrastructure/server/server.go`) and the gRPC server
   (`cmd/grpc/main.go`) need identity services (tokens, auth, users) built the
   same way. Duplicated hand-wiring in two places had a real failure mode: the
   gRPC path could forget to attach the token blacklist, silently weakening
   auth on one transport.

## Decision

We wire dependencies by hand, with three conventions:

1. **Required dependencies go through constructors.** Every service takes its
   hard dependencies as `New*` parameters, e.g.
   `service.NewAuthService(cfg, db, userRepo, tokenService, verificationRepo, emailSvc, enhancedEmailSvc)`
   in `internal/modules/identity/wire.go`. Repositories are interface-based
   (`repository.UserRepository`) so services are mockable in tests.

2. **Optional dependencies go through `Set*` methods.** Services hold a nil
   field until wired, e.g. `TokenService.SetBlacklist(...)` in
   `internal/modules/identity/service/token_service.go`, which guards use with
   `if s.blacklist != nil`. When the field is nil the feature degrades per the
   table in CLAUDE.md: nil Redis → blacklist/session-cache/rate-limiting
   skipped, nil RabbitMQ → events accumulate in the outbox table, nil FCM →
   push skipped. (Once a blacklist *is* wired but Redis is down, checks are
   fail-closed — see `internal/infrastructure/cache/token_blacklist.go`.)

3. **Per-module DI factories where wiring is shared.** The identity module
   exposes `identity.WireServices(cfg, db, emailSvc, enhancedEmailSvc)
   *Services` in `internal/modules/identity/wire.go` — a `Services` struct
   holding `TokenService`, `AuthService`, `UserService`, and `UserRepo`, plus
   nil-tolerant setters `SetBlacklist(rc)`, `SetSessionCacheWithTTL(rc, cfg)`,
   and `SetEventPublisher(ep)`. Both entry points call it:
   `setupRoutes` in `internal/infrastructure/server/server.go` (line ~373) and
   `cmd/grpc/main.go` (line ~126). The factory is the single source of truth,
   so HTTP and gRPC cannot drift in how identity services are assembled.

At the server level, each module gets a `setup<Module>Routes` function in
`server.go` (`setupIdentityRoutes`, `setupNotificationRoutes`,
`setupBlogRoutes`, `setupAdminRoutes`) that constructs the module's graph and
registers routes. There is no container and no code generation; `go.mod` has
no dependency on google/wire, uber/fx, or uber/dig.

## Alternatives Considered

**google/wire (compile-time codegen).** Wire gives a compile-time-checked
graph: a missing provider is a build error, not a runtime nil. But it adds a
codegen step (`wire_gen.go`, build tags, regeneration on every signature
change) and a layer of magic that newcomers to a skeleton must learn before
their first module. Wire also models dependencies as required-by-default;
expressing "Redis may be nil and the service still works" requires cleanup
functions or provider-set gymnastics that obscure the graceful-degradation
story. For a graph of this size (~4 modules, one shared factory), the codegen
overhead buys little.

**uber/fx or uber/dig (runtime reflection containers).** These resolve the
graph via reflection at startup: wiring errors surface as runtime panics with
container-framed stack traces, lifecycle is managed by fx hooks instead of
plain `main`, and every service signature becomes an implicit contract with
the container. That is framework lock-in in the load-bearing skeleton layer —
exactly where a template should be most boring. Debugging "why is this
dependency nil" through dig's reflection is strictly harder than reading
`setupRoutes` top to bottom.

**Global singletons / service locator.** A package-level `GetTokenService()`
hides the dependency graph entirely: nothing in a function signature says what
it needs, tests must mutate global state, and parallel tests race. The
codebase already pays for one deliberate exception (`logger.Get()`); extending
that pattern to stateful services would make the skeleton untestable in
exactly the way `WithTx`-style repository interfaces are designed to avoid.

## Consequences

Positive:

- **The graph is grep-able.** Every dependency of every service appears in a
  constructor call in `server.go`, `wire.go`, or `cmd/grpc/main.go`. There is
  nothing to decompile mentally — new-module wiring is copy-paste-modify, as
  documented step-by-step in CLAUDE.md ("Adding a New Module").
- **Graceful degradation is idiomatic, not fought.** Nil optional deps are the
  mechanism, and the `Services` setters (`SetBlacklist` returns early on nil)
  make degraded startup a one-liner at each entry point.
- **No toolchain surface.** No codegen step to forget, no container version to
  upgrade, no framework to strip when the skeleton is stamped into a project.
- **Entry-point consistency by construction.** `WireServices` exists precisely
  because HTTP and gRPC previously each hand-built identity services; the
  factory makes divergence (e.g. gRPC missing the blacklist) structurally hard.

Negative (real tradeoffs, accepted):

- **Wiring mistakes surface at runtime, not compile time.** Forgetting
  `identitySvcs.SetBlacklist(redisClient)` in an entry point compiles cleanly
  and runs with the blacklist silently disabled — the exact class of bug
  google/wire eliminates. We mitigate with the shared factory, a CLAUDE.md
  invariant ("Both entry points must wire the blacklist"), and startup log
  lines ("Token blacklist enabled (Redis)") that make absence observable.
- **Nil-checks proliferate at call sites.** Every optional dependency imposes
  `if s.blacklist != nil` / `if s.sseService != nil` guards inside services,
  and a forgotten guard is a nil-pointer panic.
- **Setters permit half-constructed objects.** A `Services` value is valid
  before its setters run, so ordering matters and dead setter stubs can
  linger (`Services.SetSessionCache` in `wire.go` is a documented no-op kept
  only to steer callers to `SetSessionCacheWithTTL`).
- **`setupRoutes` grows linearly with modules.** `server.go` is already
  700+ lines; hand-wiring concentrates there. We accept this as long as each
  module's wiring stays inside its own `setup*Routes` function.

Revisit if the module count or optional-dependency matrix grows to the point
where hand-wiring errors recur despite the factory pattern; google/wire is the
natural escalation path since the constructor-based style is already
wire-compatible.

# Backend extension contract

## Composition and lifecycle

`app.LoadConfig() (*app.Config, error)` reads environment/config files and validates the full application. It does not load `.env`; example entry points call `godotenv.Load` explicitly. `app.New(cfg, app.WithModules(...), app.WithMigrations(...))` creates infrastructure, verifies/migrates schemas, bootstraps registered permissions and wires modules. `Run()` handles HTTP plus the admin listener and shutdown signals; `Shutdown(ctx)` is available for explicit hosts. `FiberApp()` exposes test access.

An `app.Module` implements `Name() string`, `Permissions() []app.Permission`, `Register(*app.ModuleContext) error`. Names are unique. Context provides `Config`, GORM `DB`, `Router` already grouped at `/api/v1`, `Auth`, `Authz`, `Events`, and slog `Logger`. `Register` must not open connections or spawn goroutines; return wiring failures to abort startup.

## Endpoint recipe

1. Define request/response DTOs separately from writable model fields. Parse/validate identifiers and bounded user inputs. Do not bind ownership, status or privileged fields directly from arbitrary JSON.
2. Define the route group's permissions and exact HTTP action. Both collection and item path patterns are required: `Objects: []string{"/api/v1/projects", "/api/v1/projects/*"}`. Names such as `projects.view` and each `(object, action)` pair must be unique in the registry. GET → `app.ActionRead`, POST → `ActionCreate`, PUT/PATCH → `ActionUpdate`, DELETE → `ActionDelete`. `ActionList` does not automatically authorize GET.
3. Register `ctx.Router.Group("/projects", ctx.Auth, ctx.Authz)` then routes relative to that group.
4. Read `caller, ok := identity.FromContext(c)`; return unauthorized when `!ok`. Principal fields are UUID `UserID`, `Username`, `Email`, `Roles`, `Permissions`, `AuthMethod` (`jwt` or `api_key`). An authenticated principal can legitimately have no grants.
5. Scope data access to `caller.UserID` or a server-validated tenant. Apply the predicate to both count and list, item reads and mutations. Decide whether inaccessible rows intentionally return 404 to avoid disclosing existence.
6. Bound list sizes and establish stable ordering with a tie-breaker. Add pagination if the product must browse beyond the startup example's latest 50.
7. Return explicit status and JSON shapes. New JSON endpoints should not return text status labels. Core SDK supports legacy text responses, but that is not a reason to introduce inconsistent new contracts.

New module permissions are bootstrapped for `system_admin`; other roles need explicit grants. Refresh the client's profile/session after grant changes when checking UI visibility. Backend permission state and UI presentation are distinct.

## Data and migrations

Optional `app.MigrationProvider` implements `MigrationSource() app.MigrationSource`. Source fields: permanent `Name` matching `^[a-z][a-z0-9_]{0,30}$` and `FS fs.FS` with SQL at root. `core` is reserved. Use `//go:embed migrations/*.sql` then `fs.Sub(files, "migrations")`. Register module sources automatically via `WithModules` OR as application sources via `WithMigrations`; avoid duplicate registration.

Core runs first, application sources then module sources follow registration order. Each source has `<name>_schema_versions`, core has `core_schema_versions`; numbering is independent. Start consumer SQL at `00001`, use `-- +goose Up`, append later versions. The runner rejects nontransactional annotations and top-level transaction control. Do not edit deployed SQL or rename a source. Do not use GORM AutoMigrate as the production schema lifecycle.

Keep identical source sets in the serving binary and migration job. The core-only CLI cannot discover consumer SQL. Use `app.LoadMigratorConfig()` (database settings only), `app.NewMigrator(cfg, consumerSources...)`, `Up(ctx)`, `Close()`. Core is automatically included. `Report(ctx)` is read-only; `CheckServing(ctx)` checks serving admission with only database config and creates no tables; `UpOne(ctx, source)` returns `app.ErrNothingPending` when current. `PrepareSchema(ctx, cfg, sources...)` supplies admission/migration/checks for a custom entry point. See operations before baseline or tolerance changes.

## Atomic business events

```go
err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    if err := tx.Create(&row).Error; err != nil { return err }
    return publisher.Dispatch(app.ContextWithTx(ctx, tx), "project.created", row.ID.String(), map[string]any{
        "project_id": row.ID.String(), "name": row.Name,
    })
})
```

Use the actual request context and module DB/publisher. Return errors; a failing dispatch must not report creation as committed. `AggregateID` is retained. Local handlers run before commit and cannot assume other DB connections see the data. Durable external delivery requires idempotent consumers and is not exactly once.

## Tests and failure behavior

Test real middleware for anonymous 401 and no-grant 403; mock principal injection proves handler behavior only. Test cross-owner read/update/count, malformed IDs and bounded input, dispatch failure rollback and successful restart persistence. PostgreSQL migration behavior needs PostgreSQL, not SQLite. For concurrent token/session changes use fixed identities and overlapping requests; do not hide collisions with sleeps. If auth storage fails, inspect error propagation in the actual entry point; never weaken authorization to make a consumer pass.

Read current `app/*.go`, `identity/principal.go`, `examples/startup`, the consumer guide and ADR-0006/0007 in the pinned core checkout before implementing unfamiliar operations.

## Backend-only and custom-client authentication

No core-ui dependency is required. Use the pinned OpenAPI contract (`docs/openapi.json`, interactive `/docs` on the running API) for all payloads. Relative to `/api/v1`:

- `POST /auth/login` takes `email` and `password`. A normal response contains `user`, `access_token`, `refresh_token`, `expires_at`.
- If `requires_two_factor` is true, this is not an authenticated session. Send `two_factor_token` and `code` to `POST /auth/2fa/validate` to obtain the full token pair. Do not confuse this actual field with `2fa_required`.
- Protected endpoints use `Authorization: Bearer <access_token>`. `POST /auth/refresh` takes `refresh_token` and rotates the token pair. Serialize refresh per session and replace both tokens atomically; do not replay an already consumed refresh token.
- Keep token storage appropriate to the client runtime (for example OS secure storage on native mobile), never hardcode tokens or ship service secrets. Preserve a valid refresh credential across transient network/server errors; rejected credentials require login. Do not blindly retry business mutations.

Core verification and password-reset emails construct `${APP_FRONTEND_URL}/verify-email?token=...` and `/reset-password?token=...`; welcome emails also link to `/login`. Backend-only does not remove these user-facing recovery flows. For a mobile-only product explicitly choose verified universal/app links handled by the mobile app, an existing website, or a minimal hosted recovery page. Match the core path/query contract and test the token exchange through the API. This does not require the core-admin UI; do not silently scaffold a React app or treat an arbitrary custom URI scheme as already supported by configuration validation.

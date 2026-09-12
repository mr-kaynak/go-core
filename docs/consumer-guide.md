# Build a product on go-core

Use go-core as a dependency for identity, roles, permissions, notifications and the application lifecycle. Your repository owns the product's routes, data and SQL. Core updates then arrive through `go.mod` and `go.sum`.

Start with the [runnable startup tutorial](../examples/startup/README.md). It pairs with [core-ui's startup admin](https://github.com/mr-kaynak/core-ui/tree/main/apps/startup). For the smallest possible module, read [minimal/orders](../examples/minimal/orders/orders.go); its data is intentionally lost on restart.

## What lives where

```text
your-product/
  backend/
    main.go                    app.New + your modules
    projects/                  routes, services, repositories, embedded SQL
    go.mod                     pinned go-core dependency
  admin/
    src/main.tsx               createAdminApp + branding + your modules
    src/projects-module.tsx    product screens calling /projects
    package.json               pinned sdk/ui/admin package versions

go-core                        shared identity, RBAC, lifecycle, migration runner
core-ui                        shared SDK, components, admin shell
```

A module is code inside one service process. A separate service has its own executable, deployment, configuration and data ownership. `WithModules` does not create a networked microservice or share login sessions across independent databases. Begin with modules in one product service; split deployment when the product actually needs it. There is no built-in tenant isolation in this example.

## Public extension reference

| Entry point | Contract |
| --- | --- |
| `app.LoadConfig() (*app.Config, error)` | Reads process environment/config files; validates application settings. Does not load `.env`; the example calls godotenv explicitly. |
| `app.New(cfg, options...) (*app.App, error)` | Validates module/migration registration, prepares infrastructure/schema, bootstraps permissions, registers routes. |
| `app.WithModules(modules ...app.Module)` | Registers consumer modules in order; module names must be unique. |
| `App.Run() error` | Serves HTTP, handles SIGINT/SIGTERM and shutdown. Returns startup/listener failures. |
| `App.Shutdown(context.Context) error` | Explicit shutdown for hosts/tests. |
| `App.FiberApp()` | Fiber handle, useful for HTTP tests; does not replace startup wiring. |
| `app.Module` | `Name() string`, `Permissions() []app.Permission`, `Register(*app.ModuleContext) error`. |
| `app.ModuleContext` | `Config`, `DB` (GORM), `Router` (`/api/v1`), `Auth`, `Authz`, `Events`, `Logger` (`*slog.Logger`). |
| `app.Permission` | Unique `Name`, full-path `Objects []string`, `Action`; each object/action pair must also be unique. |
| `identity.FromContext(c)` | Returns `(*identity.Principal, bool)`: `UserID`, `Username`, `Email`, `Roles`, `Permissions`, `AuthMethod`. |
| `app.EventPublisher.Dispatch(ctx, eventType, aggregateID, data)` | Publishes via the outbox; `data` is `map[string]any`; returns an error. |
| `app.ContextWithTx(ctx, tx)` | Carries the open GORM transaction into event publication. |

The supported action constants are `ActionCreate`, `ActionRead`, `ActionUpdate`, `ActionDelete`, `ActionList`, `ActionManage`, `ActionExport`, `ActionImport`. Ordinary GET requests require `ActionRead`, including collection lists.

### Migration reference

| Entry point | Contract |
| --- | --- |
| `app.MigrationSource{Name, FS}` | Name matches `^[a-z][a-z0-9_]{0,30}$`; `core` is reserved. SQL files must be at the filesystem root. |
| `app.MigrationProvider` | Optional `MigrationSource() app.MigrationSource` on a module; automatically collected by `app.New`. |
| `app.WithMigrations(sources...)` | Registers application-owned sources in addition to module-owned ones. Do not register the same source twice. |
| `app.CoreMigrationSource()` / `coremigrations.FS()` | Core's embedded migration source / SQL filesystem. |
| `app.LoadMigratorConfig()` | Reads database-only configuration; no JWT, SMTP or encryption secrets required. |
| `app.MigratorConfigFromApp(cfg)` | Uses an already loaded application configuration for a migration job. |
| `app.NewMigrator(cfg, sources...)` | Includes core automatically; consumers pass only their own sources. Call `Close()` when done. |
| `Migrator.Up(ctx)` | Applies pending core SQL, then consumer sources in registration order. |
| `Migrator.UpOne(ctx, source)` | Applies one next migration; returns `app.ErrNothingPending` if current. |
| `Migrator.Report(ctx)` | Read-only history classification; creates no tables. |
| `Migrator.CheckServing(ctx)` | Read-only serving admission under the migration lock, using database-only configuration. Startup still rechecks. |
| `Migrator.Sources()` | Registered source names, core first. |
| `Migrator.Baseline(ctx, plan, dryRun)` | Converts legacy history after checks; see the [baseline runbook](migration-baseline-runbook.md). |
| `app.PrepareSchema(ctx, cfg, sources...)` | Startup admission, optional migration, then serving admission for custom entry points. |

Core uses `core_schema_versions`; each consumer source uses `<name>_schema_versions`. Sources may independently start at `00001`. Every migration needs `-- +goose Up`. SQL runs transactionally; `NO TRANSACTION` and top-level transaction-control statements are rejected. Each migration commits separately: a later failure does not roll back earlier successful migrations. Keep source names and deployed files immutable. See [ADR-0007](adr/0007-separated-migration-histories.md) and the [inventory lock](migration-inventory-lock.md).

## How to start an independent repository

First run the tutorial in this checkout. Then copy **only** `examples/startup/` to your new backend directory. Do not copy its `.env` or local data.

```bash
# In the new backend directory, before a go.mod exists:
go mod init example.com/acme/backend
# Set GOCORE_REF to a reviewed commit or published release tag first.
go get "github.com/mr-kaynak/go-core@${GOCORE_REF:?set a reviewed core ref}"
```

Change the projects import in `main.go` from `github.com/mr-kaynak/go-core/examples/startup/projects` to `example.com/acme/backend/projects`. Leave the `go-core/app` and `go-core/identity` imports intact. Run `go mod tidy`, `go test ./...`, and the tutorial's setup/run commands.

For local core development, `go mod edit -replace=github.com/mr-kaynak/go-core=/absolute/path/to/go-core` is useful. Remove that replacement before distributing the consumer and pin a real commit/tag. Local replacement proves the API boundary, not registry availability.

## How to add your next service module

1. Copy the example's `projects/` directory into your consumer as `billing/` or another domain. Rename Go types, routes, table names, permission names and event types.
2. Choose a unique, permanent migration source name. Add SQL for your own tables; never modify core's embedded SQL.
3. Register `billing.New()` beside existing modules in `app.WithModules`. Add its migration source to your dedicated migration command too. Keep the two source sets identical.
4. Use both middleware handlers on the route group. A collection permission needs both `/api/v1/billing` and `/api/v1/billing/*` when collection and item endpoints exist.
5. Give a non-system role the required permissions through the core admin UI. `system_admin` receives registered permissions during bootstrap; other roles do not receive your new permissions automatically.
6. Register the matching UI route **and** menu permission. Add ownership/tenant checks to data access, independently of RBAC.

Verify an unauthenticated caller gets 401, a caller without the grant gets 403, the intended role succeeds, and another owner's data stays hidden. Verify event failure rolls back the write and a successful write survives restart.

For an independently deployed service, create another consumer executable and its database/configuration. The facade currently exposes event **publication**, not a general subscription/worker lifecycle or reusable gRPC server factory. Implement externally owned integrations explicitly; do not reach into core internals to obtain them.

## How to migrate and upgrade

In development, `DB_AUTO_MIGRATE=true` applies core plus registered consumer SQL during startup. For deployment, build and run your consumer's migration command first, then serve with `DB_AUTO_MIGRATE=false`. The startup check still rejects a legacy, pending or incompatible schema. The core-only `cmd/migrate` cannot discover your consumer's embedded SQL automatically.

Before updating dependencies, back up the database and rehearse on a restore. Pin the intended core ref, run tests and migrations, and verify an existing user's data and login through the matching UI packages. Update all three UI packages as a tested set. Consult the baseline runbook for a legacy `goose_db_version` database; startup does not silently baseline it. Do not enable migration tolerances merely to suppress a startup error.

Local tags and tarball builds are not evidence of a published release or a proven production upgrade. Check the actual release artifacts and compatible version set before treating an update as routine.

## Troubleshooting

| Symptom | Check / fix |
| --- | --- |
| Config validation fails | Run from the example directory, run `bash setup.sh`, check required DB/JWT/SMTP settings. Placeholder secrets in the root `.env.example` are rejected. |
| Redis unreachable | Start the example Compose stack. Keep `REDIS_MODE=required`; optional/disabled modes change revocation guarantees. |
| Auth works but projects return 404 | Start `examples/startup`, not core's `cmd/api`. The latter does not register projects. |
| A valid user gets 403 | Assign `projects.view` / `projects.create` to their role; check full-path object patterns and HTTP action. |
| Another user's known ID returns 404 | Expected: the example scopes reads to the authenticated owner. |
| Browser reports CORS/network failure | Use API `http://localhost:3300/api/v1` and UI origin `http://localhost:5174`; origins include the port. |
| Missing table after adding a module | Check `MigrationProvider`, root-level SQL, source registration in both API and migration job. |
| Legacy/pending/unknown history error | Read the reported classification and runbook; don't delete histories or renumber migrations. |
| Project list stops at 50 | Deliberately bounded demo list; add pagination for a product that needs browsing beyond 50. |

## Installed agent workflow

Install the [core-platform skill](../README.md#install-the-ai-agent-skill) for self-contained scenario selection and implementation guidance across backend and UI. The skill includes its own references and can discover the correct repositories from a new workspace.

## A task prompt for your agent

```text
Read AGENTS.md, docs/consumer-guide.md and examples/startup first.
In my consumer repository, add a billing module through the public app facade.
Use caller-owned rows, a permanent billing migration source, RBAC for collection
and item routes, and transactional billing.created events. Register the same
sources in API and migration job. Add a core-admin module using useCoreClient;
guard route and menu separately. Do not modify core internals or applied SQL.
Test denied access, cross-owner access, validation, rollback and persistence.
Report commands, outcomes and skipped checks; do not claim untested deployment.
```

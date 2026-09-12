# Working on go-core

## Read first

- [Consumer guide](docs/consumer-guide.md): start a product, add a module, migrate and upgrade.
- [Persistent startup example](examples/startup/README.md): executable API + SQL + events.
- [Minimal module](examples/minimal/orders/orders.go): the smallest in-memory example.
- [Application facade ADR](docs/adr/0006-public-application-facade.md).
- [Separated migrations ADR](docs/adr/0007-separated-migration-histories.md).

## Decide where the change belongs

This repository is a shared dependency. A product's projects, orders, billing rules and other business logic belong in the **consumer repository**. Register them with `app.WithModules`; do not fork core or add product routes to `internal/infrastructure/server`. Change core when the capability is reusable across consumers or its public contract is insufficient.

Consumers import `github.com/mr-kaynak/go-core/app`, `identity` and optionally `coremigrations`. They may use Fiber v3 and GORM directly. Do not import core `internal/` or core `examples/` from an external consumer. Copy the example into the consumer and rewrite its local imports.

## Contracts to preserve

- `app.Module`: `Name`, `Permissions`, `Register`. `Register` wires dependencies and routes; no new connections or goroutines. There is no public background-worker lifecycle or event-subscription API yet.
- `ModuleContext.Router` already starts at `/api/v1`. Protect business groups with **both** `Auth` and `Authz`.
- Permission `Objects` contain full paths: collection **and** item pattern. GET maps to `ActionRead`, POST to `ActionCreate`, PUT/PATCH to `ActionUpdate`, DELETE to `ActionDelete`.
- RBAC grants an operation; handlers/repositories still filter rows by the authenticated owner or validated tenant. Never trust an `owner_id` or tenant header as authorization by itself.
- Read the caller through `identity.FromContext(c)` and check `ok`. Missing grants are valid; missing identity is unauthorized.
- Migration sources use stable names and root-level embedded SQL. Never rename a deployed source or edit an applied migration. Add the next SQL file. Do not AutoMigrate business models during `Register`.
- Write business data in a GORM transaction; pass `app.ContextWithTx(ctx, tx)` to `Events.Dispatch` and return dispatch errors. Local event callbacks run before commit; only the outbox write shares the transaction.
- UI guards are presentation. Backend authorization is mandatory.
- Keep changes to auth, refresh, permissions, migration admission and outbox behavior covered by regression tests. Do not weaken checks to make an example work.

## Verify

Run from the repository root with the Go version declared in `go.mod`:

```bash
go test ./... -count=1
go vet ./...
bash scripts/external-consumer-check.sh
```

For schema/locking changes, also run the PostgreSQL suite using a disposable database server whose role can create databases:

```bash
GOCORE_REQUIRE_POSTGRES=1 GOCORE_TEST_POSTGRES_DSN='postgres://postgres:local-password@127.0.0.1:5432/postgres?sslmode=disable' go test ./internal/infrastructure/database/... ./app/... -race -count=1
```

That DSN is an example, not a configured service. Without the test DSN PostgreSQL tests may skip: a green unit suite does not prove migration behavior. Never point these tests at production.

## Handoff

Report changed files, commands and outcomes, skipped checks and known limitations. Keep repository code and documentation in English. Never include `.env`, bootstrap passwords, tokens or database contents in commits or reports. Do not claim a published version or successful upgrade from a local build alone.

# Startup API: projects with persistence

This is a small product backend built entirely on public go-core APIs. It adds caller-owned projects to the core login, role and permission system. Its [admin companion](https://github.com/mr-kaynak/core-ui/tree/main/apps/startup) lists and creates projects.

## Start in three steps

You need the Go toolchain declared in the repository's `go.mod`, Docker Compose and OpenSSL. Commands below start from **the go-core repository root**. The isolated development stack uses PostgreSQL 55435, Redis 56380, RabbitMQ 55673 and test SMTP 51026, all bound to loopback.

```bash
# 1. Generate a local .env without overwriting an existing one.
bash examples/startup/setup.sh

# 2. Start the example's own infrastructure; existing core services are separate.
docker compose -f examples/startup/compose.yaml up -d --wait

# 3. Start the API from its configuration directory.
cd examples/startup
go run .
```

On the first boot, migrations create core tables and `startup_projects`. Bootstrap prints a random initial password for `admin@system.local` to the local terminal. Save it and change it after signing in. It is printed only when that account is created; there is no shared default password.

In another terminal:

```bash
curl --fail http://localhost:3300/livez
curl --fail http://localhost:3300/readyz
# No token: expect HTTP 401.
curl -i http://localhost:3300/api/v1/projects
```

Start the [admin companion](https://github.com/mr-kaynak/core-ui/tree/main/apps/startup), sign in and open **My projects**. Create a project, restart the API and refresh: the row remains. Mail for this local environment is visible in Mailpit at `http://localhost:58026`. All four infrastructure images are pinned by version and digest; all four services have health checks for Compose `--wait`.

## Follow the code

| File | What it teaches |
| --- | --- |
| [main.go](main.go) | App construction and database-only `-report` / `-migrate` modes. |
| [projects/projects.go](projects/projects.go) | Public module contract, owner filtering, input validation and a transactional event. |
| [00001_create_projects.sql](projects/migrations/00001_create_projects.sql) | A module-owned table/index, embedded through `MigrationProvider`. |
| [.env.example](.env.example) | Explicit local configuration, including API/UI ports and CORS. |
| [compose.yaml](compose.yaml) | Isolated development infrastructure with persistent PostgreSQL data. |

## API contract

All paths below are relative to `http://localhost:3300/api/v1`; all require authentication and the corresponding permission.

| Method/path | Permission | Response |
| --- | --- | --- |
| `GET /projects` | `projects.view` | `200 {"projects": [...], "total": 1}`; newest 50 owned rows, total counts all owned rows. |
| `GET /projects/:id` | `projects.view` | `200` project; invalid UUID `400`; missing/other owner's project `404`. |
| `POST /projects` | `projects.create` | Body `{"name":"My startup"}`; `201` project; blank or over 120 characters `400`. |

A project contains `id`, `name`, `owner_id` and `created_at`. Ownership always comes from the verified caller. This is per-user ownership, **not** organization tenancy or collaboration. Even a system administrator is scoped to their own project rows.

Creation writes the project and `project.created` outbox record in one transaction. Event data contains `project_id`, `owner_id`, `name`; aggregate ID is the project ID. Returning the dispatch error rolls back the business write. Consumers of delivered events must still be idempotent; this does not promise exactly-once external side effects.

## Dedicated migration job

Run from `examples/startup`:

```bash
go run . -report
go run . -migrate
DB_AUTO_MIGRATE=false go run .
```

The migration mode uses `LoadMigratorConfig`, includes core plus `startup_projects`, and does not start HTTP, Redis or email. `-report` prints read-only state and a `serve_allowed` verdict for the same embedded sources. This is a point-in-time check; serving startup checks admission again. For a legacy database use the [baseline runbook](../../docs/migration-baseline-runbook.md) with the actual historical source files; the new example is not a legacy schema mapping. It only requires database configuration; the local `.env` happens to include API settings too. Repeating `-migrate` when current applies nothing. Histories are `core_schema_versions` and `startup_projects_schema_versions`.

## Turn this into your product

Follow [the consumer guide](../../docs/consumer-guide.md#how-to-start-an-independent-repository). Copy the example's source and configuration template; initialize your own Go module, change its local projects import and pin go-core. Build your own business features here while continuing to get shared fixes through the dependency.

For deployment, replace the local credentials and Compose setup with your environment's configuration, run the migration job, and verify non-admin role permissions. The example is intentionally small: no update/delete, pagination controls, tenant model, billing or background event consumer.

## Stop and troubleshoot

Stop the API with Ctrl-C. From the go-core root, `docker compose -f examples/startup/compose.yaml stop` stops only this stack and preserves projects. Use `up -d --wait` to resume it. Do not remove its volume if you want the data to survive.

If a port is occupied, change the matching Compose mapping and `.env` setting. If startup says a required secret is missing, check the working directory and rerun `setup.sh` only after deciding how to handle your existing `.env`. Other common failures are covered in [the troubleshooting table](../../docs/consumer-guide.md#troubleshooting).

## What you built

A product-owned API and schema, running alongside core identity and RBAC, with persistent data and atomic event publication. Add the next domain as another module through the same public contract.

The projects-to-users foreign key intentionally restricts hard deletion while projects exist. A product must choose reassignment, retention or explicit cleanup before deleting an owner; this example does not cascade business-data deletion.

# Operations, verification and compatibility

## Evidence levels

A clean unit suite, a real PostgreSQL migration run, an external consumer build, a browser workflow and a published-registry install prove different things. Record each separately. A skipped PostgreSQL test or absent predecessor must never be reported as a proven upgrade.

## Core development checks

From go-core root:

```bash
go test ./... -count=1
go vet ./...
bash scripts/external-consumer-check.sh
```

CI also runs lint and race/coverage gates; inspect the pinned workflow/config for current versions and thresholds. For migration or schema-sensitive work, use a disposable PostgreSQL server whose role can create test databases:

```bash
GOCORE_REQUIRE_POSTGRES=1 GOCORE_TEST_POSTGRES_DSN='postgres://postgres:local-password@127.0.0.1:5432/postgres?sslmode=disable' go test ./internal/infrastructure/database/... ./app/... -race -count=1
```

That is a placeholder DSN, not an existing service. Tests create/drop their own DBs; never use production. Without a DSN, PostgreSQL tests may skip. SQLite tests can validate handler logic but cannot establish PostgreSQL SQL/locking/catalog behavior.

From core-ui root:

```bash
bun install --frozen-lockfile
bun run typecheck
bun run test
bun run lint
bun run build
bun run pack-test
```

Pack-test compiles real modules from tarballs outside the workspace. Builds include startup and playground. Perform browser checks against the intended API too: login, permission-gated direct URL, create, refresh, reload after API restart, and a user without the grant. Do not use system_admin as your only authorization test.

## Migration/deployment contract

Development `DB_AUTO_MIGRATE=true` applies core and consumer sources at startup. Deployment runs a consumer-specific migration job first, then serves with `DB_AUTO_MIGRATE=false`. Startup still checks schema admission. Use separate DDL/runtime database roles as your deployment supports them; the job can load only DB config via `LoadMigratorConfig`.

Migration execution is transactional per file, not for the entire source set. A failed later migration rolls itself back; earlier commits remain. Histories and source names must remain immutable. Core baseline handles a legacy single `goose_db_version` history only after plan/fingerprint checks: read [the included baseline runbook](legacy-baseline.md), reconcile it with the pinned implementation, rehearse against a restored DB and use dry-run first. Do not improvise metadata edits, drop histories, force baseline or enable pending/unknown-version tolerances to conceal an unexplained state.

Serving and DDL jobs must ship the same consumer source inventory. Core-only `cmd/migrate` cannot see a consumer's embedded files. `Report` is read-only; `Up` changes schema; `Baseline` changes migration metadata. Follow the user's existing authorization for these actual actions.

## Upgrade workflow

1. Record current backend ref, three UI versions/tarball hashes, consumer lockfiles and migration source inventory. Determine whether the DB is fresh, legacy or already separated.
2. Select a reviewed candidate ref/artifact set. Confirm actual remote tags/registry availability; version strings in package manifests do not prove publication. `git ls-remote --tags` and registry metadata are read-only discovery; package publication is a separate action.
3. Back up and restore into a disposable environment. Verify the restore itself and baseline state before applying anything.
4. Build a consumer using the candidate dependencies with no private imports. Test packed UI artifacts and, for an actual release, install all three exact versions from the real registry without local overrides and verify artifact integrity.
5. Migrate using the candidate consumer job. Assert both history versions and seed/business data. Run old-client/new-backend and current-client/new-backend flows. Check pending outbox recovery and permissions.
6. Rehearse rollback using the documented compatibility policy. An old binary is not automatically compatible with a newer schema. Never enable unknown-version tolerance without a specific tested rollback procedure.
7. Report consumer code changes separately from dependency/lockfile changes. Promote only a candidate whose required gates actually ran. Do not assert “dependency bump only” if product code changed.

Before a first published release there is no previous published version to upgrade from. Record that gap explicitly, run fresh install and candidate checks, and don't manufacture a predecessor tag to create a green badge. Existing source repositories may not have a complete synchronized release-set pipeline; inspect the current workflows. A request to push code does not imply publish npm artifacts, release tags, or deploy production.

## Troubleshooting map

| Symptom | Diagnostic / fix |
| --- | --- |
| Required config/placeholder secret error | Correct working directory, explicit godotenv load, setup-generated secrets; never print the secret to debug. |
| Redis startup refused | Start intended instance/check credentials; preserve `required` outside deliberate local experiments. |
| `/projects` 404 | Start consumer example, not stock cmd/api. |
| Valid user 403 | Check actual grants, full collection/item object patterns and HTTP action. |
| Menu hidden, URL accessible | Route requires its own `requirePermission`. |
| Owner's count/list mismatch | Apply same scope to both queries; consider concurrent snapshot semantics. |
| Permission succeeds but UI errors | Inspect response status/content type with SDK version; use JSON mutation responses and supported legacy handling. |
| Fast login gives duplicate refresh hash | Verify unique refresh jti on both normal/transaction issuance paths. Test overlapping sessions without sleeps. |
| Missing CSS | Build packages; dist/styles.css is a build artifact. |
| CORS/deep-link failure | Match origin including port, API /api/v1, basename and host SPA fallback. |
| Missing source history/legacy refusal | Compare source inventory, report classification and baseline runbook; do not delete metadata. |
| Works with workspace aliases only | Compile separate Go module or packed UI consumer; remove private source aliases. |
| Two app copies touch one local stack | Change copied Compose project name, ports, database and broker namespace. |

## Completion record

Keep a short record with exact refs, source inventory, commands and results, skipped gates and why, behavior/security checks, and deploy/rollback order. Logs must omit secrets and tokens. Avoid claiming that one example proves every admin feature or multi-replica/tenant behavior. Open issues should describe a reproducible trigger and expected result, not just a vague readiness score.

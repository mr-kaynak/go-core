# Legacy history conversion: operator runbook

Use this only for an existing DB whose report says `legacy` (`goose_db_version`). It is a one-time metadata conversion, not ordinary SQL migration. A fresh DB needs `Up`, not baseline. Never infer ownership of legacy version numbers.

## Prerequisites

Obtain a tested backup/restore, stop all application/migration/cron writers, and obtain the exact source SQL that was applied. Use the pinned core CLI plus every consumer source. You can build the CLI from `cmd/migrate` in a reviewed checkout; the skill discovers/clones missing source into a directory outside the product repo. Export only the DB environment variables from the intended environment when invoking it. Do not assume a root `.env` points at the intended DB.

`migrate status --source-dir orders=/absolute/path/to/orders/sql` reports core plus that source. Repeat `--source-dir` for each consumer. The startup example's `go run . -report` reports its embedded core/projects inventory but is not a general baseline interface.

## Mapping and validation

Read legacy applied versions using the actual history. Assign every applied positive version to exactly one registered source. Core must own exactly the prefix `1..N`, with no gap. A source must supply SQL at those same version numbers; baseline does not renumber its files. A newly created startup migration numbered 1 cannot be used as evidence for an old orders migration numbered 17.

Example **only if** the real database applied core 1–16 and orders 17–18:

```bash
migrate baseline --map core:1-16 --map orders:17-18 --source-dir orders=/absolute/path/to/orders/sql
```

This defaults to dry-run. Read the legacy versions, proposed histories, core fingerprint match, unverifiable data-only migrations, and required overrides. Dry-run follows the locked conversion path and rolls back; no conversion is committed. Refuse missing/gapped ownership or unexplained fingerprint differences.

If the plan is verified and the user has authorized the DB mutation, rerun the same command with `--apply`. Baseline commits histories and audit together. It leaves the old table as evidence. Then run status with the same sources, apply pending SQL using the full consumer inventory, and verify serving admission and business data on the restored rehearsal first.

The SQL/source assignment and fingerprint must agree. `--force`, `--unverified-source` and `--acknowledge-data-migrations` are explicit policy decisions, not fixes for an unexplained failure. The tool cannot prove data-only migrations through catalog fingerprints; verify their data effects separately. An unverified source still needs to be registered; the flag does not permit inventing a missing source with no FS.

## Public programmatic types

`app.BaselinePlan` is an alias with these accessible fields:

```go
plan := app.BaselinePlan{
    Mapping: map[string][]int64{"core": {1, 2}, "orders": {3}},
    Unverified: map[string]bool{},
    Force: false,
    AcknowledgeDataMigrations: false,
}
// m is app.NewMigrator with the actual, complete consumer sources.
// outcome, err := m.Baseline(ctx, plan, true) // true = dry-run
```

The tiny numbers above illustrate the type only; they are not a plan for a real DB. `BaselineOutcome` exposes `Applied`, `Histories`, `FingerprintDiffs`, `UnverifiableVersions`, `LegacyTable`, `LegacyApplied`, `CoreTarget` and additional current audit metadata. Read the pinned type for the complete shape before serializing it. `Migrator.Report` does not write. `Migrator.CheckServing(ctx)` verifies serving admission under the migration lock using database-only settings; an error refuses serving. It is a point-in-time rehearsal check and does not replace startup admission. `Up`/`UpOne` use the guarded runner and reject legacy state before DDL; there is no need to bypass this with a custom goose invocation.

## Recovery

On conversion failure, inspect the transaction outcome and audit; do not keep retrying with wider overrides. A successful baseline can be undone as metadata only **before any new migration runs**, after verification and explicit authorization; otherwise restore a known-good backup. Never drop history tables as an automatic recovery action. Rehearse on the restore, preserve the legacy evidence and confirm which source inventories each binary expects before serving traffic.

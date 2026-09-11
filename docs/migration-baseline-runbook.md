# Runbook: moving an existing database onto separated migration histories

This is a one-time conversion, run once per database, by hand, with the
application stopped. It writes **only metadata** — no DDL, nothing applied, no
rollback. What it writes is a claim that your schema is already at a given
version, and everything afterwards trusts that claim.

Budget 30 minutes for a first run, most of it reading output.

See [ADR-0007](adr/0007-separated-migration-histories.md) for why the
conversion exists at all.

## Who needs this

Run `migrate status`. If it reports `state legacy`, you need this:

```
core
  history table          core_schema_versions
  applied                none
  pending                none
  state                  legacy

overall state: legacy

cannot serve: this database still uses the single pre-separation migration history (goose_db_version, versions 1-18).
Convert it once, with the application stopped:
    migrate baseline            # review the plan
    migrate baseline --apply    # perform it
The legacy table is left in place as evidence and is not read again afterwards.
```

If it reports `fresh`, you need nothing: an empty database gets separated
histories on its first migration. If it reports `healthy`, this was already
done.

## Before you start

**Take a backup.** Everything below refuses rather than guesses, and the
refusals are thorough, but this is a schema you cannot re-derive.

**Stop everything that writes.** The application, any migration job, any cron
task holding a connection. The conversion holds the migration lock, so a
concurrent migration cannot interleave — but an application that starts
mid-conversion will see a database in a state it refuses to serve, and you do
not want to debug that and this at the same time.

**Have the migration files.** Core's are embedded in the binary. Every other
source's are yours, and you need the directory on disk.

## Step 1 — Write down who owned which version

This is the whole job. Everything else is checking.

The legacy history is a stream of numbers with no record of who wrote each
one. It cannot be inferred, and a wrong guess writes a history claiming a
migration ran when it did not — the kind of error whose first symptom is a
query against a column that was never created, months later.

List what is there:

```sql
SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY version_id;
```

Then split it. Core's versions are the ones that came from this skeleton;
match them against `coremigrations/sql/` in the version you were running when
each was applied. Everything else belongs to a source you name yourself.

For a database that took core 1–16 and then two migrations of its own:

```
core    1-16
orders  17-18
```

**Core's share must be exactly 1..N with no gaps.** goose refuses to advance a
history missing a version below its highest applied one, so a gapped mapping
is complete, disjoint, and permanently unusable. The tool checks this before
writing anything:

```
baseline refused: versions assigned to "core" must be exactly 1..N with no gaps,
but [1 2 4 5 6 7 8 9 10 11 12 13 14 15 16] is missing 3.
goose cannot advance a history with a gap below its highest version
```

If you genuinely have a gap in core's versions, stop and ask. It means a
migration was applied by something other than this tool, or removed from the
history by hand.

## Step 2 — Dry run

A dry run is the default. It takes the identical path — same lock, same
checks, same writes — and aborts at commit.

```
migrate baseline \
  --map core:1-16 \
  --map orders:17-18 \
  --source-dir orders=./migrations
```

`--source-dir` points at a source's migration files. Without it, that source's
mapped versions cannot be checked against anything, and the conversion is
refused rather than taken on trust.

Read the output:

```
Baseline dry run. Nothing below has been written.

Found
  legacy history         goose_db_version
  versions applied       1-18

Would write
  core
    history table        core_schema_versions
    versions             1-16 (16 migrations)
  orders
    history table        orders_schema_versions
    versions             17-18 (2 migrations)

Schema fingerprint
  matches what core version 16 produces

Not verified
  nothing; every mapped version was checked

Requires
  nothing beyond --apply

Nothing was written. To perform this conversion, stop every application and
migration job against this database, then run:
  migrate baseline --map core:1-16 --map orders:17-18 --source-dir orders=./migrations --apply
```

Four things to check:

- **Found → versions applied** matches what you saw in step 1.
- **Would write** matches what you wrote down, source by source.
- **Schema fingerprint** says `matches`. This is independent evidence: the live
  catalog is compared against what core's migrations are recorded to produce,
  reconstructed by replaying the recorded per-migration deltas. It does not
  ask the history, because the history is the claim under question.
- **Requires** says `nothing beyond --apply`. Anything else is a decision you
  have to make; see *When it refuses* below.

The printed apply command is rebuilt from the parsed plan, not echoed back from
what you typed. It is canonical and safe to paste.

## Step 3 — Apply

Add `--apply` to the command the dry run printed.

```
Baseline applied.

Wrote
  core
    history table        core_schema_versions
    versions             1-16 (16 migrations)
  orders
    history table        orders_schema_versions
    versions             17-18 (2 migrations)

The conversion is recorded in core_migration_baseline.
The legacy history goose_db_version was left in place as evidence and is not read again.
```

Everything happens in one transaction holding the migration lock. There is no
half-converted state to recover from.

## Step 4 — Confirm, then start the application

```
migrate status --source-dir orders=./migrations
```

```
core
  history table          core_schema_versions
  applied                16, highest 16
  pending                none
  state                  healthy

orders
  history table          orders_schema_versions
  applied                2, highest 18
  pending                none
  state                  healthy

overall state: healthy
```

Pass `--source-dir` here too. Without it, `status` reports core alone and says
nothing about the history you just wrote.

`pending` will be non-empty if the binary you are holding ships migrations this
database has not run. That is normal after an upgrade — run `migrate up`.

The audit record is in `core_migration_baseline`, one row per source:

```
source        | core
history_table | public.core_schema_versions
versions      | 1-16
legacy_table  | public.goose_db_version
core_target   | 16
forced        | f
unverified    | f
differences   | 0
unverifiable  | 0
tool_version  | 2f4defaf283be5dbbe66a002a3f27d8c3d765b39
recorded_at   | 2026-09-11 01:07:33.287832+00
```

Running it a second time is refused, not repeated:

```
cannot baseline: this database already uses separated histories; there is nothing to convert.
```

`goose_db_version` is deliberately left in place. It is the only record of what
this database was, and the moment you would want it is the moment something
went wrong with the conversion. Drop it when you are confident, not before.

## When it refuses

Every refusal below happens before anything is written.

### A version is not assigned to any source

```
baseline refused: the mapping does not describe this database. Every applied
legacy version must be assigned to exactly one source:
  - version 17 is applied in goose_db_version but is not assigned to any source
```

You missed one. A version left out would be recorded nowhere, and the source
that owns it would try to re-run a migration that already ran.

### A source has no migration at that number

```
baseline refused:
  - "core" has no migration numbered 17, so recording it as applied would
    describe a file that does not exist
```

The version belongs to a different source than you assigned it to — or to a
source you have not declared with `--source-dir` at all.

### The schema does not match the fingerprint

```
baseline refused: the schema does not match what version 16 produces.
1 difference(s): 0 missing, 0 mismatched, 1 extra
    - unexpected index public.idx_users_hotfix (CREATE INDEX idx_users_hotfix ON public.users USING btree (email)|valid=true|ready=true)

Establish why before converting. If the differences are understood and accepted,
re-run with --force; they are recorded either way
```

The live catalog differs from what the version you claimed is recorded to
produce. Everything is compared: tables, columns, constraints, indexes,
triggers, policies, rules, functions — including the differences that change
behavior without changing shape, like a forced row-level-security flag or a
changed column collation.

**Read them before doing anything.** Two common causes:

- **A hand-applied change.** An index added during an incident, a column
  widened by hand. Real, expected, and not a reason to stop — this is what
  `--force` is for. It proceeds and records every difference in the audit row.
- **The mapping is wrong.** The schema is at a different version than you
  claimed. Fix the mapping. Do not force it.

The tool cannot tell these apart. You can.

Re-running the dry run with `--force` shows the full report with the
differences in place, so you can review the whole conversion before consenting
to it:

```
Schema fingerprint
  differs from what core version 16 produces:
  1 difference(s): 0 missing, 0 mismatched, 1 extra
    - unexpected index public.idx_users_hotfix (CREATE INDEX ...)

Requires
  --force                        1 schema difference(s), listed above
```

### Versions no fingerprint can verify

```
Not verified
  core version(s) 11, 14 change no catalog state, so no fingerprint can confirm they ran
```

A data-only migration — a backfill, a settings row — leaves no catalog trace.
Nothing can confirm it ran. `--acknowledge-data-migrations` says you accept
that; the count goes into the audit row.

### A source you cannot supply files for

`--unverified-source NAME` accepts a source's mapped versions without checking
them against its files. Use it when the files are genuinely gone — a module
that was removed, whose migrations still ran. The source is recorded as
unverified in the audit row.

## Rolling back

There is nothing to roll back. Baseline writes metadata, and if you decide the
conversion was wrong, the recovery is:

```sql
DROP TABLE core_schema_versions, orders_schema_versions, core_migration_baseline;
```

`goose_db_version` is untouched, so the database returns to `legacy` and you can
start again with a corrected mapping. This only holds if you have not migrated
since — once `migrate up` has run against the new histories, DDL has happened
and this is no longer a metadata-only situation.

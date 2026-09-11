# ADR-0007: Separated migration histories with stated, verified baselining

- **Status:** Accepted
- **Date:** 2026-09-11

## Context

ADR-0006 replaced copy-distribution with versioned-dependency distribution. That
change moves one thing out of the consumer's control that used to be entirely
theirs: the database schema.

Under copy-distribution there was one migration history, `goose_db_version`, and
one author. A consumer who added `00017_orders.sql` owned version 17 forever,
because nobody else was writing migrations into their tree.

Under dependency-distribution there are two authors and one number line. Core
ships `00017` in a release; the consumer already applied their own `00017`. The
history records that version 17 was applied, so core's 17 never runs — and
nothing anywhere reports a problem. The consumer's database is missing a core
migration permanently, and the first symptom is a query against a column that
was never created.

This is not a hypothetical ordering race. It is the guaranteed outcome of two
independent release cadences writing into one sequence.

Three further problems follow from the same root:

- **A history says *that*, never *what*.** `goose_db_version` records that
  version 12 ran. It does not record what version 12 contained. Editing a
  released migration therefore changes what fresh installs receive while every
  existing database keeps the old result, silently and permanently.
- **goose's own entry points create metadata outside the lock.** `Provider.Up`
  consults `HasPending` on an unlocked connection, which creates the history
  table; `Provider.Status` and `Provider.Version` create the table they report
  on. Any safety check bolted on top of those entry points is checking a
  database that the call already modified, and is skipped entirely when nothing
  is pending.
- **Existing databases have to get there from here.** Every deployment already
  on the single history needs a way onto the separated ones, run once, by hand,
  against production.

## Decision

### One history per named source

Each source of migrations owns its own history table: core writes
`core_schema_versions`, a consumer source named `orders` writes
`orders_schema_versions`. Version numbers are per-source, so core's 17 and the
consumer's 17 are different rows in different tables and neither can hide the
other.

A source is `{Name, fs.FS}`, registered through `app.WithMigrations(...)` or by
a module implementing `app.MigrationProvider`. Names are validated
(`^[a-z][a-z0-9_]{0,30}$`) because the name becomes an identifier in SQL. Core
is always registered and always runs first: a consumer's migration may depend
on a core table, and the reverse is not possible.

Core's versions must be contiguous from 1. goose refuses to advance a history
that is missing a version below its highest applied one, so a gap is not a
warning — it is a history that can never move again.

### Migrations must be transactional, and that is enforced

A migration and its history row commit together. That single fact is the only
reason an interrupted migration leaves nothing behind. So `-- +goose NO
TRANSACTION`, `-- +goose ENVSUB ON`, and top-level transaction-control
statements are rejected when a source is registered — for every source, core
included. `StatementBegin`/`StatementEnd` is not an exemption; a dollar-quoted
function body is, and the scanner distinguishes them.

### Immutability is checked, not asked for

`coremigrations/migrations.lock` records a digest per shipped migration file,
and CI verifies it. "Released migrations are immutable" stops being a rule in a
document that a reviewer has to remember and becomes a failing build.

### Classification and admission are separate questions

Before anything is applied, the database is classified into one of eight states
per source — `fresh`, `healthy`, `pending`, `gapped-history`,
`unknown-applied-versions`, `legacy`, `orphan-schema`, `ambiguous-provenance` —
and a matrix says which of `serve`, `migrate`, `baseline` and `diagnose` each
state admits.

They are separate because they answer different questions. Classification says
what the database *is*; admission says what may be *done* to it. The two
documented escape hatches (`DB_ALLOW_PENDING_MIGRATIONS` for a rolling deploy,
`DB_ALLOW_UNKNOWN_APPLIED_VERSIONS` for a rollback) change a classification or
an admission wholesale. Neither punches a hole in one cell of the matrix, which
is how a tolerance granted for one procedure ends up permitting a different one
nobody considered.

The checks run whether or not migration is enabled. The recommended production
setting is `DB_AUTO_MIGRATE=false`, so a refusal reachable only through the
migration path would never run where it matters most.

### The guard runs inside goose's own lock

goose's entry points cannot be wrapped from outside without the metadata
problem described above. The one hook that is genuinely inside the locked
section is `SessionLocker`: goose calls it on the migration connection before
`ensureVersionTable`, and its error aborts the run.

So the lock is a decorator. It acquires goose's advisory lock, then performs
classification, inventory verification and sentinel setup on that same
connection, unlocking on failure. Every source's locked section re-validates
*core*'s history against the running binary's inventory, which stops an older
runner that returns for its own source after a newer one has advanced core.

Direct goose use is confined to four packages, enforced by a type-aware AST
gate — resolved receiver types, not matched names, so a local alias does not
slip past it.

### Schema fingerprints as independent evidence

For baselining, "the history says version 16" is exactly the claim under
question, so it cannot be the evidence. Counting tables is not evidence either:
core migrations 10 through 16 create no tables at all.

Each migration's effect on the catalog is therefore recorded as a delta —
tables including RLS flags, columns including collation, constraints, indexes,
triggers, FK enforcement state, policies, rules, functions — one file per
migration, generated by replaying the migrations against a scratch database.
Deltas rather than snapshots: 204KB instead of over a megabyte, and a diff that
shows what a migration did.

Extraction runs in a fixed environment (`search_path=''`, `row_security=off`,
`quote_all_identifiers=off`) restored on exit, because otherwise the operator's
session settings decide what the fingerprint says.

Ownership is by parent relation, not by expected identity. Narrowing a
comparison to the objects the version was supposed to create is what makes a
hand-added `CHECK (false) NOT VALID` — which blocks every insert — invisible.

### Baselining states the mapping; the tool verifies it

A legacy history is a stream of version numbers with no record of who owned
each one. Inferring the split would write a history claiming a migration ran
when it did not, or the reverse, and nothing downstream would ever question it.

So the operator states the mapping and the tool checks it: every applied
version assigned exactly once, nothing assigned that was never applied, core's
share exactly 1..N, every resulting history usable by the same calculation the
runner will use, and the live schema matching the reconstructed fingerprint.
Refusals are the default; `--force` records the differences rather than
discarding them; every run writes an audit row. A dry run is the default and
takes the identical path, aborting at commit.

### A migration job loads only database settings

`app.LoadMigratorConfig` and `app.Migrator` exist so a migration step can run
without the application's configuration. Validating the full configuration
would mean a container that runs DDL cannot start without a JWT secret, an SMTP
host and an encryption key — and the only way to satisfy that is to give them
to it. Both loaders read the same variables and the same defaults, so they
cannot disagree about the lock bounds they share.

## Alternatives Considered

**One history, core reserves a version range.** Simple, and wrong the first time
a consumer forks or a second module ships migrations. It also does not solve
ordering: a reserved range still says nothing about whether core's 17 must
precede the consumer's 20.

**Timestamp versions instead of sequential ones.** Collisions become unlikely
rather than impossible, and the number stops being reviewable — nobody can tell
by reading a PR whether `20260911142233` comes before or after what is already
deployed. It also breaks the contiguity check that makes a gap detectable.

**Infer the baseline mapping from file names present in the consumer's tree.**
Attractive, and it fails exactly where it matters: a consumer who deleted or
renamed an applied migration gets a confident wrong answer. The operator has
information the tool does not, so the tool asks for it.

**Store a full schema snapshot per version.** Simpler to generate and to
compare. Over a megabyte of committed JSON, a diff nobody can read in review,
and no way to see what an individual migration did.

**Compare by hashing `pg_dump` output.** Sensitive to the server version, the
dump version, and ordering that carries no meaning; a 17.11 → 17.12 upgrade
would report every database as wrong. The catalog query is narrower and states
what it looks at.

**Trust the operator and skip verification when baselining.** The failure mode
is a database that reports healthy, skips a migration it never ran, and breaks
at a query months later. This is the one operation where a refusal costs
minutes and a silent success costs a schema.

## Consequences

### Positive

- Core and consumers can release independently without any possibility of
  version collision.
- A released migration cannot be edited without failing CI.
- A database that is not in a state the running binary can serve is refused at
  startup, before anything writes, including Casbin's adapter.
- goose's unlocked-metadata behavior is neutralized rather than documented.
- Baselining is reviewable before it runs and auditable after.
- A migration job needs one database role and no application secrets.

### Negative

- **A new migration now requires two generated artifacts.** `go run
  ./cmd/inventorylock` and `go run ./cmd/schemafingerprint`, the latter needing
  a live PostgreSQL. Forgetting either fails CI rather than shipping something
  wrong, but it is friction on every schema change.
- **Fingerprints pin the major PostgreSQL version.** They are generated against
  one; a major upgrade will require regeneration and a review of the diff. The
  exact minor is deliberately excluded.
- **A data-only migration cannot be verified.** It changes no catalog state, so
  its fingerprint is empty. Baseline names those versions and requires
  `--acknowledge-data-migrations` rather than pretending they were checked.
- **Baselining is manual and needs the application stopped.** It is a one-time
  cost per database, and it is the honest shape of the operation.
- **Eight states and four operations is more than a boolean.** An operator
  reading a refusal has more to learn than "migrations pending". The matrix is
  in `migrationstate` and the message names the state and what it admits.
- **`goose_db_version` is left in place after baselining.** Dropping it would
  destroy the only record of what the database was, at the moment that record
  is most likely to be needed.

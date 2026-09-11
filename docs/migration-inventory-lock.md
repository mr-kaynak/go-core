# Migration inventory lock

Released migration SQL is immutable. A database that has already recorded
version `N` will never run it again, so editing `N` changes what fresh installs
get while leaving every existing database behind — silently, and with no error
anywhere. Version numbers cannot catch this: the history records *that* `N` was
applied, never *what* `N` contained.

The lock file makes it checkable. `coremigrations/migrations.lock` records each
migration's version, filename and SHA-256, and CI compares the source tree
against it on every pull request.

## What the check rejects

| Change | Why it is rejected |
|---|---|
| Editing an applied migration | Existing databases keep the old schema; new ones get the new one |
| Deleting a migration | Existing databases still reference the version |
| Renaming or renumbering | Breaks the mapping between recorded versions and files |
| Adding one without updating the lock | Not a defect, but it must be a deliberate, visible act |

## Adding a migration

```bash
make migrate-create NAME=add_orders_index
# write the -- +goose Up / -- +goose Down sections
go run ./cmd/inventorylock
```

Commit the migration and the regenerated lock together.

## Fixing a mistake in a released migration

Do not edit it. Add a new migration that corrects the schema. Databases that
already ran the flawed version get the correction; fresh installs run both and
land in the same place. That property — every database converging on the same
schema regardless of when it started — is the whole point.

Only regenerate the lock over an edited migration when the migration has
genuinely never left this repository, and say so in the commit message.

## Consumers: the same rule applies to your own migrations

Once your application has run its own migrations anywhere that matters, those
files are just as immutable as core's. `coremigrations` exports the same two
functions core uses on itself, so you can run the identical check over your own
filesystem:

```go
package migrations_test

import (
    "embed"
    "io/fs"
    "os"
    "testing"

    "github.com/mr-kaynak/go-core/coremigrations"
)

//go:embed sql/*.sql
var migrationsFS embed.FS

func TestMigrationsMatchLock(t *testing.T) {
    sqlFS, err := fs.Sub(migrationsFS, "sql")
    if err != nil {
        t.Fatal(err)
    }

    lock, err := os.Open("migrations.lock")
    if err != nil {
        t.Fatal(err)
    }
    defer lock.Close()

    if err := coremigrations.VerifyInventory(sqlFS, lock); err != nil {
        t.Fatal(err)
    }
}
```

Generate your lock file with `coremigrations.Inventory` and
`coremigrations.WriteInventory`, the same pair `cmd/inventorylock` uses.

## What this does not cover

The lock proves the SQL files did not change. It says nothing about whether a
given database actually ran them — a history table can claim a version that was
never applied, or was applied from different SQL. That is the schema
fingerprint's job.

## Schema fingerprints

A migration history records *that* version N was applied, never *what* N did.
Converting an existing database onto a separated history therefore needs
independent evidence that its schema really is at N, and counting tables is not
that evidence: core migrations 10–16 create no tables at all — `00016`'s entire
effect is replacing an index, `00003`'s is a trigger.

`internal/infrastructure/database/schemafp/fingerprints/data/` records what each
migration does to the catalog: tables, columns, constraints, indexes, triggers,
functions, and the enforcement state of foreign keys. Baseline reconstructs the
expected state for a version and compares it against the live database.

One file per migration rather than a snapshot per version: sixteen cumulative
snapshots of this schema would be well over a megabyte of near-identical JSON,
while the deltas together are the size of one. The shape pays off twice — it
reconstructs any version exactly, and a delta is also the most direct statement
of what a migration did, readable in review without inferring it from SQL.

### Regenerating

Only when adding a migration, and it needs a PostgreSQL server:

```bash
docker run --rm -d -p 5432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17-alpine
GOCORE_TEST_POSTGRES_DSN="postgres://postgres@localhost:5432/postgres?sslmode=disable"   go run ./cmd/schemafingerprint
```

CI replays every migration and compares, so a migration changed or added
without regenerating fails there rather than in somebody's baseline.

### What a fingerprint cannot establish

**Catalog state only.** A migration that changes data and no schema records an
empty delta, and the fingerprint can attest to nothing about it — a test names
those versions rather than leaving the gap implicit.

A matching fingerprint says the catalog looks the way that version produces.
It does not say:

- that any DML the migration carried alongside its DDL ran, or ran correctly
- that existing rows satisfy the constraints now recorded — a foreign key can
  be present and valid in the catalog over data that predates it
- that the migrations were ever executed at all. A schema built by hand to the
  same shape is indistinguishable, which is the point: baseline is converting
  a database somebody already has, not proving its provenance.

Comparisons are per PostgreSQL major version and refused across majors rather
than reported as differences. The exact minor is deliberately not recorded:
catalog output does not depend on it, and including it would make a patch
upgrade fail regeneration with nothing actually changed.

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
never applied, or was applied from different SQL. Establishing that is the job
of the schema fingerprint, which compares the live catalog against the state a
given version is expected to produce.

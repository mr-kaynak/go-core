package pgtest

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"

	"github.com/mr-kaynak/go-core/coremigrations"
	"github.com/pressly/goose/v3"
)

// CoreHistoryTable is the history table the separated core history will use.
// Tests create it under that name from the start so fixtures do not have to be
// rewritten when the runner lands.
const CoreHistoryTable = "core_schema_versions"

// ApplyCoreMigrations brings db up to the given core version, or to the latest
// one when upTo is 0.
func ApplyCoreMigrations(t *testing.T, db *sql.DB, upTo int64) {
	t.Helper()
	ApplyMigrationsFS(t, db, coremigrations.FS(), CoreHistoryTable, upTo)
}

// ApplyMigrationsFS applies migrations from an arbitrary filesystem into the
// named history table, up to the given version (0 for all of them). The
// upgrade tests use it to build a database with a previous release's
// migrations before running the current ones over it.
//
// It drives goose through ApplyVersion rather than Up: Up calls HasPending
// first, which initializes the history table on an unlocked connection, and
// several tests here are specifically about what exists before the history
// table does.
func ApplyMigrationsFS(t *testing.T, db *sql.DB, fsys fs.FS, historyTable string, upTo int64) {
	t.Helper()

	provider, err := goose.NewProvider(
		goose.DialectPostgres, db, fsys,
		goose.WithTableName(historyTable),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		t.Fatalf("pgtest: failed to build the migration provider: %v", err)
	}

	ctx := context.Background()
	applied := appliedVersions(t, db, historyTable)

	for _, source := range provider.ListSources() {
		if upTo > 0 && source.Version > upTo {
			break
		}
		if applied[source.Version] {
			continue
		}
		if _, err := provider.ApplyVersion(ctx, source.Version, true); err != nil {
			t.Fatalf("pgtest: failed to apply migration %d (%s): %v", source.Version, source.Path, err)
		}
	}
}

// appliedVersions reads the history table directly rather than through goose,
// because the table may not exist yet and asking goose would create it.
func appliedVersions(t *testing.T, db *sql.DB, historyTable string) map[int64]bool {
	t.Helper()

	ctx := context.Background()
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT to_regclass($1) IS NOT NULL`, historyTable,
	).Scan(&exists); err != nil {
		t.Fatalf("pgtest: failed to check for %s: %v", historyTable, err)
	}
	if !exists {
		return map[int64]bool{}
	}

	rows, err := db.QueryContext(ctx,
		`SELECT version_id FROM `+historyTable+` WHERE is_applied`)
	if err != nil {
		t.Fatalf("pgtest: failed to read %s: %v", historyTable, err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced below

	applied := map[int64]bool{}
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("pgtest: failed to scan a history row: %v", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("pgtest: failed to iterate %s: %v", historyTable, err)
	}
	return applied
}

// LatestCoreVersion is the highest embedded core migration version.
func LatestCoreVersion(t *testing.T) int64 {
	t.Helper()

	inventory, err := coremigrations.Inventory(coremigrations.FS())
	if err != nil {
		t.Fatalf("pgtest: failed to read the core inventory: %v", err)
	}
	var latest int64
	for version := range inventory {
		if version > latest {
			latest = version
		}
	}
	return latest
}

// ChurnOIDs allocates and drops throwaway objects so subsequent DDL receives
// different OIDs than it would on a pristine database. A fingerprint that
// leaks an OID or a system-generated name changes under this; one built from
// stable identities does not.
func ChurnOIDs(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx := context.Background()
	for i := range 25 {
		name := "oid_churn_" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if _, err := db.ExecContext(ctx, `CREATE TABLE `+name+` (id int primary key, payload text)`); err != nil {
			t.Fatalf("pgtest: failed to create churn table: %v", err)
		}
		if _, err := db.ExecContext(ctx, `DROP TABLE `+name); err != nil {
			t.Fatalf("pgtest: failed to drop churn table: %v", err)
		}
	}
}

package main

import (
	"bytes"
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

// The property under test is the one that made this command worth rewriting:
// goose's own Status and Version create the version table as a side effect,
// and a history table that exists is exactly what classification reads to
// decide a database is no longer legacy. A status run that creates it would
// silently convert "needs a baseline" into "fresh, go ahead and migrate", so
// the diagnostic commands have to leave an empty database empty.
func TestDiagnosticCommandsWriteNothing(t *testing.T) {
	db := pgtest.New(t)
	runner := newTestRunner(t, db)

	before := tableNames(t, db.DB)
	if len(before) != 0 {
		t.Fatalf("pgtest handed out a database that is not empty: %v", before)
	}

	for _, command := range []string{"status", "version"} {
		var out bytes.Buffer
		if err := runTestCommand(runner, command, &out); err != nil {
			t.Fatalf("%s failed against an empty database: %v", command, err)
		}
		if created := tableNames(t, db.DB); len(created) != 0 {
			t.Fatalf(
				"%s created %v; a diagnostic that creates the history table disarms the "+
					"classification that reads its absence as a legacy database",
				command, created,
			)
		}
	}
}

func TestStatusAndVersionReportWhatWasApplied(t *testing.T) {
	db := pgtest.New(t)
	runner := newTestRunner(t, db)

	var upOutput bytes.Buffer
	if err := runTestCommand(runner, "up", &upOutput); err != nil {
		t.Fatalf("up failed: %v", err)
	}

	applied := appliedCoreVersions(t, runner)
	if len(applied) == 0 {
		t.Fatal("up applied nothing; the rest of this test would prove nothing")
	}
	top := highest(applied)

	var status bytes.Buffer
	if err := runTestCommand(runner, "status", &status); err != nil {
		t.Fatalf("status failed after up: %v", err)
	}
	for _, want := range []string{
		migrationsource.TableName(migrationsource.CoreName),
		appliedSummary(applied),
		"pending",
		string(migrationstate.StateHealthy),
	} {
		if !strings.Contains(status.String(), want) {
			t.Fatalf("status output is missing %q:\n%s", want, status.String())
		}
	}

	var version bytes.Buffer
	if err := runTestCommand(runner, "version", &version); err != nil {
		t.Fatalf("version failed after up: %v", err)
	}
	// version is consumed by scripts, so it is one line per source and
	// nothing else.
	wantVersion := migrationsource.CoreName + " " + strconv.FormatInt(top, 10) + "\n"
	if version.String() != wantVersion {
		t.Fatalf("version printed %q, want %q", version.String(), wantVersion)
	}
}

func newTestRunner(t *testing.T, db *pgtest.DB) *database.MigrationRunner {
	t.Helper()

	runner, err := database.NewMigrationRunner(
		database.MigrationConfig{DSN: db.DSN},
		app.CoreMigrationSource(),
	)
	if err != nil {
		t.Fatalf("failed to build the migration runner: %v", err)
	}
	t.Cleanup(func() { runner.Close() }) //nolint:errcheck // test cleanup
	return runner
}

func runTestCommand(runner *database.MigrationRunner, command string, out *bytes.Buffer) error {
	return dispatch(
		context.Background(), runner, command,
		migrationsource.CoreName, nil, migrationstate.Tolerances{}, out,
	)
}

func appliedCoreVersions(t *testing.T, runner *database.MigrationRunner) []int64 {
	t.Helper()

	state, err := sourceState(context.Background(), runner, migrationsource.CoreName)
	if err != nil {
		t.Fatalf("failed to read the core source state: %v", err)
	}
	return state.Applied
}

func tableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public'
		ORDER BY table_name`)
	if err != nil {
		t.Fatalf("failed to list the tables: %v", err)
	}
	defer rows.Close() //nolint:errcheck // read-only catalog query

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan a table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate the table list: %v", err)
	}
	return names
}

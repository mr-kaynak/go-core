package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

// What baseline writes is a claim that a schema is already at a given version,
// and the command's product is the report an operator reads before allowing
// it. These tests are about that report being true: the dry run has to leave
// the database exactly as it found it while naming what it would write, and
// the apply has to write what the dry run named.

func TestBaselineDryRunWritesNothingAndNamesThePlan(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := newLegacyDatabase(t, latest)

	opts := baselineOptionsForTest(t, "--map", coreSpec(latest))
	runner := newBaselineRunner(t, db, opts)

	var out bytes.Buffer
	if err := baseline(context.Background(), runner, opts, &out); err != nil {
		t.Fatalf("the dry run should succeed against a legacy database: %v", err)
	}

	// A dry run must be indistinguishable from not having run.
	for _, table := range []string{
		migrationsource.TableName(migrationsource.CoreName),
		migrationstate.BaselineAuditTable,
	} {
		if hasRelation(t, db, table) {
			t.Errorf("the dry run created %s", table)
		}
	}

	report := strings.Join([]string{
		// What it found.
		migrationstate.LegacyHistoryTable,
		fmt.Sprintf("1-%d", latest),
		// What it would write.
		migrationsource.TableName(migrationsource.CoreName),
		// The evidence, and what was left unestablished.
		fmt.Sprintf("matches what core version %d produces", latest),
		"nothing; every mapped version was checked",
		"nothing beyond --apply",
		// That nothing happened, and how to make it happen.
		"Nothing was written.",
		"migrate baseline --map " + coreSpec(latest) + " --apply",
	}, "\x00")

	for _, want := range strings.Split(report, "\x00") {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the dry run does not report %q:\n%s", want, out.String())
		}
	}
}

// The case baseline exists for: core's versions and a consumer's in one legacy
// stream, with no record of which was which. The consumer's migrations come
// from disk, because this binary ships none of them.
func TestBaselineApplySeparatesTheHistories(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	consumerVersion := latest + 1

	db := newLegacyDatabase(t, latest)
	ordersDir := newConsumerSourceTree(t, consumerVersion)
	// The consumer's migration ran into the same legacy history, which is what
	// made the two indistinguishable.
	pgtest.ApplyMigrationsFS(t, db.DB, os.DirFS(ordersDir), migrationstate.LegacyHistoryTable, 0)

	opts := baselineOptionsForTest(t,
		"--map", coreSpec(latest),
		"--map", fmt.Sprintf("orders:%d", consumerVersion),
		"--source-dir", "orders="+ordersDir,
		"--apply",
	)
	runner := newBaselineRunner(t, db, opts)
	ctx := context.Background()

	var out bytes.Buffer
	if err := baseline(ctx, runner, opts, &out); err != nil {
		t.Fatalf("the conversion should succeed: %v", err)
	}

	for _, source := range []string{migrationsource.CoreName, "orders"} {
		state, err := sourceState(ctx, runner, source)
		if err != nil {
			t.Fatalf("failed to read the state of %q: %v", source, err)
		}
		if state.State != migrationstate.StateHealthy {
			t.Errorf("%s should be healthy after the conversion, got %s", source, state.State)
		}
	}
	if got := appliedVersionCount(t, db, migrationsource.TableName("orders")); got != 1 {
		t.Errorf("the consumer's history records %d version(s), want 1", got)
	}

	// The legacy table is the only remaining record of what was there before,
	// so the conversion leaves it alone — and says so.
	if !hasRelation(t, db, migrationstate.LegacyHistoryTable) {
		t.Error("the legacy history must be left in place as evidence")
	}
	for _, want := range []string{
		"Baseline applied.",
		migrationsource.TableName("orders"),
		migrationstate.BaselineAuditTable,
		"left in place as evidence",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the apply does not report %q:\n%s", want, out.String())
		}
	}

	// A converted database is an ordinary one: it can serve, and the next
	// deploy can migrate it.
	if err := runner.CheckAdmission(ctx, migrationstate.OpServe); err != nil {
		t.Fatalf("a converted database should be serveable: %v", err)
	}
}

// Accepting a schema difference is a decision somebody owns, so a dry run that
// carries --force has to show what is being accepted rather than merely that
// something was.
func TestBaselineDryRunShowsTheDifferencesForceWouldAccept(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := newLegacyDatabase(t, latest)

	// Exactly the failure a table-existence check would miss: enforcement
	// switched off, every table still present.
	if _, err := db.ExecContext(context.Background(),
		`ALTER TABLE public.template_categories DISABLE TRIGGER ALL`); err != nil {
		t.Fatalf("failed to disable enforcement: %v", err)
	}

	opts := baselineOptionsForTest(t, "--map", coreSpec(latest), "--force")
	runner := newBaselineRunner(t, db, opts)

	var out bytes.Buffer
	if err := baseline(context.Background(), runner, opts, &out); err != nil {
		t.Fatalf("--force should proceed: %v", err)
	}

	for _, want := range []string{
		fmt.Sprintf("differs from what core version %d produces", latest),
		"difference(s)",
		"--force ",
		"migrate baseline --map " + coreSpec(latest) + " --force --apply",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the dry run does not report %q:\n%s", want, out.String())
		}
	}
	if hasRelation(t, db, migrationsource.TableName(migrationsource.CoreName)) {
		t.Error("a forced dry run is still a dry run")
	}
}

// A refusal is the baseline code's own message, and it is written to be acted
// on. Printing a plan beside it would describe a conversion that is not going
// to happen.
func TestBaselineRefusalIsReportedAsWritten(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := newLegacyDatabase(t, latest)

	// One version short: the legacy history holds a migration the mapping
	// assigns to nobody, so converting would offer it again later.
	opts := baselineOptionsForTest(t, "--map", fmt.Sprintf("core:1-%d", latest-1))
	runner := newBaselineRunner(t, db, opts)

	var out bytes.Buffer
	err := baseline(context.Background(), runner, opts, &out)
	if err == nil {
		t.Fatal("a mapping that does not cover the legacy history must be refused")
	}
	if !strings.Contains(err.Error(), "not assigned to any source") {
		t.Fatalf("the refusal should name the unassigned version, got: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a refused dry run should print no plan, got:\n%s", out.String())
	}
	if hasRelation(t, db, migrationsource.TableName(migrationsource.CoreName)) {
		t.Error("a refused baseline must write nothing")
	}
}

// newLegacyDatabase builds a database as it looked before the histories were
// separated: core's real migrations, recorded under goose's default table.
func newLegacyDatabase(t *testing.T, upTo int64) *pgtest.DB {
	t.Helper()

	db := pgtest.New(t)
	pgtest.ApplyMigrationsFS(t, db.DB, app.CoreMigrationSource().FS, migrationstate.LegacyHistoryTable, upTo)
	return db
}

// newConsumerSourceTree writes a consumer's migrations to disk, which is where
// --source-dir reads them from.
func newConsumerSourceTree(t *testing.T, version int64) string {
	t.Helper()

	dir := t.TempDir()
	name := fmt.Sprintf("%05d_create_orders.sql", version)
	content := "-- +goose Up\nCREATE TABLE orders (id int primary key);\n" +
		"-- +goose Down\nDROP TABLE orders;\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write the consumer migration: %v", err)
	}
	return dir
}

// baselineOptionsForTest parses a real command line, so the tests exercise the
// flags an operator types rather than a structure assembled beside them.
func baselineOptionsForTest(t *testing.T, args ...string) baselineOptions {
	t.Helper()

	opts, err := parseArgs(append([]string{commandBaseline}, args...))
	if err != nil {
		t.Fatalf("failed to parse %v: %v", args, err)
	}
	return opts.baseline
}

// newBaselineRunner registers the same sources run would: core, plus whatever
// --source-dir supplied.
func newBaselineRunner(t *testing.T, db *pgtest.DB, opts baselineOptions) *database.MigrationRunner {
	t.Helper()

	runner, err := database.NewMigrationRunner(
		database.MigrationConfig{DSN: db.DSN}, app.CoreMigrationSource(), opts.sources()...,
	)
	if err != nil {
		t.Fatalf("failed to build the migration runner: %v", err)
	}
	t.Cleanup(func() { runner.Close() }) //nolint:errcheck // test cleanup
	return runner
}

func coreSpec(upTo int64) string {
	return fmt.Sprintf("%s:1-%d", migrationsource.CoreName, upTo)
}

func hasRelation(t *testing.T, db *pgtest.DB, name string) bool {
	t.Helper()

	var exists bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatalf("failed to look up %s: %v", name, err)
	}
	return exists
}

func appliedVersionCount(t *testing.T, db *pgtest.DB, table string) int {
	t.Helper()

	var count int
	// The sentinel row is goose's own initialization rather than a migration.
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE version_id > 0 AND is_applied`).Scan(&count); err != nil {
		t.Fatalf("failed to read %s: %v", table, err)
	}
	return count
}

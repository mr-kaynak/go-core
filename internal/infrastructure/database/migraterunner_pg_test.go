package database_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mr-kaynak/go-core/coremigrations"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

// These tests are the reason the PostgreSQL harness exists. Locking, DDL
// transactionality and what survives an interrupted run are all behaviors
// SQLite does not share, so a suite that agreed with itself on SQLite would
// be agreeing about a different database.

func coreSource() modcontract.MigrationSource {
	return modcontract.MigrationSource{Name: migrationsource.CoreName, FS: coremigrations.FS()}
}

// tinyCore is a stand-in for core in tests about the runner rather than about
// core's schema. Sixteen real migrations make a slow fixture and obscure what
// is being tested.
func tinyCore(versions ...int) modcontract.MigrationSource {
	fsys := fstest.MapFS{}
	for _, v := range versions {
		fsys[fmt.Sprintf("%05d_core_%d.sql", v, v)] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf(
				"-- +goose Up\nCREATE TABLE core_t%d (id int primary key);\n"+
					"-- +goose Down\nDROP TABLE core_t%d;\n", v, v)),
		}
	}
	return modcontract.MigrationSource{Name: migrationsource.CoreName, FS: fsys}
}

func appSource(name string, statements map[int]string) modcontract.MigrationSource {
	fsys := fstest.MapFS{}
	for v, sqlText := range statements {
		fsys[fmt.Sprintf("%05d_%s_%d.sql", v, name, v)] = &fstest.MapFile{
			Data: []byte("-- +goose Up\n" + sqlText + "\n"),
		}
	}
	return modcontract.MigrationSource{Name: name, FS: fsys}
}

func newRunner(
	t *testing.T,
	db *pgtest.DB,
	core modcontract.MigrationSource,
	extra ...modcontract.MigrationSource,
) *database.MigrationRunner {
	t.Helper()
	return newRunnerWithConfig(t, database.MigrationConfig{DSN: db.DSN}, core, extra...)
}

func newRunnerWithConfig(
	t *testing.T,
	cfg database.MigrationConfig,
	core modcontract.MigrationSource,
	extra ...modcontract.MigrationSource,
) *database.MigrationRunner {
	t.Helper()

	runner, err := database.NewMigrationRunner(cfg, core, extra...)
	if err != nil {
		t.Fatalf("failed to build the migration runner: %v", err)
	}
	t.Cleanup(func() { runner.Close() }) //nolint:errcheck // test cleanup
	return runner
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var exists bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatalf("failed to look up %s: %v", name, err)
	}
	return exists
}

func appliedOf(t *testing.T, report migrationstate.Report, source string) []int64 {
	t.Helper()

	for _, state := range report.Sources {
		if state.Source == source {
			return state.Applied
		}
	}
	t.Fatalf("source %q missing from the report", source)
	return nil
}

// M1: each source lands in its own history, and core goes first because
// consumer migrations are allowed to depend on core objects.
func TestEachSourceMigratesIntoItsOwnHistory(t *testing.T) {
	db := pgtest.New(t)
	orders := appSource("orders", map[int]string{
		1: "CREATE TABLE orders (id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id));",
	})

	runner := newRunner(t, db, coreSource(), orders)
	if err := runner.Up(context.Background()); err != nil {
		t.Fatalf("Up failed: %v", err)
	}

	for _, table := range []string{"core_schema_versions", "orders_schema_versions", "users", "orders"} {
		if !tableExists(t, db.DB, table) {
			t.Errorf("expected %s to exist after migrating", table)
		}
	}

	// The foreign key above only resolves if core ran first; that it applied
	// at all is the ordering assertion.
	report, err := runner.Report(context.Background())
	if err != nil {
		t.Fatalf("Report failed: %v", err)
	}
	if state := report.State(); state != migrationstate.StateHealthy {
		t.Fatalf("expected a healthy database, got %s", state)
	}
}

// M2: running again applies nothing. Re-running a migration is the normal
// case — every restart does it — so it has to be free of side effects.
func TestRunningAgainAppliesNothing(t *testing.T) {
	db := pgtest.New(t)
	runner := newRunner(t, db, tinyCore(1, 2))

	ctx := context.Background()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("first Up failed: %v", err)
	}
	before := historyRowCount(t, db.DB, "core_schema_versions")

	if err := runner.Up(ctx); err != nil {
		t.Fatalf("second Up failed: %v", err)
	}
	if after := historyRowCount(t, db.DB, "core_schema_versions"); after != before {
		t.Fatalf("a second run added %d history rows; it should add none", after-before)
	}
}

func historyRowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM `+table).Scan(&count); err != nil {
		t.Fatalf("failed to count %s: %v", table, err)
	}
	return count
}

// M7: a build older than the schema must not proceed. goose itself does not
// notice — it reports nothing pending and returns success — so the check has
// to be ours.
func TestABuildOlderThanTheSchemaIsRefused(t *testing.T) {
	db := pgtest.New(t)

	newer := newRunner(t, db, tinyCore(1, 2, 3))
	if err := newer.Up(context.Background()); err != nil {
		t.Fatalf("failed to prepare the newer schema: %v", err)
	}

	older := newRunner(t, db, tinyCore(1, 2))
	err := older.Up(context.Background())
	if err == nil {
		t.Fatal("a build that does not ship version 3 must refuse a database that has it")
	}
	if !errors.Is(err, migrationstate.ErrRefused) {
		t.Fatalf("the refusal should be identifiable as one, got %T: %v", err, err)
	}

	// The documented rollback procedure turns this into a deliberate choice.
	tolerant := newRunnerWithConfig(t, database.MigrationConfig{
		DSN:        db.DSN,
		Tolerances: migrationstate.Tolerances{UnknownAppliedVersions: true},
	}, tinyCore(1, 2))
	if err := tolerant.Up(context.Background()); err != nil {
		t.Fatalf("with the rollback tolerance set, the same database should be accepted: %v", err)
	}
}

// M13: generation fencing. The dangerous shape is not two identical runners —
// it is an old one and a new one interleaving. A runs core to 2 and releases
// the lock; B applies core 3; A then comes back for its own source and must
// not proceed against a core schema it does not know.
func TestAnOlderRunnerIsFencedOutAfterANewerOneAdvancesCore(t *testing.T) {
	db := pgtest.New(t)
	orders := appSource("orders", map[int]string{1: "CREATE TABLE orders (id int primary key);"})

	older := newRunner(t, db, tinyCore(1, 2), orders)
	newer := newRunner(t, db, tinyCore(1, 2, 3))

	ctx := context.Background()
	if err := older.Up(ctx); err != nil {
		t.Fatalf("the older runner should manage its own generation: %v", err)
	}

	// A newer deployment moves core forward.
	if err := newer.Up(ctx); err != nil {
		t.Fatalf("the newer runner failed: %v", err)
	}

	// Now the older one runs again — as a lagging replica would. Its own
	// source has nothing pending, which is exactly the case that would slip
	// through without a locked completion check.
	err := older.Up(ctx)
	if err == nil {
		t.Fatal("the older runner must be refused once core moved past what it ships")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("the refusal should name the unknown version, got: %v", err)
	}
}

// M8(a): two runners racing from empty. Both must finish, and neither may see
// a duplicate-object error from applying the same migration twice.
func TestConcurrentRunnersBothComplete(t *testing.T) {
	db := pgtest.New(t)
	core := tinyCore(1, 2, 3)

	first := newRunner(t, db, core)
	second := newRunner(t, db, core)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)

	for i, runner := range []*database.MigrationRunner{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = runner.Up(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("runner %d failed: %v", i, err)
		}
	}
	for _, table := range []string{"core_t1", "core_t2", "core_t3"} {
		if !tableExists(t, db.DB, table) {
			t.Errorf("expected %s after both runners finished", table)
		}
	}
}

// M8(d): a failing migration must leave the ones before it applied and itself
// applied nowhere — neither its DDL nor its history row.
func TestAFailingMigrationRollsBackOnlyItself(t *testing.T) {
	db := pgtest.New(t)
	broken := appSource("orders", map[int]string{
		1: "CREATE TABLE orders_first (id int primary key);",
		2: "CREATE TABLE orders_second (id int primary key); SELECT no_such_function();",
	})

	runner := newRunner(t, db, tinyCore(1), broken)
	err := runner.Up(context.Background())
	if err == nil {
		t.Fatal("a migration referencing a missing function must fail")
	}

	if !tableExists(t, db.DB, "orders_first") {
		t.Error("the migration that succeeded before the failure must stay applied")
	}
	if tableExists(t, db.DB, "orders_second") {
		t.Error("the failing migration's DDL must be rolled back with it")
	}

	report := reportOf(t, newRunner(t, db, tinyCore(1),
		appSource("orders", map[int]string{1: "CREATE TABLE orders_first (id int primary key);"})))
	if applied := appliedOf(t, report, "orders"); len(applied) != 1 || applied[0] != 1 {
		t.Fatalf("history should record exactly the migration that committed, got %v", applied)
	}
}

func reportOf(t *testing.T, runner *database.MigrationRunner) migrationstate.Report {
	t.Helper()

	report, err := runner.Report(context.Background())
	if err != nil {
		t.Fatalf("Report failed: %v", err)
	}
	return report
}

// M8(b): killing the connection a migration is running on. The killed runner
// has to fail — silently succeeding would be the worst outcome — the
// interrupted migration has to leave nothing behind, and a later runner has
// to be able to finish the job.
func TestKillingARunnerLeavesNoPartialMigration(t *testing.T) {
	db := pgtest.New(t)
	slow := appSource("orders", map[int]string{
		1: "CREATE TABLE orders_first (id int primary key);",
		2: "CREATE TABLE orders_slow (id int primary key); SELECT pg_sleep(30);",
	})

	runner := newRunner(t, db, tinyCore(1), slow)

	done := make(chan error, 1)
	go func() { done <- runner.Up(context.Background()) }()

	pid := waitForSleepingBackend(t, db)
	if _, err := db.ExecContext(context.Background(),
		`SELECT pg_terminate_backend($1)`, pid); err != nil {
		t.Fatalf("failed to terminate the migrating backend: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the killed runner reported success; an interrupted migration must fail")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the killed runner never returned")
	}

	if !tableExists(t, db.DB, "orders_first") {
		t.Error("a migration that committed before the interruption must survive it")
	}
	if tableExists(t, db.DB, "orders_slow") {
		t.Error("the interrupted migration must leave no DDL behind")
	}

	// The lock has to have been released with the session, or nothing could
	// ever run here again.
	recovery := newRunner(t, db, tinyCore(1), appSource("orders", map[int]string{
		1: "CREATE TABLE orders_first (id int primary key);",
		2: "CREATE TABLE orders_slow (id int primary key);",
	}))
	if err := recovery.Up(context.Background()); err != nil {
		t.Fatalf("a later runner must be able to finish the job: %v", err)
	}
	if !tableExists(t, db.DB, "orders_slow") {
		t.Error("the recovery run should have applied the previously interrupted migration")
	}
}

func waitForSleepingBackend(t *testing.T, db *pgtest.DB) int {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var pid int
		err := db.QueryRowContext(context.Background(), `
			SELECT pid FROM pg_stat_activity
			WHERE datname = $1 AND query LIKE '%pg_sleep%' AND query NOT LIKE '%pg_stat_activity%'
			LIMIT 1`, db.Name).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("failed to look for the migrating backend: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the slow migration never appeared in pg_stat_activity")
	return 0
}

// M16: a history table that exists but holds nothing.
//
// goose skips its own initialization when the table is already there and then
// rejects a history with no rows, so an empty table left by an interrupted
// run or a stray tool would be permanently unusable. The guard writes the
// sentinel under the lock precisely so this recovers.
func TestHistoryTablesInEveryEmptyShapeStillMigrate(t *testing.T) {
	for _, shape := range []struct {
		name  string
		setup func(t *testing.T, db *pgtest.DB, table string)
	}{
		{"absent", func(*testing.T, *pgtest.DB, string) {}},
		{"empty", func(t *testing.T, db *pgtest.DB, table string) {
			createEmptyHistory(t, db, table)
		}},
		{"sentinel only", func(t *testing.T, db *pgtest.DB, table string) {
			createEmptyHistory(t, db, table)
			if _, err := db.ExecContext(context.Background(),
				`INSERT INTO `+table+` (version_id, is_applied) VALUES (0, true)`); err != nil {
				t.Fatalf("failed to insert the sentinel: %v", err)
			}
		}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			db := pgtest.New(t)
			orders := appSource("orders", map[int]string{1: "CREATE TABLE orders (id int primary key);"})

			shape.setup(t, db, "core_schema_versions")
			shape.setup(t, db, "orders_schema_versions")

			runner := newRunner(t, db, tinyCore(1, 2), orders)
			if err := runner.Up(context.Background()); err != nil {
				t.Fatalf("a %s history should still migrate: %v", shape.name, err)
			}
			if !tableExists(t, db.DB, "orders") {
				t.Error("expected the consumer migration to have been applied")
			}
		})
	}
}

func createEmptyHistory(t *testing.T, db *pgtest.DB, table string) {
	t.Helper()

	_, err := db.ExecContext(context.Background(), `CREATE TABLE `+table+` (
		id SERIAL PRIMARY KEY,
		version_id BIGINT NOT NULL,
		is_applied BOOLEAN NOT NULL,
		tstamp TIMESTAMP NULL DEFAULT NOW()
	)`)
	if err != nil {
		t.Fatalf("failed to create an empty history table: %v", err)
	}
}

// M17: the startup check waits for the migration lock, so it must give up
// rather than hang. An unbounded wait would turn a long migration elsewhere
// into an indefinite outage here, with no message saying why.
func TestTheStartupCheckGivesUpRatherThanWaitingForever(t *testing.T) {
	db := pgtest.New(t)

	ready := newRunner(t, db, tinyCore(1))
	if err := ready.Up(context.Background()); err != nil {
		t.Fatalf("failed to prepare the database: %v", err)
	}

	holder := db.Open(t)
	conn, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatalf("failed to reserve a connection: %v", err)
	}
	defer conn.Close() //nolint:errcheck // released with the test

	// Hold the migration lock from another session, as a migration job would.
	if _, err := conn.ExecContext(context.Background(),
		`SELECT pg_advisory_lock(5712004311)`); err != nil {
		t.Fatalf("failed to take the migration lock: %v", err)
	}

	const wait = 2 * time.Second
	blocked := newRunnerWithConfig(t, database.MigrationConfig{DSN: db.DSN, LockWait: wait}, tinyCore(1))

	start := time.Now()
	err = blocked.CheckAdmission(context.Background(), migrationstate.OpServe)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("the startup check should not succeed while the migration lock is held")
	}
	if !strings.Contains(err.Error(), "migration") {
		t.Fatalf("the error should say what it was waiting for, got: %v", err)
	}
	if elapsed > wait*3 {
		t.Fatalf("the check waited %s for a %s bound", elapsed, wait)
	}
}

// Rolling back writes, and destructively. It therefore has to pass the same
// admission check as a forward migration — otherwise `down` against a legacy
// database would create separated metadata and report success, and `down`
// against an ambiguous one would run destructive SQL that admission exists to
// refuse.
func TestRollbackIsRefusedOnADatabaseMigrationWouldBeRefusedOn(t *testing.T) {
	db := pgtest.New(t)

	// A legacy database: history under goose's default table name.
	pgtest.ApplyMigrationsFS(t, db.DB, tinyCore(1, 2).FS, migrationstate.LegacyHistoryTable, 0)

	runner := newRunner(t, db, tinyCore(1, 2))
	err := runner.DownOne(context.Background(), migrationsource.CoreName)

	if err == nil {
		t.Fatal("rolling back a legacy database must be refused, not silently performed")
	}
	if !errors.Is(err, migrationstate.ErrRefused) {
		t.Fatalf("the refusal should be identifiable as one, got %T: %v", err, err)
	}
	if tableExists(t, db.DB, "core_schema_versions") {
		t.Error("a refused rollback must not create separated history metadata")
	}
	if !tableExists(t, db.DB, "core_t2") {
		t.Error("a refused rollback must not have run any down SQL")
	}
}

// up-one has the same unlocked-plan problem a full run has, and the same
// answer. Without a locked completion check it reports "everything is already
// applied" — exit zero — while the schema has moved past what it ships.
func TestUpOneDoesNotReportSuccessAgainstANewerSchema(t *testing.T) {
	db := pgtest.New(t)

	older := newRunner(t, db, tinyCore(1, 2))
	if err := older.Up(context.Background()); err != nil {
		t.Fatalf("failed to prepare the older generation: %v", err)
	}

	newer := newRunner(t, db, tinyCore(1, 2, 3))
	if err := newer.Up(context.Background()); err != nil {
		t.Fatalf("the newer runner failed: %v", err)
	}

	// The older runner's own history looks complete. Only a locked re-read
	// sees that core is now at a version it does not ship.
	err := older.UpOne(context.Background(), migrationsource.CoreName)
	if err == nil || errors.Is(err, database.ErrNothingPending) {
		t.Fatalf(
			"up-one must not report completion against a schema newer than this build, got %v", err)
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("the refusal should name the unknown version, got: %v", err)
	}
}

package migrationstate_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"testing/fstest"

	"github.com/mr-kaynak/go-core/coremigrations"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

// consumerSource stands in for a module that registers migrations of its own.
const consumerSource = "orders"

// consumerMigrations is a two-migration source, small enough that a reader can
// see the whole fixture. It only needs to create objects goose can apply and
// roll back cleanly inside a transaction.
var consumerMigrations = fstest.MapFS{
	"00001_create_orders.sql": &fstest.MapFile{Data: []byte(
		"-- +goose Up\nCREATE TABLE orders (id uuid PRIMARY KEY);\n" +
			"-- +goose Down\nDROP TABLE orders;\n",
	)},
	"00002_add_orders_total.sql": &fstest.MapFile{Data: []byte(
		"-- +goose Up\nALTER TABLE orders ADD COLUMN total numeric;\n" +
			"-- +goose Down\nALTER TABLE orders DROP COLUMN total;\n",
	)},
}

func coreInventory(t *testing.T) migrationstate.Inventory {
	t.Helper()

	files, err := coremigrations.Inventory(coremigrations.FS())
	if err != nil {
		t.Fatalf("failed to read the core inventory: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("the core inventory is empty; every fixture here assumes core ships migrations")
	}

	versions := make([]int64, 0, len(files))
	for version := range files {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	return migrationstate.Inventory{
		Source:   migrationsource.CoreName,
		Table:    migrationsource.TableName(migrationsource.CoreName),
		Versions: versions,
	}
}

func consumerInventory() migrationstate.Inventory {
	return migrationstate.Inventory{
		Source:   consumerSource,
		Table:    migrationsource.TableName(consumerSource),
		Versions: []int64{1, 2},
	}
}

func classify(
	t *testing.T,
	q migrationstate.Querier,
	tol migrationstate.Tolerances,
	inventories ...migrationstate.Inventory,
) migrationstate.Report {
	t.Helper()

	report, err := migrationstate.Classify(context.Background(), q, inventories, migrationsource.CoreName, tol)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	return report
}

func stateOf(t *testing.T, report migrationstate.Report, source string) migrationstate.SourceState {
	t.Helper()

	for _, candidate := range report.Sources {
		if candidate.Source == source {
			return candidate
		}
	}
	t.Fatalf("the report has no source %q\n%s", source, describe(report))
	return migrationstate.SourceState{}
}

func exec(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()

	if _, err := db.ExecContext(context.Background(), statement, args...); err != nil {
		t.Fatalf("failed to run %q: %v", statement, err)
	}
}

// createHistoryTable reproduces goose's PostgreSQL history schema so a fixture
// can build a history that no migration ever wrote — an empty one, a legacy
// one, a hand-damaged one.
func createHistoryTable(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	exec(t, db, `CREATE TABLE `+table+` (
		id serial PRIMARY KEY,
		version_id bigint NOT NULL,
		is_applied boolean NOT NULL,
		tstamp timestamp NULL DEFAULT now()
	)`)
}

// appendHistoryRow appends a transition, which is what goose itself does: it
// never updates a row, so the newest id for a version is the current truth.
func appendHistoryRow(t *testing.T, db *sql.DB, table string, version int64, isApplied bool) {
	t.Helper()

	exec(t, db, `INSERT INTO `+table+` (version_id, is_applied) VALUES ($1, $2)`, version, isApplied)
}

func countHistoryRows(t *testing.T, db *sql.DB, table string, version int64) int {
	t.Helper()

	var count int
	err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE version_id = $1`, version).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count rows for version %d in %s: %v", version, table, err)
	}
	return count
}

func sameVersions(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func contains(versions []int64, version int64) bool {
	for _, candidate := range versions {
		if candidate == version {
			return true
		}
	}
	return false
}

func TestAnEmptyDatabaseIsFreshAndMayBeMigratedButNotServed(t *testing.T) {
	db := pgtest.New(t)
	inventory := coreInventory(t)

	report := classify(t, db.DB, migrationstate.Tolerances{}, inventory)
	core := stateOf(t, report, migrationsource.CoreName)

	if core.State != migrationstate.StateFresh {
		t.Fatalf("an untouched database should be %s\n%s", migrationstate.StateFresh, describe(report))
	}
	if len(core.Applied) != 0 {
		t.Errorf("nothing has run here, so nothing may be applied\n%s", describe(report))
	}
	if !sameVersions(core.Pending, inventory.Versions) {
		t.Errorf("every shipped migration is pending; got %v, want %v\n%s",
			core.Pending, inventory.Versions, describe(report))
	}

	if err := report.Allows(migrationstate.OpMigrate, migrationstate.Tolerances{}); err != nil {
		t.Errorf("a fresh database is exactly what migrate is for: %v\n%s", err, describe(report))
	}
	if err := report.Allows(migrationstate.OpServe, migrationstate.Tolerances{}); err == nil {
		t.Errorf("serving against a database with no schema must be refused\n%s", describe(report))
	}
}

func TestAFullyMigratedDatabaseIsHealthy(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	inventory := coreInventory(t)
	report := classify(t, db.DB, migrationstate.Tolerances{}, inventory)
	core := stateOf(t, report, migrationsource.CoreName)

	if core.State != migrationstate.StateHealthy {
		t.Fatalf("a fully migrated database should be %s\n%s", migrationstate.StateHealthy, describe(report))
	}
	if len(core.Pending) != 0 {
		t.Errorf("nothing should be pending after every migration ran\n%s", describe(report))
	}
	if !sameVersions(core.Applied, inventory.Versions) {
		t.Errorf("applied versions should be the whole inventory; got %v, want %v\n%s",
			core.Applied, inventory.Versions, describe(report))
	}
	if err := report.Allows(migrationstate.OpServe, migrationstate.Tolerances{}); err != nil {
		t.Errorf("a healthy database must be allowed to serve: %v\n%s", err, describe(report))
	}
}

func TestADatabaseBehindTheInventoryListsExactlyTheRemainingVersions(t *testing.T) {
	const stepsBehind = 2

	db := pgtest.New(t)
	latest := pgtest.LatestCoreVersion(t)
	pgtest.ApplyCoreMigrations(t, db.DB, latest-stepsBehind)

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t))
	core := stateOf(t, report, migrationsource.CoreName)

	if core.State != migrationstate.StatePending {
		t.Fatalf("a database %d migrations behind should be %s\n%s",
			stepsBehind, migrationstate.StatePending, describe(report))
	}
	want := []int64{latest - 1, latest}
	if !sameVersions(core.Pending, want) {
		t.Errorf("pending should be exactly the unapplied tail; got %v, want %v\n%s",
			core.Pending, want, describe(report))
	}
}

// A database created before the histories were separated has all of core's
// schema, recorded under goose's default table name. It is not broken and it
// is not fresh — it needs converting, and baseline is the operation that does
// that.
func TestALegacyHistoryAdmitsOnlyBaseline(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyMigrationsFS(t, db.DB, coremigrations.FS(), migrationstate.LegacyHistoryTable, 0)

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t))
	core := stateOf(t, report, migrationsource.CoreName)

	if core.State != migrationstate.StateLegacy {
		t.Fatalf("a database whose only history is %s should be %s\n%s",
			migrationstate.LegacyHistoryTable, migrationstate.StateLegacy, describe(report))
	}
	if len(report.LegacyApplied) == 0 {
		t.Errorf("the legacy versions have to be reported; the refusal message quotes them\n%s", describe(report))
	}

	if err := report.Allows(migrationstate.OpBaseline, migrationstate.Tolerances{}); err != nil {
		t.Errorf("baseline is the operation that converts this database, so it must be admitted: %v\n%s",
			err, describe(report))
	}
	for _, op := range []migrationstate.Operation{migrationstate.OpServe, migrationstate.OpMigrate} {
		if err := report.Allows(op, migrationstate.Tolerances{}); err == nil {
			t.Errorf("%s must be refused until the legacy history is converted\n%s", op, describe(report))
		}
	}
}

// Core's tables with no history explaining them is not the same as a fresh
// database: running migrations here would try to create tables that already
// exist. The probe is what tells the two apart.
func TestCoreTablesWithoutAHistoryAreAnOrphanSchema(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)
	exec(t, db.DB, `DROP TABLE `+migrationsource.TableName(migrationsource.CoreName))

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t))
	core := stateOf(t, report, migrationsource.CoreName)

	if core.State != migrationstate.StateOrphanSchema {
		t.Fatalf("core tables with a dropped history should be %s\n%s",
			migrationstate.StateOrphanSchema, describe(report))
	}
	if !report.CoreObjectsPresent {
		t.Errorf("the core object probe should have found core's tables\n%s", describe(report))
	}
}

func TestTwoHistoriesWithNoRecordedConversionAreAmbiguous(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	// A second migrator writing its own history against the same database.
	createHistoryTable(t, db.DB, migrationstate.LegacyHistoryTable)
	appendHistoryRow(t, db.DB, migrationstate.LegacyHistoryTable, 1, true)

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t))
	core := stateOf(t, report, migrationsource.CoreName)

	if core.State != migrationstate.StateAmbiguousProvenance {
		t.Fatalf("two histories with applied versions and no conversion record should be %s\n%s",
			migrationstate.StateAmbiguousProvenance, describe(report))
	}
	if report.BaselineRecorded {
		t.Errorf("no conversion was recorded in this fixture\n%s", describe(report))
	}
	if err := report.Allows(migrationstate.OpMigrate, migrationstate.Tolerances{}); err == nil {
		t.Errorf("migrating while another writer may be active must be refused\n%s", describe(report))
	}
}

// A converted database looks identical to the ambiguous one from the outside:
// both histories exist and both hold applied versions, because baseline leaves
// the legacy table in place as evidence. The conversion record is the only
// thing separating them. Without it, every successfully converted database
// would be refused forever — baseline would break the database it just fixed.
func TestARecordedConversionMakesTwoHistoriesUnambiguous(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	createHistoryTable(t, db.DB, migrationstate.LegacyHistoryTable)
	appendHistoryRow(t, db.DB, migrationstate.LegacyHistoryTable, 1, true)

	exec(t, db.DB, `CREATE TABLE `+migrationstate.BaselineAuditTable+` (
		id serial PRIMARY KEY,
		converted_at timestamptz NOT NULL DEFAULT now(),
		legacy_version bigint NOT NULL
	)`)
	exec(t, db.DB,
		`INSERT INTO `+migrationstate.BaselineAuditTable+` (legacy_version) VALUES ($1)`,
		pgtest.LatestCoreVersion(t))

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t))
	core := stateOf(t, report, migrationsource.CoreName)

	if !report.BaselineRecorded {
		t.Fatalf("a non-empty %s is the record of the conversion\n%s",
			migrationstate.BaselineAuditTable, describe(report))
	}
	if core.State == migrationstate.StateAmbiguousProvenance {
		t.Fatalf("a converted database must not be read as two competing writers\n%s", describe(report))
	}
	if core.State != migrationstate.StateHealthy {
		t.Fatalf("after conversion the database classifies on its own history alone; want %s\n%s",
			migrationstate.StateHealthy, describe(report))
	}
	if err := report.Allows(migrationstate.OpServe, migrationstate.Tolerances{}); err != nil {
		t.Errorf("a converted database must be able to serve: %v\n%s", err, describe(report))
	}
}

// An older binary meeting a newer schema. Refusing is the default because the
// build cannot know what the extra migration did; the tolerance exists for the
// documented rollback, where an operator has decided that it can.
func TestVersionsBeyondTheInventoryAreUnknownUnlessTolerated(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	beyond := pgtest.LatestCoreVersion(t) + 1
	appendHistoryRow(t, db.DB, migrationsource.TableName(migrationsource.CoreName), beyond, true)

	inventory := coreInventory(t)

	strict := classify(t, db.DB, migrationstate.Tolerances{}, inventory)
	core := stateOf(t, strict, migrationsource.CoreName)
	if core.State != migrationstate.StateUnknownVersions {
		t.Fatalf("a history recording version %d, which this build does not ship, should be %s\n%s",
			beyond, migrationstate.StateUnknownVersions, describe(strict))
	}
	if !sameVersions(core.Unknown, []int64{beyond}) {
		t.Errorf("the unknown version has to be named; got %v, want [%d]\n%s",
			core.Unknown, beyond, describe(strict))
	}

	// The tolerance changes what the database *is*, not what may be done with
	// it: the same database now classifies as healthy, and every operation
	// follows from that one decision instead of each one re-deciding.
	relaxed := classify(t, db.DB, migrationstate.Tolerances{UnknownAppliedVersions: true}, inventory)
	relaxedCore := stateOf(t, relaxed, migrationsource.CoreName)
	if relaxedCore.State != migrationstate.StateHealthy {
		t.Fatalf("with UnknownAppliedVersions set the same database should classify as %s\n%s",
			migrationstate.StateHealthy, describe(relaxed))
	}
	if !sameVersions(relaxedCore.Unknown, []int64{beyond}) {
		t.Errorf("tolerating the version must not hide it from the report; got %v\n%s",
			relaxedCore.Unknown, describe(relaxed))
	}
}

// goose refuses to advance a history that is missing a version below its
// highest applied one. Calling this "pending" would send the operator to
// `migrate up`, which cannot succeed here — they would run it, watch it fail,
// and still not know that a history row is missing. It gets its own state so
// the message can say "repair the history" instead.
func TestAHistoryMissingAVersionBelowItsMaximumIsGappedNotPending(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	inventory := coreInventory(t)
	middle := inventory.Versions[len(inventory.Versions)/2]
	exec(t, db.DB,
		`DELETE FROM `+inventory.Table+` WHERE version_id = $1`, middle)

	report := classify(t, db.DB, migrationstate.Tolerances{}, inventory)
	core := stateOf(t, report, migrationsource.CoreName)

	if core.State == migrationstate.StatePending {
		t.Fatalf("a gap is not outstanding work; reporting it as %s points at a command that cannot run\n%s",
			migrationstate.StatePending, describe(report))
	}
	if core.State != migrationstate.StateGappedHistory {
		t.Fatalf("a deleted history row should be %s\n%s", migrationstate.StateGappedHistory, describe(report))
	}
	if !sameVersions(core.MissingBelowMax, []int64{middle}) {
		t.Errorf("the missing version has to be named for the operator to repair it; got %v, want [%d]\n%s",
			core.MissingBelowMax, middle, describe(report))
	}
	if contains(core.Applied, middle) {
		t.Errorf("version %d was deleted from the history and must not count as applied\n%s",
			middle, describe(report))
	}
}

// goose appends a row per transition rather than updating one, so a rolled
// back version leaves both rows behind. Reading them as a set — or taking the
// first row per version — would report the version as still applied, and the
// migration that has to be re-run would never appear as pending.
// A version whose newest row says "not applied" is not pending work.
//
// It reads like pending — nothing is applied — but goose refuses to apply any
// version that already has a row in the history, whatever the row says. So
// calling it pending would admit a migration that the very next step rejects,
// which is the worst kind of answer: it passes the check and fails the work.
//
// Stock goose deletes the row when it rolls back and never leaves this shape,
// so a history that has it was written by something else — an import, a
// hand-edit — and needs repair rather than a migration.
func TestAVersionRecordedAsNotAppliedIsBrokenRatherThanPending(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	table := migrationsource.TableName(migrationsource.CoreName)
	latest := pgtest.LatestCoreVersion(t)
	appendHistoryRow(t, db.DB, table, latest, false)

	if got := countHistoryRows(t, db.DB, table, latest); got < 2 {
		t.Fatalf("the fixture needs both rows for version %d, found %d", latest, got)
	}

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t))
	core := stateOf(t, report, migrationsource.CoreName)

	// The newest row still decides whether it counts as applied.
	if contains(core.Applied, latest) {
		t.Fatalf("the newer row says version %d is not applied\n%s", latest, describe(report))
	}
	if contains(core.Pending, latest) {
		t.Fatalf(
			"version %d must not be offered as pending: goose will refuse it because a row already exists\n%s",
			latest, describe(report))
	}
	if !contains(core.Unusable, latest) {
		t.Errorf("version %d should be reported as unusable\n%s", latest, describe(report))
	}
	if core.State != migrationstate.StateGappedHistory {
		t.Errorf(
			"a history goose cannot advance is %s, not %s\n%s",
			migrationstate.StateGappedHistory, core.State, describe(report))
	}

	// And the operation it would have admitted is refused.
	if err := report.Allows(migrationstate.OpMigrate, migrationstate.Tolerances{}); err == nil {
		t.Error("migrating a history goose cannot advance must be refused")
	}
}

// goose stamps a version-0 row into a history table when it initializes one.
// It records that the table exists, not a migration anyone wrote. Counting it
// would make an inventory that starts at 1 look permanently gapped, and an
// empty-but-initialized history look like it had run something.
func TestTheVersionZeroSentinelIsNotAnAppliedMigration(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	table := migrationsource.TableName(migrationsource.CoreName)
	if countHistoryRows(t, db.DB, table, 0) == 0 {
		t.Fatalf(
			"goose no longer stamps a version-0 row into %s, so this fixture exercises nothing. "+
				"Either drop the test or build the sentinel by hand.", table,
		)
	}

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t))
	core := stateOf(t, report, migrationsource.CoreName)

	if contains(core.Applied, 0) {
		t.Fatalf("version 0 is goose's sentinel, not a migration, and must not be applied\n%s", describe(report))
	}
	if core.State != migrationstate.StateHealthy {
		t.Fatalf("the sentinel must not disturb an otherwise healthy database\n%s", describe(report))
	}
}

// Every source keeps its own history and is classified on its own. The
// database-wide signals — the legacy history, orphaned core tables — describe
// core's schema and nothing else: a module registered against a database that
// has not been baselined yet is not itself legacy, and telling its operator to
// run `migrate baseline` for it would be wrong.
func TestEachSourceIsClassifiedOnItsOwnHistory(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyMigrationsFS(t, db.DB, coremigrations.FS(), migrationstate.LegacyHistoryTable, 0)
	pgtest.ApplyMigrationsFS(t, db.DB, consumerMigrations, migrationsource.TableName(consumerSource), 0)

	report := classify(t, db.DB, migrationstate.Tolerances{}, coreInventory(t), consumerInventory())

	core := stateOf(t, report, migrationsource.CoreName)
	if core.State != migrationstate.StateLegacy {
		t.Fatalf("core's own history is absent while the legacy one holds its versions, so core is %s\n%s",
			migrationstate.StateLegacy, describe(report))
	}

	consumer := stateOf(t, report, consumerSource)
	if consumer.State == migrationstate.StateLegacy {
		t.Fatalf("%s has never had a legacy history; there is nothing about it for baseline to convert\n%s",
			consumerSource, describe(report))
	}
	if consumer.State != migrationstate.StateHealthy {
		t.Fatalf("%s applied both of its migrations, so it is %s\n%s",
			consumerSource, migrationstate.StateHealthy, describe(report))
	}
	if !sameVersions(consumer.Applied, []int64{1, 2}) {
		t.Errorf("%s should have both versions applied; got %v\n%s", consumerSource, consumer.Applied, describe(report))
	}

	// Core is the worse of the two, so it governs — and the whole database is
	// held back until core is converted.
	if got := report.State(); got != migrationstate.StateLegacy {
		t.Errorf("the governing state should be core's %s, got %s\n%s",
			migrationstate.StateLegacy, got, describe(report))
	}
}

// An initialized-but-empty history is what a database looks like the moment
// before its first migration runs. Reading it as anything other than fresh
// would make an interrupted first deployment unrecoverable.
func TestAHistoryWithNothingAppliedIsFresh(t *testing.T) {
	inventory := coreInventory(t)

	cases := map[string]func(t *testing.T, db *sql.DB){
		"table exists with no rows": func(_ *testing.T, _ *sql.DB) {},
		"table holds only the version-0 sentinel": func(t *testing.T, db *sql.DB) {
			appendHistoryRow(t, db, inventory.Table, 0, true)
		},
	}

	for label, seed := range cases {
		t.Run(label, func(t *testing.T) {
			db := pgtest.New(t)
			createHistoryTable(t, db.DB, inventory.Table)
			seed(t, db.DB)

			report := classify(t, db.DB, migrationstate.Tolerances{}, inventory)
			core := stateOf(t, report, migrationsource.CoreName)

			if core.State != migrationstate.StateFresh {
				t.Fatalf("an empty history is %s, not a broken database\n%s",
					migrationstate.StateFresh, describe(report))
			}
			if !sameVersions(core.Pending, inventory.Versions) {
				t.Errorf("every migration is still pending; got %v, want %v\n%s",
					core.Pending, inventory.Versions, describe(report))
			}
			if err := report.Allows(migrationstate.OpMigrate, migrationstate.Tolerances{}); err != nil {
				t.Errorf("migrating must be possible from here: %v\n%s", err, describe(report))
			}
		})
	}
}

// The probe is a name-based heuristic, which is fine for its job — separating
// an empty database from a core schema whose history was dropped — but only
// while the names are real. A renamed or removed core table would silently
// weaken it, and the orphan-schema state would stop firing.
func TestCoreObjectProbeMatchesTheShippedSchema(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	probe := migrationstate.CoreObjectProbe()
	if len(probe) == 0 {
		t.Fatal("the probe is empty, so orphan schemas can never be detected")
	}

	for _, table := range probe {
		var exists bool
		err := db.QueryRowContext(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to look up %q: %v", table, err)
		}
		if !exists {
			t.Errorf(
				"CoreObjectProbe names table %q, which the shipped core migrations do not create. "+
					"Update the probe list in classify.go to match core's current tables.",
				table,
			)
		}
	}
}

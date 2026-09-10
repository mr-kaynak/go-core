package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/platform/modcontract"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

// Baseline writes metadata claiming a schema is somewhere it already is. It
// runs once per database, by hand, against production. Every test here is
// about a way that claim could be wrong.

// legacyDatabase builds a database as it looked before the histories were
// separated: core's real migrations, recorded under goose's default table.
func legacyDatabase(t *testing.T, upTo int64) *pgtest.DB {
	t.Helper()

	db := pgtest.New(t)
	pgtest.ApplyMigrationsFS(t, db.DB, coreSource().FS, migrationstate.LegacyHistoryTable, upTo)
	return db
}

func coreMapping(upTo int64) map[string][]int64 {
	versions := make([]int64, 0, upTo)
	for v := int64(1); v <= upTo; v++ {
		versions = append(versions, v)
	}
	return map[string][]int64{"core": versions}
}

func TestBaselineConvertsALegacyDatabase(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := legacyDatabase(t, latest)
	runner := newRunner(t, db, coreSource())
	ctx := context.Background()

	outcome, err := runner.Baseline(ctx, database.BaselinePlan{Mapping: coreMapping(latest)}, false)
	if err != nil {
		t.Fatalf("converting a legacy database should succeed: %v", err)
	}
	if !outcome.Applied {
		t.Fatal("outcome should report that it was applied")
	}

	// The database is now ordinary: healthy, and able to serve.
	report := reportOf(t, runner)
	if state := report.State(); state != migrationstate.StateHealthy {
		t.Fatalf("a converted database should be healthy, got %s\n", state)
	}
	if err := runner.CheckAdmission(ctx, migrationstate.OpServe); err != nil {
		t.Fatalf("a converted database should be serveable: %v", err)
	}

	// The legacy table is the only remaining record of what was there
	// before. A conversion that destroyed its own evidence would leave
	// nothing to check against if it turned out to be wrong.
	if !tableExists(t, db.DB, migrationstate.LegacyHistoryTable) {
		t.Error("the legacy history must be left in place as evidence")
	}
	if !tableExists(t, db.DB, migrationstate.BaselineAuditTable) {
		t.Error("the conversion must be recorded")
	}
}

// A dry run has to exercise the same path an apply does — otherwise it
// reports on checks that will not be the ones that run — and then leave
// nothing behind.
func TestBaselineDryRunChecksEverythingAndWritesNothing(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := legacyDatabase(t, latest)
	runner := newRunner(t, db, coreSource())

	before := relationCount(t, db)

	outcome, err := runner.Baseline(context.Background(),
		database.BaselinePlan{Mapping: coreMapping(latest)}, true)
	if err != nil {
		t.Fatalf("the dry run should succeed: %v", err)
	}
	if outcome.Applied {
		t.Error("a dry run must not report that it applied anything")
	}
	if got := outcome.Histories["core"]; len(got) != int(latest) {
		t.Errorf("the dry run should report what it would write, got %d versions", len(got))
	}

	if after := relationCount(t, db); after != before {
		t.Errorf("a dry run created %d relation(s)", after-before)
	}
	if tableExists(t, db.DB, "core_schema_versions") {
		t.Error("a dry run must not create the history table")
	}
}

// The mapping is the operator's claim about who owned each legacy version.
// Every way of getting it wrong writes a history that says a migration ran
// when it did not, or the reverse.
func TestBaselineRefusesAMappingThatDoesNotDescribeTheDatabase(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)

	cases := map[string]struct {
		mapping map[string][]int64
		want    string
	}{
		"leaves a version unassigned": {
			mapping: coreMapping(latest - 1),
			want:    "not assigned to any source",
		},
		"assigns a version that was never applied": {
			mapping: map[string][]int64{"core": append(versionsTo(latest), latest+1)},
			want:    "not applied in",
		},
		"names an unknown source": {
			mapping: map[string][]int64{"core": versionsTo(latest - 1), "orders": {latest}},
			want:    "unknown source",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := legacyDatabase(t, latest)
			runner := newRunner(t, db, coreSource())

			_, err := runner.Baseline(context.Background(),
				database.BaselinePlan{Mapping: tc.mapping}, false)
			if err == nil {
				t.Fatal("the mapping should have been refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error should explain the problem; want it to mention %q, got: %v", tc.want, err)
			}
			if tableExists(t, db.DB, "core_schema_versions") {
				t.Error("a refused baseline must write nothing")
			}
		})
	}
}

// A legacy history with a gap in it cannot be converted into a core history,
// because goose refuses to advance past a missing version below the highest
// applied one. The refusal has to come here, before the metadata is written —
// afterwards it would look like a successful conversion and break at the next
// deploy.
func TestBaselineRefusesToWriteACoreHistoryWithAGap(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := legacyDatabase(t, latest)

	// A legacy history that records everything except version 3, as a
	// hand-edited or partially-restored database might.
	if _, err := db.ExecContext(context.Background(),
		`DELETE FROM `+migrationstate.LegacyHistoryTable+` WHERE version_id = 3`); err != nil {
		t.Fatalf("failed to build the gapped fixture: %v", err)
	}

	runner := newRunner(t, db, coreSource())
	_, err := runner.Baseline(context.Background(), database.BaselinePlan{
		Mapping: map[string][]int64{"core": omit(versionsTo(latest), 3)},
	}, false)

	if err == nil {
		t.Fatal("a core history with a gap must be refused")
	}
	if !strings.Contains(err.Error(), "exactly 1..N") {
		t.Fatalf("the refusal should explain the prefix rule, got: %v", err)
	}
	if tableExists(t, db.DB, "core_schema_versions") {
		t.Error("a refused baseline must write nothing")
	}
}

// The point of the fingerprint: a history is about to claim the schema is at
// a version, so the schema is checked against what that version produces.
func TestBaselineRefusesASchemaThatDoesNotMatchTheClaim(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := legacyDatabase(t, latest)

	// Exactly the failure a table-existence check would miss: enforcement
	// switched off, every table still present.
	if _, err := db.ExecContext(context.Background(),
		`ALTER TABLE public.template_categories DISABLE TRIGGER ALL`); err != nil {
		t.Fatalf("failed to disable enforcement: %v", err)
	}

	runner := newRunner(t, db, coreSource())
	plan := database.BaselinePlan{Mapping: coreMapping(latest)}

	_, err := runner.Baseline(context.Background(), plan, false)
	if err == nil {
		t.Fatal("a schema that does not match the claimed version must be refused")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("the refusal should say what to do if the difference is understood, got: %v", err)
	}
	if tableExists(t, db.DB, "core_schema_versions") {
		t.Error("a refused baseline must write nothing")
	}

	// Forcing is allowed, and the acceptance is recorded rather than lost.
	plan.Force = true
	outcome, err := runner.Baseline(context.Background(), plan, false)
	if err != nil {
		t.Fatalf("--force should proceed: %v", err)
	}
	if len(outcome.FingerprintDiffs) == 0 {
		t.Error("the differences that were accepted should be reported")
	}
	if forced := auditForced(t, db); !forced {
		t.Error("the audit must record that the conversion was forced")
	}
}

// A mixed history is the case baseline exists for: core's versions plus a
// consumer's, in one stream, with no record of which was which.
func TestBaselineSeparatesAMixedHistory(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := legacyDatabase(t, latest)

	orders := modcontract.MigrationSource{Name: "orders", FS: appSource("orders", map[int]string{
		int(latest) + 1: "CREATE TABLE orders (id int primary key);",
	}).FS}

	// The consumer's migration was applied into the same legacy history.
	pgtest.ApplyMigrationsFS(t, db.DB, orders.FS, migrationstate.LegacyHistoryTable, 0)

	runner := newRunner(t, db, coreSource(), orders)
	outcome, err := runner.Baseline(context.Background(), database.BaselinePlan{
		Mapping: map[string][]int64{
			"core":   versionsTo(latest),
			"orders": {latest + 1},
		},
	}, false)
	if err != nil {
		t.Fatalf("separating a mixed history should succeed: %v", err)
	}

	if got := outcome.Histories["orders"]; len(got) != 1 || got[0] != latest+1 {
		t.Fatalf("the consumer's version should land in its own history, got %v", got)
	}
	if !tableExists(t, db.DB, "orders_schema_versions") {
		t.Fatal("the consumer's history table should exist")
	}

	report := reportOf(t, runner)
	if state := report.State(); state != migrationstate.StateHealthy {
		t.Fatalf("both histories should be healthy afterwards, got %s", state)
	}
}

// Converting twice would record the same migrations as applied a second time.
func TestBaselineRefusesAnAlreadyConvertedDatabase(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := legacyDatabase(t, latest)
	runner := newRunner(t, db, coreSource())
	ctx := context.Background()

	if _, err := runner.Baseline(ctx, database.BaselinePlan{Mapping: coreMapping(latest)}, false); err != nil {
		t.Fatalf("the first conversion should succeed: %v", err)
	}

	_, err := runner.Baseline(ctx, database.BaselinePlan{Mapping: coreMapping(latest)}, false)
	if err == nil {
		t.Fatal("converting an already-converted database must be refused")
	}
}

func versionsTo(n int64) []int64 {
	out := make([]int64, 0, n)
	for v := int64(1); v <= n; v++ {
		out = append(out, v)
	}
	return out
}

func omit(versions []int64, drop int64) []int64 {
	out := make([]int64, 0, len(versions))
	for _, v := range versions {
		if v != drop {
			out = append(out, v)
		}
	}
	return out
}

func relationCount(t *testing.T, db *pgtest.DB) int {
	t.Helper()

	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'`).Scan(&count); err != nil {
		t.Fatalf("failed to count relations: %v", err)
	}
	return count
}

func auditForced(t *testing.T, db *pgtest.DB) bool {
	t.Helper()

	var forced bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT bool_or(forced) FROM `+migrationstate.BaselineAuditTable).Scan(&forced); err != nil {
		t.Fatalf("failed to read the audit: %v", err)
	}
	return forced
}

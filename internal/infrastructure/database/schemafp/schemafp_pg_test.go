package schemafp_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

const schema = "public"

// The fingerprint's job is to answer "is this database really at version N",
// so the properties that matter are: identical schemas fingerprint identically
// no matter how they were built or who looks at them, and the changes that
// make a claimed version a lie are detected.

func TestFingerprintIsStableAcrossOIDAllocation(t *testing.T) {
	pristine := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, pristine.DB, 0)

	churned := pgtest.New(t)
	pgtest.ChurnOIDs(t, churned.DB)
	pgtest.ApplyCoreMigrations(t, churned.DB, 0)

	a := extract(t, pristine.DB)
	b := extract(t, churned.DB)

	diffs, err := schemafp.Compare(a, b)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf(
			"the same schema fingerprinted differently after OID churn — an OID or generated name is leaking into an identity:\n%s",
			schemafp.FormatDifferences(diffs, 10),
		)
	}
}

// PostgreSQL's deparse output qualifies names according to resolution, so a
// fingerprint taken by a role with a different search_path — or against a
// schema where a consumer has shadowed a core function name — must still come
// out identical.
func TestFingerprintIsIndependentOfInspectingSession(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	baseline := extract(t, db.DB)

	// Shadow a core function name in a schema that comes first on the
	// inspecting session's search_path. Unqualified deparse output would now
	// resolve to the consumer's function; qualified output must not move.
	exec(t, db.DB, `CREATE SCHEMA consumer`)
	exec(t, db.DB, `CREATE FUNCTION consumer.notify_outbox_new_message() RETURNS trigger
	                LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`)

	shadowed := db.Open(t)
	exec(t, shadowed, `SET search_path = consumer, public`)

	after := extract(t, shadowed)

	diffs, err := schemafp.Compare(baseline, after)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf(
			"fingerprint changed with the inspecting session's search_path (a shadowing consumer function should not matter):\n%s",
			schemafp.FormatDifferences(diffs, 10),
		)
	}
}

// The case that motivated the enforcement objects: disabling a foreign key's
// internal triggers switches enforcement off without touching
// pg_get_constraintdef or convalidated. A fingerprint that only recorded the
// constraint definition would call this database healthy.
func TestFingerprintDetectsDisabledForeignKeyEnforcement(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	before := extract(t, db.DB)
	exec(t, db.DB, `ALTER TABLE public.template_categories DISABLE TRIGGER ALL`)
	after := extract(t, db.DB)

	diffs, err := schemafp.Compare(before, after)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	var enforcement []schemafp.Difference
	for _, d := range diffs {
		if d.Kind == schemafp.KindEnforcement {
			enforcement = append(enforcement, d)
		}
	}
	if len(enforcement) == 0 {
		t.Fatalf(
			"disabled foreign key enforcement went undetected; diffs were:\n%s",
			schemafp.FormatDifferences(diffs, 10),
		)
	}
	for _, d := range enforcement {
		if d.Class != schemafp.DiffMismatched {
			t.Errorf("expected a mismatch, got %s for %s", d.Class, d.Identity)
		}
	}

	// Every constraint definition is unchanged, which is exactly why the
	// enforcement objects have to exist.
	for _, d := range diffs {
		if d.Kind == schemafp.KindConstraint {
			t.Errorf("constraint definitions should be unchanged by DISABLE TRIGGER, got: %s", d)
		}
	}
}

// A self-referencing foreign key puts both an UPDATE check trigger and an
// UPDATE action trigger on the same relation with the same tgtype; only the
// trigger function tells them apart. Disabling one must not be masked by the
// other still being enabled.
func TestFingerprintDistinguishesSelfReferencingForeignKeyRoles(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	roles := internalTriggerNames(t, db.DB, "template_categories")
	if len(roles) < 4 {
		t.Fatalf("expected the self-referencing FK to install at least 4 internal triggers, got %d: %v", len(roles), roles)
	}

	before := extract(t, db.DB)
	seen := make(map[string]bool, len(roles))

	for _, trigger := range roles {
		exec(t, db.DB, `ALTER TABLE public.template_categories DISABLE TRIGGER "`+trigger+`"`)
		after := extract(t, db.DB)

		diffs, err := schemafp.Compare(before, after)
		if err != nil {
			t.Fatalf("Compare failed: %v", err)
		}

		var identities []string
		for _, d := range diffs {
			if d.Kind == schemafp.KindEnforcement && d.Class == schemafp.DiffMismatched {
				identities = append(identities, d.Identity)
			}
		}
		if len(identities) != 1 {
			t.Fatalf(
				"disabling %q should surface exactly one enforcement mismatch, got %d:\n%s",
				trigger, len(identities), schemafp.FormatDifferences(diffs, 10),
			)
		}
		if seen[identities[0]] {
			t.Fatalf(
				"two different triggers map to the same enforcement identity %q — the identity is not unique",
				identities[0],
			)
		}
		seen[identities[0]] = true

		exec(t, db.DB, `ALTER TABLE public.template_categories ENABLE TRIGGER "`+trigger+`"`)
	}
}

func TestFingerprintDetectsAlteredFunction(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	before := extract(t, db.DB)
	exec(t, db.DB, `CREATE OR REPLACE FUNCTION public.notify_outbox_new_message() RETURNS trigger
	                LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`)
	after := extract(t, db.DB)

	diffs, err := schemafp.Compare(before, after)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}
	if !hasDiff(diffs, schemafp.KindFunction, schemafp.DiffMismatched) {
		t.Fatalf("an altered function body went undetected:\n%s", schemafp.FormatDifferences(diffs, 10))
	}
}

func TestFingerprintReportsExtraIndexOnCoreTable(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	before := extract(t, db.DB)
	exec(t, db.DB, `CREATE INDEX idx_users_extra_local ON public.users (created_at)`)
	after := extract(t, db.DB)

	diffs, err := schemafp.Compare(before, after)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}
	if !hasDiff(diffs, schemafp.KindIndex, schemafp.DiffExtra) {
		t.Fatalf("an extra index on a core table should be reported:\n%s", schemafp.FormatDifferences(diffs, 10))
	}
}

// A consumer's own table with a foreign key into users is the supported
// extension pattern. Once the snapshot is narrowed to core-owned objects it
// must produce no differences at all — otherwise every consumer would need
// --force to baseline.
func TestFingerprintIgnoresConsumerTablesAndTheirForeignKeys(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	baseline := extract(t, db.DB)
	coreOwned := baseline.Identities()

	exec(t, db.DB, `CREATE TABLE public.orders (
		id uuid PRIMARY KEY,
		user_id uuid NOT NULL REFERENCES public.users(id),
		total numeric NOT NULL
	)`)
	exec(t, db.DB, `CREATE INDEX idx_orders_user ON public.orders (user_id)`)

	after := extract(t, db.DB).Filter(coreOwned)

	diffs, err := schemafp.Compare(baseline, after)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf(
			"consumer tables referencing core tables must not affect the core fingerprint:\n%s",
			schemafp.FormatDifferences(diffs, 10),
		)
	}
}

// Constraint names are only unique per table, so two tables may both have a
// constraint called fk_user. Their enforcement entries must stay distinct.
func TestFingerprintSeparatesSameNamedConstraintsOnDifferentTables(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 0)

	exec(t, db.DB, `CREATE TABLE public.first_child (
		id uuid PRIMARY KEY,
		user_id uuid NOT NULL,
		CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES public.users(id)
	)`)
	exec(t, db.DB, `CREATE TABLE public.second_child (
		id uuid PRIMARY KEY,
		user_id uuid NOT NULL,
		CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES public.users(id)
	)`)

	snapshot := extract(t, db.DB)

	identities := make(map[string]int)
	for _, o := range snapshot.Objects {
		if o.Kind == schemafp.KindEnforcement && strings.Contains(o.Identity, ".fk_user|") {
			identities[o.Identity]++
		}
	}
	if len(identities) == 0 {
		t.Fatal("expected enforcement entries for the two fk_user constraints")
	}
	for identity, count := range identities {
		if count != 1 {
			t.Errorf("enforcement identity %q appears %d times; identities must be unique", identity, count)
		}
	}

	var firstChild, secondChild int
	for identity := range identities {
		switch {
		case strings.HasPrefix(identity, "public.first_child.fk_user|"):
			firstChild++
		case strings.HasPrefix(identity, "public.second_child.fk_user|"):
			secondChild++
		}
	}
	if firstChild == 0 || secondChild == 0 {
		t.Fatalf(
			"both tables' fk_user constraints must be represented (first_child=%d, second_child=%d): %v",
			firstChild, secondChild, identities,
		)
	}
}

func TestCompareRefusesSnapshotsFromDifferentEnvironments(t *testing.T) {
	db := pgtest.New(t)
	pgtest.ApplyCoreMigrations(t, db.DB, 1)

	current := extract(t, db.DB)
	foreign := *current
	foreign.Environment.ServerMajor = current.Environment.ServerMajor + 1

	if _, err := schemafp.Compare(&foreign, current); err == nil {
		t.Fatal("comparing snapshots from different major versions should be refused")
	}
}

func extract(t *testing.T, db *sql.DB) *schemafp.Snapshot {
	t.Helper()

	snapshot, err := schemafp.Extract(context.Background(), db, schema)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	return snapshot
}

func exec(t *testing.T, db *sql.DB, statement string) {
	t.Helper()

	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("failed to run %q: %v", firstLine(statement), err)
	}
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx] + " ..."
	}
	return s
}

func internalTriggerNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), `
		SELECT t.tgname
		FROM pg_catalog.pg_trigger t
		JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND t.tgisinternal
		ORDER BY t.tgname`, schema, table)
	if err != nil {
		t.Fatalf("failed to list internal triggers: %v", err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced below

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan a trigger name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate trigger names: %v", err)
	}
	return names
}

func hasDiff(diffs []schemafp.Difference, kind schemafp.Kind, class schemafp.DiffClass) bool {
	for _, d := range diffs {
		if d.Kind == kind && d.Class == class {
			return true
		}
	}
	return false
}

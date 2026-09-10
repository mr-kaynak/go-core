// Package schemafp extracts a canonical fingerprint of the schema objects a
// migration history claims to have created.
//
// A migration history records that version N was applied; it never records
// what version N did. Baselining an existing database onto a separated history
// therefore needs independent evidence that the schema really is at version N.
// Counting tables is not that evidence: core migrations 10-16 create no tables
// at all — 00016's entire effect is replacing an index, 00003's is a trigger.
// This package compares the catalog itself.
//
// # Determinism
//
// PostgreSQL's deparse output (pg_get_triggerdef, pg_get_functiondef, ...)
// qualifies referenced objects according to name resolution, so the same
// schema renders differently under different search_path settings. Every
// extraction therefore runs inside one transaction with a fixed environment
// (see fixedEnvironment) and that environment is recorded in the snapshot;
// comparing snapshots taken under different environments is refused.
//
// # What it does not cover
//
// Catalog state only. Row data, backfills and anything else invisible to the
// system catalogs are out of scope and must be established another way.
package schemafp

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Kind labels the sort of catalog object an [Object] describes. The kind is
// part of an object's identity: a table and an index may share a name.
type Kind string

const (
	KindTable       Kind = "table"
	KindColumn      Kind = "column"
	KindConstraint  Kind = "constraint"
	KindIndex       Kind = "index"
	KindTrigger     Kind = "trigger"
	KindEnforcement Kind = "enforcement"
	KindFunction    Kind = "function"
)

// Object is one catalog fact: a stable identity and the state recorded for it.
//
// Identity never contains an OID or a system-generated name, so it survives
// dump/restore and differing creation orders. State holds whatever would make
// the object behave differently — a column's type and nullability, an index's
// definition and validity, a constraint's enforcement being switched off.
type Object struct {
	Kind     Kind   `json:"kind"`
	Identity string `json:"identity"`
	State    string `json:"state,omitempty"`
}

// Environment records the extraction settings a snapshot was taken under.
// Snapshots taken under different environments are not comparable, because
// PostgreSQL's deparse output depends on them.
type Environment struct {
	ServerVersionNum int    `json:"serverVersionNum"`
	ServerMajor      int    `json:"serverMajor"`
	Schema           string `json:"schema"`
	SearchPath       string `json:"searchPath"`
	Collation        string `json:"collation"`
}

// Snapshot is the full set of catalog objects found in one schema, sorted
// canonically.
type Snapshot struct {
	Environment Environment `json:"environment"`
	Objects     []Object    `json:"objects"`
}

// fixedEnvironment is applied to every extraction transaction.
//
// search_path is emptied rather than set to a schema: with nothing to elide,
// the deparse functions qualify every reference, which is what makes the
// output independent of the inspecting role's own search_path. Sorting is done
// in Go under a byte comparison, so no server collation participates.
const (
	fixedSearchPath = ""
	fixedCollation  = "C (client-side byte order)"
)

// Extract reads the catalog for one schema and returns its canonical snapshot.
//
// It runs in its own read-only transaction so the fixed environment is applied
// with SET LOCAL and cannot leak into the caller's session.
func Extract(ctx context.Context, db *sql.DB, schema string) (*Snapshot, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("schemafp: failed to begin extraction transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only transaction, rollback is the happy path

	return ExtractTx(ctx, tx, schema)
}

// ExtractTx is [Extract] against a caller-supplied transaction. Baseline uses
// it to read the catalog inside the same transaction that writes the history,
// so the evidence and the decision cannot drift apart.
func ExtractTx(ctx context.Context, tx *sql.Tx, schema string) (*Snapshot, error) {
	if schema == "" {
		return nil, fmt.Errorf("schemafp: schema must not be empty")
	}

	// Restored before returning: a caller that extracts inside its own
	// transaction — baseline does, so the evidence and the decision come from
	// one read — would otherwise find every later unqualified name
	// unresolvable, having never asked for that.
	var previousSearchPath string
	if err := tx.QueryRowContext(ctx, `SELECT current_setting('search_path')`).Scan(&previousSearchPath); err != nil {
		return nil, fmt.Errorf("schemafp: failed to read search_path: %w", err)
	}
	defer func() {
		// Best effort: an error here cannot mask the extraction's own result,
		// and the transaction is the caller's to abandon if it matters.
		tx.ExecContext(ctx, `SET LOCAL search_path = `+quoteSetting(previousSearchPath)) //nolint:errcheck // best effort; see above
	}()

	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path = ''`); err != nil {
		return nil, fmt.Errorf("schemafp: failed to fix search_path: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL row_security = off`); err != nil {
		return nil, fmt.Errorf("schemafp: failed to disable row security: %w", err)
	}

	env := Environment{Schema: schema, SearchPath: fixedSearchPath, Collation: fixedCollation}
	if err := tx.QueryRowContext(ctx,
		`SELECT current_setting('server_version_num')::int`,
	).Scan(&env.ServerVersionNum); err != nil {
		return nil, fmt.Errorf("schemafp: failed to read server version: %w", err)
	}
	env.ServerMajor = env.ServerVersionNum / 10000

	var objects []Object
	for _, q := range queries {
		found, err := collect(ctx, tx, q, schema)
		if err != nil {
			return nil, err
		}
		objects = append(objects, found...)
	}
	sortObjects(objects)

	return &Snapshot{Environment: env, Objects: objects}, nil
}

// quoteSetting renders a GUC value as a single-quoted literal. SET takes no
// parameters, and a search_path is a comma-separated list that may contain
// quoted identifiers.
func quoteSetting(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

type query struct {
	kind Kind
	sql  string
}

func collect(ctx context.Context, tx *sql.Tx, q query, schema string) ([]Object, error) {
	rows, err := tx.QueryContext(ctx, q.sql, schema)
	if err != nil {
		return nil, fmt.Errorf("schemafp: failed to read %s objects: %w", q.kind, err)
	}
	defer rows.Close()

	var out []Object
	for rows.Next() {
		var identity, state string
		if err := rows.Scan(&identity, &state); err != nil {
			return nil, fmt.Errorf("schemafp: failed to scan a %s row: %w", q.kind, err)
		}
		out = append(out, Object{Kind: q.kind, Identity: identity, State: state})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schemafp: failed to iterate %s rows: %w", q.kind, err)
	}
	return out, nil
}

func sortObjects(objects []Object) {
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Kind != objects[j].Kind {
			return objects[i].Kind < objects[j].Kind
		}
		if objects[i].Identity != objects[j].Identity {
			return objects[i].Identity < objects[j].Identity
		}
		return objects[i].State < objects[j].State
	})
}

// Identities returns the set of "kind\videntity" keys in the snapshot. It is
// the ownership vocabulary: a version owns the identities that appeared when
// it was applied.
func (s *Snapshot) Identities() map[string]struct{} {
	set := make(map[string]struct{}, len(s.Objects))
	for _, o := range s.Objects {
		set[o.Key()] = struct{}{}
	}
	return set
}

// Key is the object's identity across kinds, used as a map key when diffing.
func (o Object) Key() string { return string(o.Kind) + "\v" + o.Identity }

// Filter returns the objects whose keys are in keep, preserving order. It is
// how a full-database snapshot is narrowed to the core-owned subset, so a
// consumer's own tables never take part in the comparison.
func (s *Snapshot) Filter(keep map[string]struct{}) *Snapshot {
	out := &Snapshot{Environment: s.Environment}
	for _, o := range s.Objects {
		if _, ok := keep[o.Key()]; ok {
			out.Objects = append(out.Objects, o)
		}
	}
	return out
}

// queries are the catalog reads that make up a fingerprint. Every one of them
// is parameterized by schema and returns (identity, state).
// bookkeepingPredicate excludes migration history tables from every read.
//
// They are not schema in the sense this package means. A fingerprint answers
// "did this migration run", and the tables recording that answer cannot be
// part of it: a database being baselined has the old history and not the new
// one, by definition, so including them would report the very difference
// baseline exists to remove. Their contents are read by classification, which
// is a different question.
const bookkeepingPredicate = ` AND NOT (
	%[1]s.relname = 'goose_db_version'
	OR %[1]s.relname = 'core_migration_baseline'
	OR %[1]s.relname LIKE '%%\_schema\_versions'
)`

// excludeBookkeeping applies the predicate to the relation alias a query uses.
func excludeBookkeeping(alias string) string {
	return fmt.Sprintf(bookkeepingPredicate, alias)
}

var queries = []query{
	{
		kind: KindTable,
		sql: `
SELECT n.nspname || '.' || c.relname AS identity, '' AS state
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p')` + excludeBookkeeping("c"),
	},
	{
		// Column order is deliberately excluded: adding a column changes every
		// later column's attnum without changing behavior.
		kind: KindColumn,
		sql: `
SELECT n.nspname || '.' || c.relname || '.' || a.attname AS identity,
       'type=' || pg_catalog.format_type(a.atttypid, a.atttypmod)
         || '|notnull=' || a.attnotnull::text
         || '|default=' || COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '')
         || '|identity=' || a.attidentity::text
         || '|generated=' || a.attgenerated::text AS state
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped` +
			excludeBookkeeping("c"),
	},
	{
		kind: KindConstraint,
		sql: `
SELECT n.nspname || '.' || c.relname || '.' || con.conname AS identity,
       pg_catalog.pg_get_constraintdef(con.oid) || '|validated=' || con.convalidated::text AS state
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND con.conrelid <> 0` + excludeBookkeeping("c"),
	},
	{
		kind: KindIndex,
		sql: `
SELECT n.nspname || '.' || ic.relname AS identity,
       pg_catalog.pg_get_indexdef(i.indexrelid)
         || '|valid=' || i.indisvalid::text
         || '|ready=' || i.indisready::text AS state
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
JOIN pg_catalog.pg_namespace n ON n.oid = ic.relnamespace
JOIN pg_catalog.pg_class tc ON tc.oid = i.indrelid
WHERE n.nspname = $1` + excludeBookkeeping("tc"),
	},
	{
		kind: KindTrigger,
		sql: `
SELECT n.nspname || '.' || c.relname || '.' || t.tgname AS identity,
       pg_catalog.pg_get_triggerdef(t.oid) || '|enabled=' || t.tgenabled::text AS state
FROM pg_catalog.pg_trigger t
JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND NOT t.tgisinternal` + excludeBookkeeping("c"),
	},
	{
		// Constraint enforcement. Foreign keys are enforced by internal
		// triggers whose names carry OIDs, so they cannot be identified by
		// name — but their enabled state is exactly what "ALTER TABLE ...
		// DISABLE TRIGGER ALL" turns off, and neither pg_get_constraintdef nor
		// convalidated reflects that. They are therefore identified by the
		// owning constraint, the relation they fire on, their function and
		// their type.
		//
		// All four parts are needed: a self-referencing foreign key puts both
		// its check and its action trigger on the same relation with the same
		// tgtype, and only the function tells them apart.
		kind: KindEnforcement,
		sql: `
SELECT cn.nspname || '.' || cr.relname || '.' || con.conname
         || '|fires_on=' || tn.nspname || '.' || tr.relname
         || '|fn=' || pn.nspname || '.' || p.proname
         || '|tgtype=' || t.tgtype::text AS identity,
       'enabled=' || t.tgenabled::text AS state
FROM pg_catalog.pg_trigger t
JOIN pg_catalog.pg_constraint con ON con.oid = t.tgconstraint
JOIN pg_catalog.pg_class cr ON cr.oid = con.conrelid
JOIN pg_catalog.pg_namespace cn ON cn.oid = cr.relnamespace
JOIN pg_catalog.pg_class tr ON tr.oid = t.tgrelid
JOIN pg_catalog.pg_namespace tn ON tn.oid = tr.relnamespace
JOIN pg_catalog.pg_proc p ON p.oid = t.tgfoid
JOIN pg_catalog.pg_namespace pn ON pn.oid = p.pronamespace
WHERE cn.nspname = $1 AND t.tgisinternal` + excludeBookkeeping("cr"),
	},
	{
		// prokind 'f' only: pg_get_functiondef errors on aggregates and
		// window functions.
		kind: KindFunction,
		sql: `
SELECT n.nspname || '.' || p.proname
         || '(' || pg_catalog.pg_get_function_identity_arguments(p.oid) || ')' AS identity,
       pg_catalog.pg_get_functiondef(p.oid) AS state
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname = $1 AND p.prokind = 'f'`,
	},
}

// DiffClass says how an object differs between an expected and an observed
// snapshot.
type DiffClass string

const (
	// DiffMissing: expected by the fingerprint, absent from the database.
	DiffMissing DiffClass = "missing"
	// DiffMismatched: present, but its recorded state differs.
	DiffMismatched DiffClass = "mismatched"
	// DiffExtra: present on a core-owned table but not expected. Consumers
	// are contracted not to alter core tables, so this is reported rather
	// than ignored.
	DiffExtra DiffClass = "extra"
)

// Difference is one classified discrepancy.
type Difference struct {
	Class    DiffClass
	Kind     Kind
	Identity string
	Expected string
	Observed string
}

func (d Difference) String() string {
	switch d.Class {
	case DiffMissing:
		return fmt.Sprintf("missing %s %s", d.Kind, d.Identity)
	case DiffExtra:
		return fmt.Sprintf("unexpected %s %s (%s)", d.Kind, d.Identity, d.Observed)
	default:
		return fmt.Sprintf("%s %s differs:\n      expected: %s\n      observed: %s",
			d.Kind, d.Identity, d.Expected, d.Observed)
	}
}

// Compare classifies every difference between the expected snapshot and the
// observed one. An empty result means the observed schema matches the
// fingerprint exactly.
//
// It refuses to compare snapshots taken under different extraction
// environments, because their deparse output is not comparable.
func Compare(expected, observed *Snapshot) ([]Difference, error) {
	if err := expected.Environment.compatibleWith(observed.Environment); err != nil {
		return nil, err
	}

	expectedByKey := make(map[string]Object, len(expected.Objects))
	for _, o := range expected.Objects {
		expectedByKey[o.Key()] = o
	}
	observedByKey := make(map[string]Object, len(observed.Objects))
	for _, o := range observed.Objects {
		observedByKey[o.Key()] = o
	}

	var diffs []Difference
	for _, want := range expected.Objects {
		have, present := observedByKey[want.Key()]
		switch {
		case !present:
			diffs = append(diffs, Difference{
				Class: DiffMissing, Kind: want.Kind, Identity: want.Identity, Expected: want.State,
			})
		case have.State != want.State:
			diffs = append(diffs, Difference{
				Class: DiffMismatched, Kind: want.Kind, Identity: want.Identity,
				Expected: want.State, Observed: have.State,
			})
		}
	}
	for _, have := range observed.Objects {
		if _, present := expectedByKey[have.Key()]; !present {
			diffs = append(diffs, Difference{
				Class: DiffExtra, Kind: have.Kind, Identity: have.Identity, Observed: have.State,
			})
		}
	}

	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Class != diffs[j].Class {
			return diffs[i].Class < diffs[j].Class
		}
		if diffs[i].Kind != diffs[j].Kind {
			return diffs[i].Kind < diffs[j].Kind
		}
		return diffs[i].Identity < diffs[j].Identity
	})
	return diffs, nil
}

func (e Environment) compatibleWith(other Environment) error {
	switch {
	case e.ServerMajor != other.ServerMajor:
		return fmt.Errorf(
			"schemafp: fingerprint was taken on PostgreSQL %d but this database is %d; "+
				"catalog output differs across major versions",
			e.ServerMajor, other.ServerMajor,
		)
	case e.Schema != other.Schema:
		return fmt.Errorf("schemafp: fingerprint covers schema %q but %q was inspected", e.Schema, other.Schema)
	case e.SearchPath != other.SearchPath || e.Collation != other.Collation:
		return fmt.Errorf("schemafp: fingerprint was taken under a different extraction environment")
	}
	return nil
}

// FormatDifferences renders diffs for an operator, grouped by class and capped
// so a wholesale mismatch does not bury the first useful line.
func FormatDifferences(diffs []Difference, limit int) string {
	if len(diffs) == 0 {
		return "no differences"
	}
	var b strings.Builder
	counts := map[DiffClass]int{}
	for _, d := range diffs {
		counts[d.Class]++
	}
	fmt.Fprintf(&b, "%d difference(s): %d missing, %d mismatched, %d extra\n",
		len(diffs), counts[DiffMissing], counts[DiffMismatched], counts[DiffExtra])

	for i, d := range diffs {
		if limit > 0 && i >= limit {
			fmt.Fprintf(&b, "    ... and %d more\n", len(diffs)-limit)
			break
		}
		fmt.Fprintf(&b, "    - %s\n", d)
	}
	return b.String()
}

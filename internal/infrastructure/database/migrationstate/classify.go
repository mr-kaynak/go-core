package migrationstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// coreObjectProbe is how "core has been installed here" is detected when no
// history explains it.
//
// It is a heuristic — a name-based check cannot prove ownership — but it only
// has to separate "empty database" from "core schema with its history
// missing", and a consumer owning all of these by coincidence is not a case
// worth designing for. TestCoreObjectProbeMatchesTheShippedSchema keeps the
// list from going stale as core's tables change.
var coreObjectProbe = []string{
	"users",
	"roles",
	"permissions",
	"refresh_tokens",
	"outbox_messages",
}

// CoreObjectProbe is the probe list, exported so a test can assert every name
// really is created by the shipped migrations.
func CoreObjectProbe() []string {
	return append([]string(nil), coreObjectProbe...)
}

// Classify reads the database and reports what its migration metadata means.
//
// It only reads. Every decision that follows — refuse to serve, run
// migrations, convert a legacy history — is the caller's, made against the
// admission matrix. PostgreSQL only: the catalog lookups and the
// latest-row-per-version query are not portable, and neither is the behavior
// being classified.
//
// coreSource names which inventory carries the database-wide signals; the
// legacy history and orphaned core objects say nothing about a consumer's own
// schema.
func Classify(
	ctx context.Context,
	q Querier,
	inventories []Inventory,
	coreSource string,
	tol Tolerances,
) (Report, error) {
	var report Report

	legacyExists, err := tableExists(ctx, q, LegacyHistoryTable)
	if err != nil {
		return report, err
	}
	if legacyExists {
		if report.LegacyApplied, err = appliedVersions(ctx, q, LegacyHistoryTable); err != nil {
			return report, err
		}
	}

	if report.BaselineRecorded, err = tableHasRows(ctx, q, BaselineAuditTable); err != nil {
		return report, err
	}
	if report.CoreObjectsPresent, err = anyTableExists(ctx, q, coreObjectProbe); err != nil {
		return report, err
	}

	for _, inventory := range inventories {
		state, stateErr := classifySource(ctx, q, inventory, report, inventory.Source == coreSource, tol)
		if stateErr != nil {
			return report, stateErr
		}
		report.Sources = append(report.Sources, state)
	}

	return report, nil
}

func classifySource(
	ctx context.Context,
	q Querier,
	inventory Inventory,
	report Report,
	isCore bool,
	tol Tolerances,
) (SourceState, error) {
	state := SourceState{Source: inventory.Source, Table: inventory.Table}

	exists, err := tableExists(ctx, q, inventory.Table)
	if err != nil {
		return state, err
	}
	if exists {
		if state.Applied, err = appliedVersions(ctx, q, inventory.Table); err != nil {
			return state, err
		}
	}

	// Database-wide signals outrank this source's own reading, but only for
	// core: a legacy history says nothing about a consumer's schema.
	if isCore {
		switch {
		case len(state.Applied) > 0 && len(report.LegacyApplied) > 0 && !report.BaselineRecorded:
			state.State = StateAmbiguousProvenance
			return state, nil
		case len(state.Applied) == 0 && len(report.LegacyApplied) > 0:
			state.State = StateLegacy
			return state, nil
		case len(state.Applied) == 0 && report.CoreObjectsPresent:
			state.State = StateOrphanSchema
			return state, nil
		}
	}

	// Both are computed before either is acted on. A history can have an
	// unknown version *and* a gap, and reporting only the unknown one sends
	// the operator to the rollback tolerance when what they actually have to
	// do is repair the missing version.
	state.Unknown = difference(state.Applied, inventory.Versions)
	state.MissingBelowMax = missingBelowMax(state.Applied, inventory.Versions)
	state.Unusable = unusableVersions(ctx, q, inventory.Table, state.Applied)

	// goose refuses to advance a history missing a version below its highest
	// applied one, so that is a broken history rather than pending work.
	if len(state.MissingBelowMax) > 0 {
		state.State = StateGappedHistory
		return state, nil
	}
	if len(state.Unknown) > 0 && !tol.UnknownAppliedVersions {
		state.State = StateUnknownVersions
		return state, nil
	}

	if len(state.Unusable) > 0 {
		state.State = StateGappedHistory
		state.MissingBelowMax = state.Unusable
		return state, nil
	}

	state.Pending = difference(inventory.Versions, state.Applied)
	switch {
	case len(state.Applied) == 0:
		state.State = StateFresh
	case len(state.Pending) > 0:
		state.State = StatePending
	default:
		state.State = StateHealthy
	}
	return state, nil
}

func tableExists(ctx context.Context, q Querier, table string) (bool, error) {
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
		return false, fmt.Errorf("migrationstate: failed to look up table %q: %w", table, err)
	}
	return exists, nil
}

func anyTableExists(ctx context.Context, q Querier, tables []string) (bool, error) {
	for _, table := range tables {
		exists, err := tableExists(ctx, q, table)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

func tableHasRows(ctx context.Context, q Querier, table string) (bool, error) {
	exists, err := tableExists(ctx, q, table)
	if err != nil || !exists {
		return false, err
	}

	var populated bool
	// The table name is a package constant, never caller-supplied.
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+table+`)`,
	).Scan(&populated); err != nil {
		return false, fmt.Errorf("migrationstate: failed to read %q: %w", table, err)
	}
	return populated, nil
}

// appliedVersions returns the positive versions a history currently considers
// applied.
//
// goose appends a row per transition rather than updating one, so a version
// rolled back and reapplied has several rows and only the newest counts.
// Version 0 is goose's initialization sentinel, not a migration, and is
// excluded — "applied versions" everywhere else in this package means real
// migrations.
func appliedVersions(ctx context.Context, q Querier, table string) ([]int64, error) {
	// The table name reaches here already validated (a source name matching a
	// restricted alphabet plus a fixed suffix) or as a package constant.
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT ON (version_id) version_id, is_applied
		FROM `+table+`
		ORDER BY version_id, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("migrationstate: failed to read history %q: %w", table, err)
	}
	defer rows.Close()

	var applied []int64
	for rows.Next() {
		var version int64
		var isApplied bool
		if err := rows.Scan(&version, &isApplied); err != nil {
			return nil, fmt.Errorf("migrationstate: failed to scan a row of %q: %w", table, err)
		}
		if isApplied && version > 0 {
			applied = append(applied, version)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrationstate: failed to iterate %q: %w", table, err)
	}

	sort.Slice(applied, func(i, j int) bool { return applied[i] < applied[j] })
	return applied, nil
}

// unusableVersions finds versions the history records as not applied.
//
// Such a version looks pending — nothing has been applied — but goose refuses
// to apply any version that already has a row, whatever the row says, so
// treating it as pending would produce an admission that the very next step
// rejects. Stock goose deletes the row when rolling back and never leaves
// this shape; a history that has it was written by something else, and the
// honest answer is that it needs repair.
func unusableVersions(ctx context.Context, q Querier, table string, applied []int64) []int64 {
	recorded, err := recordedVersions(ctx, q, table)
	if err != nil || len(recorded) == 0 {
		return nil
	}

	isApplied := make(map[int64]struct{}, len(applied))
	for _, v := range applied {
		isApplied[v] = struct{}{}
	}

	var unusable []int64
	for _, v := range recorded {
		if _, ok := isApplied[v]; !ok {
			unusable = append(unusable, v)
		}
	}
	sort.Slice(unusable, func(i, j int) bool { return unusable[i] < unusable[j] })
	return unusable
}

// recordedVersions are the positive versions that have any row at all.
func recordedVersions(ctx context.Context, q Querier, table string) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT version_id FROM `+table+` WHERE version_id > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		out = append(out, version)
	}
	return out, rows.Err()
}

// difference returns the members of a that are not in b, ascending.
func difference(a, b []int64) []int64 {
	if len(a) == 0 {
		return nil
	}
	in := make(map[int64]struct{}, len(b))
	for _, v := range b {
		in[v] = struct{}{}
	}

	var out []int64
	for _, v := range a {
		if _, present := in[v]; !present {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// missingBelowMax returns inventory versions that are unapplied and older than
// the newest applied one — the shape goose rejects.
func missingBelowMax(applied, inventory []int64) []int64 {
	if len(applied) == 0 {
		return nil
	}
	var highest int64
	appliedSet := make(map[int64]struct{}, len(applied))
	for _, v := range applied {
		appliedSet[v] = struct{}{}
		if v > highest {
			highest = v
		}
	}

	var missing []int64
	for _, v := range inventory {
		if v >= highest {
			continue
		}
		if _, present := appliedSet[v]; !present {
			missing = append(missing, v)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return missing
}

// ErrRefused wraps every refusal the admission matrix produces, so a caller
// can tell "this database may not be used this way" from a failure to read it
// at all — a distinction that decides whether retrying could ever help.
var ErrRefused = errors.New("refused by the migration state")

var _ Querier = (*sql.DB)(nil)
var _ Querier = (*sql.Tx)(nil)
var _ Querier = (*sql.Conn)(nil)

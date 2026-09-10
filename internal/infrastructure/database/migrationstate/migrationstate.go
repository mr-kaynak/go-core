// Package migrationstate decides what a database's migration metadata means
// and which operations that meaning permits.
//
// It is deliberately split in two. Classification is a read-only reading of
// the database; admission is a policy over that reading. Collapsing them —
// "refuse unless everything is fine" — is what makes a system that cannot
// repair itself: baseline exists to convert a legacy database, and refusing
// legacy databases everywhere would refuse the very operation that fixes them.
package migrationstate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// LegacyHistoryTable is goose's default table name, which every database
// created before the histories were separated still carries.
const LegacyHistoryTable = "goose_db_version"

// BaselineAuditTable records a conversion from the legacy history. Its
// presence is what distinguishes a converted database from one being written
// by two different migrators.
const BaselineAuditTable = "core_migration_baseline"

// State is what a database's migration metadata says about it. Exactly one
// applies at a time; see [Classify] for the precedence between them.
type State string

const (
	// StateFresh: no history, no core objects. Nothing has run here.
	StateFresh State = "fresh"
	// StateHealthy: history matches the inventory and nothing is pending.
	StateHealthy State = "healthy"
	// StatePending: history is valid and migrations remain to be applied.
	StatePending State = "pending"
	// StateGappedHistory: the inventory is missing a version below the
	// highest applied one. goose refuses to advance such a history, so this
	// is reported as its own state rather than as "pending" — the operator
	// needs to repair it, not run a migration.
	StateGappedHistory State = "gapped-history"
	// StateUnknownVersions: the history records versions this binary does
	// not ship. Usually an older binary meeting a newer schema.
	StateUnknownVersions State = "unknown-applied-versions"
	// StateLegacy: the single pre-separation history is present and this
	// source has none of its own. Conversion is baseline's job.
	StateLegacy State = "legacy"
	// StateOrphanSchema: core objects exist with no history explaining them.
	StateOrphanSchema State = "orphan-schema"
	// StateAmbiguousProvenance: both the separated and the legacy history
	// hold applied versions with no record of a conversion, so two migrators
	// may be writing to the same database.
	StateAmbiguousProvenance State = "ambiguous-provenance"
)

// severity orders states for reporting and for choosing which one governs a
// multi-source database. Lower is worse.
var severity = map[State]int{
	StateAmbiguousProvenance: 0,
	StateOrphanSchema:        1,
	StateLegacy:              2,
	StateGappedHistory:       3,
	StateUnknownVersions:     4,
	StateFresh:               5,
	StatePending:             6,
	StateHealthy:             7,
}

// Operation is a thing a caller wants to do with the database.
type Operation string

const (
	// OpServe: start handling traffic against this schema.
	OpServe Operation = "serve"
	// OpMigrate: apply pending migrations.
	OpMigrate Operation = "migrate"
	// OpBaseline: convert a legacy history into separated ones.
	OpBaseline Operation = "baseline"
	// OpDiagnose: report the state. Never refused — reporting the state is
	// the whole job, and an operator staring at a broken database needs the
	// diagnosis most.
	OpDiagnose Operation = "diagnose"
)

// admission is the matrix in §4.4a of the phase plan. A state absent from an
// operation's set is refused for it.
var admission = map[Operation]map[State]bool{
	OpServe: {
		StateHealthy: true,
		// StatePending is admitted only under Tolerances.PendingMigrations.
	},
	OpMigrate: {
		StateFresh:   true,
		StateHealthy: true,
		StatePending: true,
	},
	OpBaseline: {
		StateLegacy: true,
	},
	OpDiagnose: {
		StateFresh: true, StateHealthy: true, StatePending: true,
		StateGappedHistory: true, StateUnknownVersions: true, StateLegacy: true,
		StateOrphanSchema: true, StateAmbiguousProvenance: true,
	},
}

// Tolerances relax classification and admission. They exist for documented
// procedures — a rollback, a rolling deploy — and each one is a decision an
// operator has to make explicitly.
type Tolerances struct {
	// UnknownAppliedVersions changes classification: versions the binary does
	// not ship stop producing StateUnknownVersions. It belongs to the
	// documented rollback procedure, where an older binary is deliberately
	// started against a newer schema.
	UnknownAppliedVersions bool
	// PendingMigrations changes admission only: serving is allowed while
	// migrations are outstanding, as during a rolling deploy.
	PendingMigrations bool
}

// SourceState is the classification of one source's history.
type SourceState struct {
	Source string
	// Table is the history this source records itself in. Carried here so a
	// caller reporting on a source cannot derive a different name than the
	// one that was actually read.
	Table   string
	State   State
	Applied []int64
	Pending []int64
	// Unknown are applied versions the binary does not ship.
	Unknown []int64
	// MissingBelowMax are inventory versions absent from the history but
	// below its highest applied version — the gap goose refuses to cross.
	MissingBelowMax []int64
}

// Report is the whole reading: every source, plus the database-wide signals
// that are not any one source's business.
type Report struct {
	Sources []SourceState
	// LegacyApplied are the positive versions in the pre-separation history.
	LegacyApplied []int64
	// CoreObjectsPresent is true when core-owned tables exist.
	CoreObjectsPresent bool
	// BaselineRecorded is true when a conversion was recorded.
	BaselineRecorded bool
}

// State is the state that governs the database as a whole: the most severe
// one across every source. A healthy core with an unmigrated module is not a
// database that may serve traffic.
func (r Report) State() State {
	governing := StateHealthy
	for i := range r.Sources {
		if severity[r.Sources[i].State] < severity[governing] {
			governing = r.Sources[i].State
		}
	}
	return governing
}

// Allows reports whether op may proceed, and explains the refusal when it may
// not. The explanation is the point: an operator meeting a refused startup
// needs to know which command fixes it.
func (r Report) Allows(op Operation, tol Tolerances) error {
	state := r.State()

	if admission[op][state] {
		return nil
	}
	if op == OpServe && state == StatePending && tol.PendingMigrations {
		return nil
	}

	return &RefusalError{Op: op, DatabaseState: state, Detail: r.explain(state, op)}
}

// RefusalError is what the admission matrix returns when an operation is not
// permitted.
//
// It is a type rather than a wrapped sentinel so the message stays the thing
// an operator reads first — "cannot serve: ..." — while errors.Is still
// identifies it, and so a caller choosing an exit code can see which
// operation and which state produced it without parsing prose.
type RefusalError struct {
	Op            Operation
	DatabaseState State
	Detail        string
}

func (e *RefusalError) Error() string {
	return fmt.Sprintf("cannot %s: %s", e.Op, e.Detail)
}

// Is reports RefusalError as [ErrRefused] so callers can match the class
// without depending on the type.
func (e *RefusalError) Is(target error) bool { return target == ErrRefused }

func (r Report) explain(state State, op Operation) string {
	switch state {
	case StateLegacy:
		return fmt.Sprintf(
			"this database still uses the single pre-separation migration history (%s, versions %s).\n"+
				"Convert it once, with the application stopped:\n"+
				"    migrate baseline            # review the plan\n"+
				"    migrate baseline --apply    # perform it\n"+
				"The legacy table is left in place as evidence and is not read again afterwards.",
			LegacyHistoryTable, formatVersions(r.LegacyApplied),
		)
	case StateOrphanSchema:
		return "core tables exist but no migration history explains them. A history was probably " +
			"dropped by hand. Restore it from a backup, or baseline the database explicitly after " +
			"establishing which version its schema actually matches."
	case StateAmbiguousProvenance:
		return fmt.Sprintf(
			"both the separated history and the legacy %s hold applied versions, and no conversion "+
				"was recorded in %s. Two migrators may be writing to this database. Stop every "+
				"application and migration job against it and establish which history is authoritative "+
				"before continuing.",
			LegacyHistoryTable, BaselineAuditTable,
		)
	case StateUnknownVersions:
		return fmt.Sprintf(
			"the history records migrations this build does not ship (%s). This build is older than "+
				"the schema. Deploy the matching version, or — if this is a deliberate rollback — set "+
				"DB_ALLOW_UNKNOWN_APPLIED_VERSIONS=true, which is part of the documented rollback "+
				"procedure and not a general setting.",
			formatSourceVersions(r.Sources, func(s SourceState) []int64 { return s.Unknown }),
		)
	case StateGappedHistory:
		return fmt.Sprintf(
			"the history is missing migrations below its highest applied version (%s), which goose "+
				"refuses to advance past. This usually means migrations were applied out of order or "+
				"a history row was deleted. Repair the history before migrating.",
			formatSourceVersions(r.Sources, func(s SourceState) []int64 { return s.MissingBelowMax }),
		)
	case StateFresh:
		if op == OpBaseline {
			return "this database has no migration history to convert."
		}
		return "no migrations have been applied to this database yet. Run them before serving traffic."
	case StatePending:
		if op == OpBaseline {
			return "this database already uses separated histories; there is nothing to convert."
		}
		return fmt.Sprintf(
			"migrations are outstanding (%s). Run them, or set DB_ALLOW_PENDING_MIGRATIONS=true if "+
				"this instance is deliberately serving an older schema during a rolling deploy.",
			formatSourceVersions(r.Sources, func(s SourceState) []int64 { return s.Pending }),
		)
	case StateHealthy:
		return "this database already uses separated histories; there is nothing to convert."
	default:
		return string(state)
	}
}

func formatVersions(versions []int64) string {
	if len(versions) == 0 {
		return "none"
	}
	sorted := append([]int64(nil), versions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	if len(sorted) > 1 && sorted[len(sorted)-1]-sorted[0] == int64(len(sorted)-1) {
		return fmt.Sprintf("%d-%d", sorted[0], sorted[len(sorted)-1])
	}
	parts := make([]string, 0, len(sorted))
	for _, v := range sorted {
		parts = append(parts, fmt.Sprint(v))
	}
	return strings.Join(parts, ", ")
}

func formatSourceVersions(sources []SourceState, pick func(SourceState) []int64) string {
	var parts []string
	for i := range sources {
		if versions := pick(sources[i]); len(versions) > 0 {
			parts = append(parts, fmt.Sprintf("%s: %s", sources[i].Source, formatVersions(versions)))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "; ")
}

// Querier is the read surface classification needs. *sql.DB, *sql.Tx and
// *sql.Conn all satisfy it, which is what lets the same code run both as a
// standalone preflight and inside the migration lock on the very connection
// the migration will use.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Inventory is what one source ships: the versions present in its filesystem.
type Inventory struct {
	Source   string
	Table    string
	Versions []int64
}

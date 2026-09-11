package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp/fingerprints"
)

// BaselinePlan is an operator's statement of which legacy versions belong to
// which source.
//
// It is required rather than inferred. The legacy history is a single stream
// of numbers with no record of who owned each one, and guessing wrong writes
// a history that says a migration ran when it did not — or that one did not
// when it did. The plan makes the operator say it, and everything below
// checks that what they said is possible.
type BaselinePlan struct {
	// Mapping assigns legacy versions to sources, by source name.
	Mapping map[string][]int64
	// Unverified names sources whose supplied files are known to be an
	// incomplete record of what ran, so their mapped versions are recorded
	// without being checked against real migrations.
	//
	// It cannot mean "no files were supplied at all": a source with no
	// filesystem cannot be registered, so such a mapping is refused as naming
	// an unknown source long before this is consulted.
	Unverified map[string]bool
	// Force proceeds despite a schema fingerprint mismatch. The differences
	// are reported and recorded either way.
	Force bool
	// AcknowledgeDataMigrations proceeds despite versions whose effect the
	// fingerprint cannot see.
	AcknowledgeDataMigrations bool
}

// BaselineOutcome is what a baseline did, or would do.
type BaselineOutcome struct {
	// Applied is false for a dry run.
	Applied bool
	// Histories is what each source's history will hold, by source name.
	Histories map[string][]int64
	// FingerprintDiffs are the differences found against the expected schema
	// for core's target version. Non-empty means --force was required.
	FingerprintDiffs []schemafp.Difference
	// UnverifiableVersions are core versions whose effect the fingerprint
	// cannot confirm, because they change no catalog state.
	UnverifiableVersions []int64
	// LegacyTable is left in place; naming it here makes that explicit.
	LegacyTable string
	// LegacyApplied is what the legacy history held. Reported so a caller
	// describing the conversion does not have to read it again — a second
	// read is a second answer, taken outside the lock the checks ran under.
	LegacyApplied []int64
	// CoreTarget is the version the schema was compared against.
	CoreTarget int64
}

var errBaselineRefused = errors.New("baseline refused")

// Baseline converts a legacy single history into separated ones.
//
// It writes only metadata: no DDL runs, no migration is applied or rolled
// back. What it changes is which history table records that the schema is
// where it already is.
//
// dryRun performs every check and reports the outcome without writing, which
// is the default the CLI exposes: an operator should be able to read what
// this will claim about their database before it claims it.
func (r *MigrationRunner) Baseline(ctx context.Context, plan BaselinePlan, dryRun bool) (*BaselineOutcome, error) {
	outcome := &BaselineOutcome{
		Applied:     false,
		Histories:   map[string][]int64{},
		LegacyTable: migrationstate.LegacyHistoryTable,
	}

	// Everything below runs inside one transaction holding the migration
	// lock. The evidence and the decision must come from the same read: a
	// fingerprint taken before the lock could describe a database that
	// changed before the history was written.
	err := withBoundedWriteLock(ctx, r.pool, r.cfg.LockWait, r.cfg.StatementTimeout,
		func(ctx context.Context, tx *sql.Tx) error {
			if err := r.baselineInTx(ctx, tx, plan, outcome); err != nil {
				return err
			}
			if dryRun {
				// Rolling back is the point: a dry run must be
				// indistinguishable from not having run.
				return errDryRun
			}
			outcome.Applied = true
			return nil
		})

	if errors.Is(err, errDryRun) {
		return outcome, nil
	}
	if err != nil {
		return outcome, err
	}
	return outcome, nil
}

// errDryRun aborts the transaction after the checks have run, so a dry run
// exercises exactly the path an apply would and then leaves nothing behind.
var errDryRun = errors.New("dry run")

func (r *MigrationRunner) baselineInTx(
	ctx context.Context, tx *sql.Tx, plan BaselinePlan, outcome *BaselineOutcome,
) error {
	report, err := migrationstate.Classify(ctx, tx, r.inventories(), r.coreName, r.cfg.Schema, r.cfg.Tolerances)
	if err != nil {
		return err
	}
	if err := report.Allows(migrationstate.OpBaseline, r.cfg.Tolerances); err != nil {
		return err
	}

	outcome.LegacyApplied = report.LegacyApplied

	if err := r.checkMappingCovers(plan, report.LegacyApplied); err != nil {
		return err
	}
	coreTarget, err := r.checkCorePrefix(plan)
	if err != nil {
		return err
	}
	outcome.CoreTarget = coreTarget
	if err := r.checkTargetsExist(plan); err != nil {
		return err
	}
	if err := r.checkResultingHistories(plan, outcome); err != nil {
		return err
	}
	if err := r.checkSchemaMatches(ctx, tx, coreTarget, plan, outcome); err != nil {
		return err
	}

	return r.writeHistories(ctx, tx, plan, coreTarget, outcome)
}

// checkMappingCovers requires the mapping to account for every applied legacy
// version exactly once.
//
// Both halves matter. An unassigned version would be dropped, so a migration
// that ran would be offered again. A version assigned twice would be recorded
// as having run in two histories, and the second is a lie.
func (r *MigrationRunner) checkMappingCovers(plan BaselinePlan, legacyApplied []int64) error {
	assigned := map[int64]string{}
	var problems []string

	for source, versions := range plan.Mapping {
		if _, known := r.sourceIndex()[source]; !known {
			problems = append(problems, fmt.Sprintf(
				"maps versions to unknown source %q; this binary registers %v", source, r.Sources()))
			continue
		}
		for _, version := range versions {
			previous, dup := assigned[version]
			switch {
			case dup && previous == source:
				problems = append(problems, fmt.Sprintf(
					"version %d is assigned to %q more than once", version, source))
			case dup:
				problems = append(problems, fmt.Sprintf(
					"version %d is assigned to both %q and %q", version, previous, source))
			default:
				assigned[version] = source
			}
		}
	}

	inLegacy := make(map[int64]struct{}, len(legacyApplied))
	for _, version := range legacyApplied {
		inLegacy[version] = struct{}{}
		if _, mapped := assigned[version]; !mapped {
			problems = append(problems, fmt.Sprintf(
				"version %d is applied in %s but is not assigned to any source",
				version, migrationstate.LegacyHistoryTable))
		}
	}
	for version, source := range assigned {
		if _, present := inLegacy[version]; !present {
			problems = append(problems, fmt.Sprintf(
				"version %d is assigned to %q but is not applied in %s",
				version, source, migrationstate.LegacyHistoryTable))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf(
			"%w: the mapping does not describe this database. "+
				"Every applied legacy version must be assigned to exactly one source:\n  - %s",
			errBaselineRefused, strings.Join(problems, "\n  - "),
		)
	}
	return nil
}

// checkCorePrefix requires core's versions to be exactly 1..N.
//
// goose refuses to advance a history that is missing a version below its
// highest applied one, so a core history of 1-2,4-16 would be complete,
// disjoint, and permanently unusable — the refusal would arrive on the next
// migration rather than here, after the metadata was written.
func (r *MigrationRunner) checkCorePrefix(plan BaselinePlan) (int64, error) {
	versions := append([]int64(nil), plan.Mapping[r.coreName]...)
	if len(versions) == 0 {
		return 0, fmt.Errorf(
			"%w: no versions are assigned to %q. A baseline that records nothing as applied would "+
				"offer core's migrations again against a schema that already has them",
			errBaselineRefused, r.coreName,
		)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	for i, version := range versions {
		if version != int64(i+1) {
			return 0, fmt.Errorf(
				"%w: versions assigned to %q must be exactly 1..N with no gaps, but %v is missing %d. "+
					"goose cannot advance a history with a gap below its highest version",
				errBaselineRefused, r.coreName, versions, int64(i+1),
			)
		}
	}
	return versions[len(versions)-1], nil
}

// checkTargetsExist requires every mapped version to correspond to a real
// migration file, for sources whose inventory was supplied.
func (r *MigrationRunner) checkTargetsExist(plan BaselinePlan) error {
	var problems []string

	for source, versions := range plan.Mapping {
		if plan.Unverified[source] {
			continue
		}
		prepared, err := r.source(source)
		if err != nil {
			return err
		}
		inventory := make(map[int64]struct{}, len(prepared.versions))
		for _, version := range prepared.versions {
			inventory[version] = struct{}{}
		}
		for _, version := range versions {
			if _, exists := inventory[version]; !exists {
				problems = append(problems, fmt.Sprintf(
					"%q has no migration numbered %d, so recording it as applied would describe a file that does not exist",
					source, version))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%w:\n  - %s", errBaselineRefused, strings.Join(problems, "\n  - "))
	}
	return nil
}

// checkResultingHistories runs the rules the migration runner will apply,
// before writing rather than after.
//
// Writing a history the next migration cannot advance is the failure mode
// this exists to prevent: it would look like a successful conversion and
// break at the next deploy.
func (r *MigrationRunner) checkResultingHistories(plan BaselinePlan, outcome *BaselineOutcome) error {
	var problems []string

	for source, versions := range plan.Mapping {
		applied := append([]int64(nil), versions...)
		sort.Slice(applied, func(i, j int) bool { return applied[i] < applied[j] })
		outcome.Histories[source] = applied

		if plan.Unverified[source] {
			continue
		}
		prepared, err := r.source(source)
		if err != nil {
			return err
		}
		if missing := missingBelowMax(applied, prepared.versions); len(missing) > 0 {
			problems = append(problems, fmt.Sprintf(
				"%q would be left missing version(s) %v below its highest applied version, which goose refuses to advance past",
				source, missing))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%w:\n  - %s", errBaselineRefused, strings.Join(problems, "\n  - "))
	}
	return nil
}

// missingBelowMax mirrors goose's pending calculation: an inventory version
// absent from the history but older than its newest applied one.
func missingBelowMax(applied, inventory []int64) []int64 {
	if len(applied) == 0 {
		return nil
	}
	appliedSet := make(map[int64]struct{}, len(applied))
	var highest int64
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

// checkSchemaMatches is the evidence step: the history is about to claim the
// schema is at a version, so the schema is compared against what that version
// is recorded to produce.
func (r *MigrationRunner) checkSchemaMatches(
	ctx context.Context, tx *sql.Tx, coreTarget int64, plan BaselinePlan, outcome *BaselineOutcome,
) error {
	expected, err := fingerprints.Expected(coreTarget)
	if err != nil {
		return err
	}
	actual, err := schemafp.ExtractTx(ctx, tx, r.cfg.Schema)
	if err != nil {
		return err
	}

	// Narrowed by ownership, not by expected identity. A consumer's own
	// tables are none of this comparison's business — requiring them absent
	// would make every real database need --force — but everything attached
	// to a core table is, including objects nobody expected. A CHECK
	// constraint added by hand to users blocks inserts and would vanish from
	// a comparison filtered to what was expected.
	relations, schemaObjects := expected.Ownership()
	diffs, err := schemafp.Compare(expected, actual.OwnedBy(relations, schemaObjects))
	if err != nil {
		return err
	}
	outcome.FingerprintDiffs = diffs

	if unverifiable, uErr := unverifiableVersions(coreTarget); uErr == nil && len(unverifiable) > 0 {
		outcome.UnverifiableVersions = unverifiable
		if !plan.AcknowledgeDataMigrations {
			return fmt.Errorf(
				"%w: version(s) %v change no catalog state, so no fingerprint can confirm they ran. "+
					"Establish that separately and re-run with --acknowledge-data-migrations",
				errBaselineRefused, unverifiable,
			)
		}
	}

	if len(diffs) == 0 {
		return nil
	}
	if !plan.Force {
		return fmt.Errorf(
			"%w: the schema does not match what version %d produces.\n%s\n"+
				"Establish why before converting. If the differences are understood and accepted, "+
				"re-run with --force; they are recorded either way",
			errBaselineRefused, coreTarget, schemafp.FormatDifferences(diffs, 20),
		)
	}
	return nil
}

// unverifiableVersions are those whose recorded delta is empty, so the
// fingerprint says nothing about whether they ran.
func unverifiableVersions(upTo int64) ([]int64, error) {
	deltas, err := fingerprints.Deltas()
	if err != nil {
		return nil, err
	}

	var unverifiable []int64
	for _, delta := range deltas {
		if delta.Version <= upTo && delta.Empty() {
			unverifiable = append(unverifiable, delta.Version)
		}
	}
	return unverifiable, nil
}

// writeHistories creates each target history and records its versions.
//
// The legacy table is left exactly as it is. It is the only remaining record
// of what the database looked like before, and a conversion that destroyed
// its own evidence would leave nothing to check against if it turned out to
// be wrong.
func (r *MigrationRunner) writeHistories(
	ctx context.Context, tx *sql.Tx, plan BaselinePlan, coreTarget int64, outcome *BaselineOutcome,
) error {
	recordedAt := time.Now().UTC()

	if err := ensureAuditTable(ctx, tx, r.cfg.Schema); err != nil {
		return err
	}

	for source, versions := range outcome.Histories {
		prepared, err := r.source(source)
		if err != nil {
			return err
		}
		if err := r.writeHistory(ctx, tx, prepared, versions); err != nil {
			return err
		}
		if err := recordAudit(ctx, tx, r.cfg.Schema, auditRow{
			Source:       source,
			Table:        prepared.table,
			Versions:     versions,
			LegacyTable:  migrationstate.Qualify(r.cfg.Schema, migrationstate.LegacyHistoryTable),
			CoreTarget:   coreTarget,
			Forced:       plan.Force,
			Unverified:   plan.Unverified[source],
			Differences:  len(outcome.FingerprintDiffs),
			Unverifiable: len(outcome.UnverifiableVersions),
			RecordedAt:   recordedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *MigrationRunner) writeHistory(
	ctx context.Context, tx *sql.Tx, source preparedSource, versions []int64,
) error {
	store, err := gooseStore(source.table)
	if err != nil {
		return err
	}

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT to_regclass($1) IS NOT NULL`, source.table).Scan(&exists); err != nil {
		return fmt.Errorf("failed to look up history table %q: %w", source.table, err)
	}
	if !exists {
		if err := store.CreateVersionTable(ctx, tx); err != nil {
			return fmt.Errorf("failed to create history table %q: %w", source.table, err)
		}
	}

	var populated bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+source.table+` WHERE version_id > 0)`).Scan(&populated); err != nil {
		return fmt.Errorf("failed to read history table %q: %w", source.table, err)
	}
	if populated {
		return fmt.Errorf(
			"%w: %q already records applied versions; this database has been converted already",
			errBaselineRefused, source.table,
		)
	}

	// goose creates the table and stamps version 0 together; creating it here
	// means stamping it here too, or the first migration meets a history with
	// no rows and refuses it.
	var sentinel bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+source.table+` WHERE version_id = 0)`).Scan(&sentinel); err != nil {
		return fmt.Errorf("failed to read history table %q: %w", source.table, err)
	}
	if !sentinel {
		if err := store.Insert(ctx, tx, gooseInsertRequest(0)); err != nil {
			return fmt.Errorf("failed to initialize history table %q: %w", source.table, err)
		}
	}

	for _, version := range versions {
		if err := store.Insert(ctx, tx, gooseInsertRequest(version)); err != nil {
			return fmt.Errorf("failed to record version %d in %q: %w", version, source.table, err)
		}
	}
	return nil
}

func (r *MigrationRunner) sourceIndex() map[string]preparedSource {
	index := make(map[string]preparedSource, len(r.sources))
	for _, source := range r.sources {
		index[source.name] = source
	}
	return index
}

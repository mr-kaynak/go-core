package migrationstate_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationsource"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/migrationstate"
)

// coreSource is the source name that carries the database-wide signals. The
// classifier and the admission matrix have to agree on it, so both test files
// take it from the same place the runner will.
const coreSource = migrationsource.CoreName

// allStates is every state a classification can produce. The matrix below
// carries a decision for each one, and TestAdmissionMatrixCoversEveryState
// fails when a state is added to the package without a decision here — the
// alternative is a new state quietly inheriting "refused" from a map miss.
var allStates = []migrationstate.State{
	migrationstate.StateFresh,
	migrationstate.StateHealthy,
	migrationstate.StatePending,
	migrationstate.StateGappedHistory,
	migrationstate.StateUnknownVersions,
	migrationstate.StateLegacy,
	migrationstate.StateOrphanSchema,
	migrationstate.StateAmbiguousProvenance,
}

var allOperations = []migrationstate.Operation{
	migrationstate.OpServe,
	migrationstate.OpMigrate,
	migrationstate.OpBaseline,
	migrationstate.OpDiagnose,
}

// admissionMatrix is §4.4a of the phase plan written out in full, with no
// tolerances set. It is spelled out state by state rather than as a list of
// permitted states because the refusals are the interesting half: baseline is
// refused everywhere except legacy precisely so that it stays the one
// operation that converts a legacy database, and diagnose is permitted
// everywhere because a diagnosis is most needed on a database that is broken.
var admissionMatrix = map[migrationstate.Operation]map[migrationstate.State]bool{
	migrationstate.OpServe: {
		migrationstate.StateFresh:               false,
		migrationstate.StateHealthy:             true,
		migrationstate.StatePending:             false, // unless Tolerances.PendingMigrations
		migrationstate.StateGappedHistory:       false,
		migrationstate.StateUnknownVersions:     false,
		migrationstate.StateLegacy:              false,
		migrationstate.StateOrphanSchema:        false,
		migrationstate.StateAmbiguousProvenance: false,
	},
	migrationstate.OpMigrate: {
		migrationstate.StateFresh:               true,
		migrationstate.StateHealthy:             true,
		migrationstate.StatePending:             true,
		migrationstate.StateGappedHistory:       false,
		migrationstate.StateUnknownVersions:     false,
		migrationstate.StateLegacy:              false,
		migrationstate.StateOrphanSchema:        false,
		migrationstate.StateAmbiguousProvenance: false,
	},
	migrationstate.OpBaseline: {
		migrationstate.StateFresh:               false,
		migrationstate.StateHealthy:             false,
		migrationstate.StatePending:             false,
		migrationstate.StateGappedHistory:       false,
		migrationstate.StateUnknownVersions:     false,
		migrationstate.StateLegacy:              true,
		migrationstate.StateOrphanSchema:        false,
		migrationstate.StateAmbiguousProvenance: false,
	},
	migrationstate.OpDiagnose: {
		migrationstate.StateFresh:               true,
		migrationstate.StateHealthy:             true,
		migrationstate.StatePending:             true,
		migrationstate.StateGappedHistory:       true,
		migrationstate.StateUnknownVersions:     true,
		migrationstate.StateLegacy:              true,
		migrationstate.StateOrphanSchema:        true,
		migrationstate.StateAmbiguousProvenance: true,
	},
}

// reportIn builds a single-source report in the given state, populated with
// the detail fields that state's explanation draws on. The classifier is
// tested against a real database elsewhere; here the report is the input.
func reportIn(state migrationstate.State) migrationstate.Report {
	source := migrationstate.SourceState{Source: coreSource, State: state}
	report := migrationstate.Report{}

	switch state {
	case migrationstate.StateLegacy:
		report.LegacyApplied = []int64{1, 2, 3}
	case migrationstate.StateAmbiguousProvenance:
		report.LegacyApplied = []int64{1, 2, 3}
		source.Applied = []int64{1, 2, 3}
	case migrationstate.StateOrphanSchema:
		report.CoreObjectsPresent = true
	case migrationstate.StateUnknownVersions:
		source.Applied = []int64{1, 2, 3, 99}
		source.Unknown = []int64{99}
	case migrationstate.StateGappedHistory:
		source.Applied = []int64{1, 3}
		source.MissingBelowMax = []int64{2}
	case migrationstate.StatePending:
		source.Applied = []int64{1}
		source.Pending = []int64{2, 3}
	case migrationstate.StateHealthy:
		source.Applied = []int64{1, 2, 3}
	case migrationstate.StateFresh:
	}

	report.Sources = []migrationstate.SourceState{source}
	return report
}

// describe renders a report for a failure message. A bare "want allowed, got
// refused" leaves the reader guessing which source and which versions drove
// the decision, which is the only thing worth knowing when this fails.
func describe(report migrationstate.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "governing=%s legacyApplied=%v coreObjects=%t baselineRecorded=%t",
		report.State(), report.LegacyApplied, report.CoreObjectsPresent, report.BaselineRecorded)
	for i := range report.Sources {
		source := report.Sources[i]
		fmt.Fprintf(&b, "\n  %s: state=%s applied=%v pending=%v unknown=%v missingBelowMax=%v",
			source.Source, source.State, source.Applied, source.Pending, source.Unknown, source.MissingBelowMax)
	}
	return b.String()
}

func TestAdmissionMatrixCoversEveryState(t *testing.T) {
	for _, op := range allOperations {
		decisions, ok := admissionMatrix[op]
		if !ok {
			t.Fatalf("operation %q has no row in the test matrix", op)
		}
		for _, state := range allStates {
			if _, decided := decisions[state]; !decided {
				t.Errorf(
					"no decision recorded for %s on a %s database; add one to admissionMatrix rather than "+
						"letting it default to refused",
					op, state,
				)
			}
		}
		if len(decisions) != len(allStates) {
			t.Errorf("%s has %d decisions for %d states — the matrix has drifted", op, len(decisions), len(allStates))
		}
	}
}

func TestAdmissionMatrixWithoutTolerances(t *testing.T) {
	for _, op := range allOperations {
		for _, state := range allStates {
			t.Run(string(op)+"/"+string(state), func(t *testing.T) {
				report := reportIn(state)
				err := report.Allows(op, migrationstate.Tolerances{})

				if want := admissionMatrix[op][state]; want && err != nil {
					t.Fatalf("%s must be allowed on a %s database, but it was refused: %v\n%s",
						op, state, err, describe(report))
				} else if !want && err == nil {
					t.Fatalf("%s must be refused on a %s database, but it was allowed\n%s",
						op, state, describe(report))
				}
			})
		}
	}
}

// A diagnostic that refuses to run on a broken database is useless exactly
// when it is needed: the operator reaching for it is already staring at a
// database that will not serve. No state and no tolerance may withhold it.
func TestDiagnoseIsNeverRefused(t *testing.T) {
	tolerances := map[string]migrationstate.Tolerances{
		"none":         {},
		"pending":      {PendingMigrations: true},
		"unknown":      {UnknownAppliedVersions: true},
		"both relaxed": {PendingMigrations: true, UnknownAppliedVersions: true},
	}

	for _, state := range allStates {
		for label, tol := range tolerances {
			t.Run(string(state)+"/"+label, func(t *testing.T) {
				report := reportIn(state)
				if err := report.Allows(migrationstate.OpDiagnose, tol); err != nil {
					t.Fatalf("diagnose was refused on a %s database with tolerances %q: %v\n%s",
						state, label, err, describe(report))
				}
			})
		}
	}
}

// Baseline exists to convert a legacy database. If the matrix refused legacy
// databases the way serve and migrate do, the only repair path would be
// closed and a legacy database could never become a healthy one.
func TestBaselineIsAdmittedOnTheOneStateItRepairs(t *testing.T) {
	legacy := reportIn(migrationstate.StateLegacy)

	if err := legacy.Allows(migrationstate.OpBaseline, migrationstate.Tolerances{}); err != nil {
		t.Fatalf("baseline must be allowed on a legacy database, otherwise nothing can convert it: %v\n%s",
			err, describe(legacy))
	}
	for _, op := range []migrationstate.Operation{migrationstate.OpServe, migrationstate.OpMigrate} {
		if err := legacy.Allows(op, migrationstate.Tolerances{}); err == nil {
			t.Errorf("%s must be refused on a legacy database until baseline has converted it\n%s",
				op, describe(legacy))
		}
	}
}

func TestServeIsAdmittedWithPendingMigrationsOnlyUnderTheTolerance(t *testing.T) {
	pending := reportIn(migrationstate.StatePending)

	if err := pending.Allows(migrationstate.OpServe, migrationstate.Tolerances{}); err == nil {
		t.Fatalf("serving with outstanding migrations must be refused by default\n%s", describe(pending))
	}
	if err := pending.Allows(migrationstate.OpServe, migrationstate.Tolerances{PendingMigrations: true}); err != nil {
		t.Fatalf("a rolling deploy sets PendingMigrations deliberately; serve must then be allowed: %v\n%s",
			err, describe(pending))
	}
}

// PendingMigrations is an admission tolerance and nothing else: it may soften
// exactly one cell of the matrix. If it leaked further — letting a gapped or
// legacy database serve, say — an operator setting it for a rolling deploy
// would silently also disable the checks that catch a broken schema.
func TestPendingMigrationsToleranceAffectsOnlyServeOnAPendingDatabase(t *testing.T) {
	for _, op := range allOperations {
		for _, state := range allStates {
			t.Run(string(op)+"/"+string(state), func(t *testing.T) {
				report := reportIn(state)
				strict := report.Allows(op, migrationstate.Tolerances{}) == nil
				relaxed := report.Allows(op, migrationstate.Tolerances{PendingMigrations: true}) == nil

				changed := strict != relaxed
				shouldChange := op == migrationstate.OpServe && state == migrationstate.StatePending
				if changed != shouldChange {
					t.Fatalf(
						"PendingMigrations changed the %s decision for a %s database from allowed=%t to allowed=%t; "+
							"it may only relax serve on a pending database\n%s",
						op, state, strict, relaxed, describe(report),
					)
				}
			})
		}
	}
}

// UnknownAppliedVersions is a classification tolerance, deliberately absent
// from admission. It stops versions this build does not ship from producing
// StateUnknownVersions in the first place (see the classifier tests); once a
// report says unknown-applied-versions, admission must still refuse. Moving
// the flag into admission would turn "this build is older than the schema"
// from a fact about the database into a decision made per operation, and the
// rollback procedure would no longer be visible in the report an operator
// reads.
func TestUnknownAppliedVersionsToleranceDoesNotAppearInAdmission(t *testing.T) {
	for _, op := range allOperations {
		for _, state := range allStates {
			t.Run(string(op)+"/"+string(state), func(t *testing.T) {
				report := reportIn(state)
				strict := report.Allows(op, migrationstate.Tolerances{}) == nil
				relaxed := report.Allows(op, migrationstate.Tolerances{UnknownAppliedVersions: true}) == nil

				if strict != relaxed {
					t.Fatalf(
						"UnknownAppliedVersions changed the %s decision for a %s database (allowed=%t -> %t); "+
							"it belongs to classification, not admission\n%s",
						op, state, strict, relaxed, describe(report),
					)
				}
			})
		}
	}
}

// The database is only as usable as its least ready source. A core schema
// that is fully migrated says nothing about a module whose tables do not
// exist yet, and serving traffic against that module's routes would fail on
// the first query.
func TestAHealthyCoreWithAFreshModuleMayNotServe(t *testing.T) {
	report := migrationstate.Report{Sources: []migrationstate.SourceState{
		{Source: coreSource, State: migrationstate.StateHealthy, Applied: []int64{1, 2, 3}},
		{Source: "orders", State: migrationstate.StateFresh, Pending: []int64{1, 2}},
	}}

	if got := report.State(); got != migrationstate.StateFresh {
		t.Fatalf("the governing state should be the module's %s, got %s\n%s",
			migrationstate.StateFresh, got, describe(report))
	}
	if err := report.Allows(migrationstate.OpServe, migrationstate.Tolerances{}); err == nil {
		t.Fatalf("serving must be refused while a module has no schema\n%s", describe(report))
	}
	if err := report.Allows(migrationstate.OpMigrate, migrationstate.Tolerances{}); err != nil {
		t.Fatalf("migrating is how the module catches up; it must be allowed: %v\n%s", err, describe(report))
	}
}

func TestReportStateIsTheMostSevereSourceState(t *testing.T) {
	cases := map[string]struct {
		states []migrationstate.State
		want   migrationstate.State
	}{
		"no sources at all": {
			// Nothing to be wrong with, so nothing may be reported as wrong.
			states: nil,
			want:   migrationstate.StateHealthy,
		},
		"all healthy": {
			states: []migrationstate.State{migrationstate.StateHealthy, migrationstate.StateHealthy},
			want:   migrationstate.StateHealthy,
		},
		"pending loses to fresh": {
			states: []migrationstate.State{migrationstate.StatePending, migrationstate.StateFresh},
			want:   migrationstate.StateFresh,
		},
		"unknown versions loses to gapped history": {
			states: []migrationstate.State{migrationstate.StateUnknownVersions, migrationstate.StateGappedHistory},
			want:   migrationstate.StateGappedHistory,
		},
		"legacy loses to orphan schema": {
			states: []migrationstate.State{migrationstate.StateLegacy, migrationstate.StateOrphanSchema},
			want:   migrationstate.StateOrphanSchema,
		},
		"ambiguous provenance outranks everything": {
			states: []migrationstate.State{
				migrationstate.StateHealthy,
				migrationstate.StateOrphanSchema,
				migrationstate.StateAmbiguousProvenance,
			},
			want: migrationstate.StateAmbiguousProvenance,
		},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			report := migrationstate.Report{}
			for i, state := range tc.states {
				report.Sources = append(report.Sources, migrationstate.SourceState{
					Source: fmt.Sprintf("source%d", i),
					State:  state,
				})
			}

			if got := report.State(); got != tc.want {
				t.Fatalf("governing state = %s, want %s\n%s", got, tc.want, describe(report))
			}
		})
	}
}

// A refusal an operator cannot act on is a refusal they will work around. Each
// of these asserts the one token that turns the message into an instruction —
// the command to run, or the variable to set — not the prose around it.
func TestRefusalsCarryTheTokenAnOperatorNeeds(t *testing.T) {
	cases := map[string]struct {
		state migrationstate.State
		op    migrationstate.Operation
		want  []string
		why   string
	}{
		"legacy names the command that converts it": {
			state: migrationstate.StateLegacy,
			op:    migrationstate.OpServe,
			want:  []string{"migrate baseline", migrationstate.LegacyHistoryTable},
			why:   "baseline is the only operation admitted on a legacy database, so the refusal has to name it",
		},
		"unknown versions names the rollback variable": {
			state: migrationstate.StateUnknownVersions,
			op:    migrationstate.OpServe,
			want:  []string{"DB_ALLOW_UNKNOWN_APPLIED_VERSIONS", coreSource + ": 99"},
			why:   "a deliberate rollback is legitimate, and the operator has to be told which switch declares it",
		},
		"pending names the rolling-deploy variable and the versions": {
			state: migrationstate.StatePending,
			op:    migrationstate.OpServe,
			want:  []string{"DB_ALLOW_PENDING_MIGRATIONS", coreSource + ": 2-3"},
			why:   "the operator has to decide between running the migrations and declaring a rolling deploy",
		},
		"ambiguous provenance says to stop the other writers": {
			state: migrationstate.StateAmbiguousProvenance,
			op:    migrationstate.OpMigrate,
			want:  []string{"Stop every application", migrationstate.BaselineAuditTable},
			why:   "the repair is operational, not a command: another migrator has to be stopped first",
		},
		"gapped history names the missing version": {
			state: migrationstate.StateGappedHistory,
			op:    migrationstate.OpMigrate,
			want:  []string{"Repair the history", coreSource + ": 2"},
			why:   "migrating cannot succeed here, so the message must point at the history rather than at a command",
		},
		"orphan schema offers the two ways out": {
			state: migrationstate.StateOrphanSchema,
			op:    migrationstate.OpServe,
			want:  []string{"backup", "baseline"},
			why:   "there is no safe automatic repair; the operator has to choose restore or an explicit baseline",
		},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			report := reportIn(tc.state)
			err := report.Allows(tc.op, migrationstate.Tolerances{})
			if err == nil {
				t.Fatalf("%s should be refused on a %s database\n%s", tc.op, tc.state, describe(report))
			}

			message := err.Error()
			if !strings.HasPrefix(message, "cannot "+string(tc.op)) {
				t.Errorf("a refusal should open by naming the refused operation, got: %s", message)
			}
			for _, token := range tc.want {
				if !strings.Contains(message, token) {
					t.Errorf("refusal is missing %q (%s); message was:\n%s", token, tc.why, message)
				}
			}
		})
	}
}

// Baseline's refusals are the one place where the same state has to say
// something different depending on the operation: telling the operator to run
// baseline when they already are would be a loop.
func TestBaselineRefusalsExplainThereIsNothingToConvert(t *testing.T) {
	for _, state := range []migrationstate.State{
		migrationstate.StateFresh,
		migrationstate.StateHealthy,
		migrationstate.StatePending,
	} {
		t.Run(string(state), func(t *testing.T) {
			report := reportIn(state)
			err := report.Allows(migrationstate.OpBaseline, migrationstate.Tolerances{})
			if err == nil {
				t.Fatalf("baseline should be refused on a %s database\n%s", state, describe(report))
			}
			if strings.Contains(err.Error(), "migrate baseline") {
				t.Fatalf("a baseline refusal must not send the operator back to baseline, got:\n%s", err)
			}
			if !strings.Contains(err.Error(), "convert") {
				t.Fatalf("the refusal should say there is nothing to convert, got:\n%s", err)
			}
		})
	}
}

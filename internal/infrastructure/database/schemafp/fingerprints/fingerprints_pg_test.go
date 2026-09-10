package fingerprints_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp"
	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp/fingerprints"
	"github.com/mr-kaynak/go-core/internal/test/pgtest"
)

const schema = "public"

// The recorded deltas are only evidence if they still describe the migrations
// they were generated from. These tests replay the migrations and compare, so
// a migration edited or added without regenerating fails here rather than in
// somebody's baseline.

func TestEveryVersionReconstructsToTheRealSchema(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := pgtest.New(t)

	for version := int64(1); version <= latest; version++ {
		pgtest.ApplyCoreMigrations(t, db.DB, version)

		actual, err := schemafp.Extract(context.Background(), db.DB, schema)
		if err != nil {
			t.Fatalf("failed to fingerprint version %d: %v", version, err)
		}
		expected, err := fingerprints.Expected(version)
		if err != nil {
			t.Fatalf("failed to reconstruct version %d: %v", version, err)
		}

		diffs, err := schemafp.Compare(expected, actual)
		if err != nil {
			t.Fatalf("failed to compare version %d: %v", version, err)
		}
		if len(diffs) != 0 {
			t.Fatalf(
				"the recorded fingerprint for version %d no longer matches what the migrations produce.\n%s\n\n"+
					"If a migration changed or was added, regenerate: go run ./cmd/schemafingerprint",
				version, schemafp.FormatDifferences(diffs, 15),
			)
		}
	}
}

// The generator is only trustworthy if running it twice produces the same
// files. A fingerprint that drifted on its own would fail the check above at
// some unrelated moment and look like a migration problem.
func TestRegeneratingProducesTheCommittedFiles(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)
	db := pgtest.New(t)

	var previous *schemafp.Snapshot
	for version := int64(1); version <= latest; version++ {
		pgtest.ApplyCoreMigrations(t, db.DB, version)

		current, err := schemafp.Extract(context.Background(), db.DB, schema)
		if err != nil {
			t.Fatalf("failed to fingerprint version %d: %v", version, err)
		}
		delta, err := schemafp.Diff(version, previous, current)
		if err != nil {
			t.Fatalf("failed to diff version %d: %v", version, err)
		}
		previous = current

		rendered, err := schemafp.Render(delta)
		if err != nil {
			t.Fatalf("failed to render version %d: %v", version, err)
		}
		committed, err := os.ReadFile(filepath.Join("data", schemafp.RecordName(version)))
		if err != nil {
			t.Fatalf("failed to read the committed fingerprint for version %d: %v", version, err)
		}

		if string(rendered) != string(committed) {
			t.Fatalf(
				"the committed fingerprint for version %d differs from a fresh recording.\n"+
					"Regenerate with: go run ./cmd/schemafingerprint",
				version,
			)
		}
	}
}

// A fingerprint that records no catalog change cannot attest to anything.
// That is legitimate for a data-only migration, but it is a real limit on
// what baseline can establish, so it is surfaced rather than left implicit.
func TestVersionsThatCannotBeVerifiedAreNamed(t *testing.T) {
	deltas, err := fingerprints.Deltas()
	if err != nil {
		t.Fatalf("failed to read the recorded deltas: %v", err)
	}
	if len(deltas) == 0 {
		t.Fatal("no fingerprints are recorded; baseline would have nothing to verify against")
	}

	for _, delta := range deltas {
		if delta.Empty() {
			t.Logf(
				"version %d changes no catalog state, so a fingerprint cannot confirm it was applied",
				delta.Version,
			)
		}
	}
}

func TestExpectedRefusesAVersionItDoesNotHave(t *testing.T) {
	latest := pgtest.LatestCoreVersion(t)

	_, err := fingerprints.Expected(latest + 1)
	if err == nil {
		t.Fatal("reconstructing an unrecorded version must fail rather than return a partial schema")
	}
}

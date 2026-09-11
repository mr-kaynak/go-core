// Package fingerprints carries the recorded catalog effect of every core
// migration.
//
// Baseline needs to answer a question the migration history cannot: a history
// says version N was applied, never what N did, so converting a database onto
// a separated history has to establish independently that its schema really
// is at N. These files are that evidence — the catalog state a database
// claiming each version is supposed to have.
//
// They are generated, not written by hand: `go run ./cmd/schemafingerprint`
// against a PostgreSQL server. A test regenerates them in memory and compares,
// so they cannot drift from the migrations they describe.
package fingerprints

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"

	"github.com/mr-kaynak/go-core/internal/infrastructure/database/schemafp"
)

//go:embed data/*.json
var files embed.FS

// Deltas returns every recorded delta, ordered by version.
func Deltas() ([]*schemafp.Delta, error) {
	entries, err := fs.Glob(files, "data/*.json")
	if err != nil {
		return nil, fmt.Errorf("fingerprints: failed to list recorded deltas: %w", err)
	}

	deltas := make([]*schemafp.Delta, 0, len(entries))
	for _, name := range entries {
		content, readErr := files.ReadFile(name)
		if readErr != nil {
			return nil, fmt.Errorf("fingerprints: failed to read %s: %w", name, readErr)
		}
		var delta schemafp.Delta
		if err := json.Unmarshal(content, &delta); err != nil {
			return nil, fmt.Errorf("fingerprints: failed to parse %s: %w", name, err)
		}
		if delta.Version <= 0 {
			return nil, fmt.Errorf("fingerprints: %s records version %d", name, delta.Version)
		}
		deltas = append(deltas, &delta)
	}

	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Version < deltas[j].Version })
	return deltas, nil
}

// Expected is the catalog state a database at the given version should have.
//
// It reconstructs from the deltas rather than storing a snapshot per version,
// so the recorded evidence stays the size of one snapshot instead of one per
// migration.
func Expected(version int64) (*schemafp.Snapshot, error) {
	deltas, err := Deltas()
	if err != nil {
		return nil, err
	}
	if len(deltas) == 0 {
		return nil, fmt.Errorf(
			"fingerprints: none are recorded, so no version can be verified. " +
				"Generate them with: go run ./cmd/schemafingerprint")
	}

	highest := deltas[len(deltas)-1].Version
	if version > highest {
		return nil, fmt.Errorf(
			"fingerprints: version %d is not recorded (the highest is %d). "+
				"If a migration was added, regenerate with: go run ./cmd/schemafingerprint",
			version, highest,
		)
	}
	return schemafp.Reconstruct(deltas, version)
}

package schemafp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// RecordDir is where recorded deltas live, relative to the repository root.
const RecordDir = "internal/infrastructure/database/schemafp/fingerprints/data"

// RecordName is the file one version's delta is stored in.
func RecordName(version int64) string {
	return fmt.Sprintf("v%05d.json", version)
}

// Render writes a delta in the committed form: pretty-printed and newline
// terminated, so a change to it reads as a diff rather than as one long line.
func Render(delta *Delta) ([]byte, error) {
	var b strings.Builder
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	// Catalog definitions contain <, > and & often enough that escaping them
	// would make the committed files unreadable.
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(delta); err != nil {
		return nil, fmt.Errorf("schemafp: failed to render version %d: %w", delta.Version, err)
	}
	return []byte(b.String()), nil
}

// Delta is what one migration did to the catalog.
//
// Fingerprints are stored per migration rather than as a snapshot per
// version. Sixteen cumulative snapshots of this schema would be well over a
// megabyte of near-identical JSON; the deltas together are the size of one,
// because an object is written when it appears and again only when it
// changes.
//
// The shape earns its keep twice. It reconstructs any version exactly, and it
// is also the most direct statement of what a migration did — a reviewer
// reading a delta sees the constraint that was added or the index that was
// replaced, without inferring it from SQL.
type Delta struct {
	Version     int64       `json:"version"`
	Environment Environment `json:"environment"`
	// Added are objects this version introduced.
	Added []Object `json:"added,omitempty"`
	// Changed are objects whose recorded state this version altered.
	Changed []Change `json:"changed,omitempty"`
	// Removed are the keys of objects this version dropped.
	Removed []string `json:"removed,omitempty"`
}

// Change records one object's state moving from one value to another.
type Change struct {
	Kind     Kind   `json:"kind"`
	Identity string `json:"identity"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// Diff computes what changed between two snapshots, in the order a migration
// would have caused it.
func Diff(version int64, before, after *Snapshot) (*Delta, error) {
	if before != nil {
		if err := before.Environment.compatibleWith(after.Environment); err != nil {
			return nil, err
		}
	}

	previous := make(map[string]Object)
	if before != nil {
		for _, o := range before.Objects {
			previous[o.Key()] = o
		}
	}
	current := make(map[string]Object, len(after.Objects))
	for _, o := range after.Objects {
		current[o.Key()] = o
	}

	delta := &Delta{Version: version, Environment: after.Environment}

	for _, o := range after.Objects {
		was, existed := previous[o.Key()]
		switch {
		case !existed:
			delta.Added = append(delta.Added, o)
		case was.State != o.State:
			delta.Changed = append(delta.Changed, Change{
				Kind: o.Kind, Identity: o.Identity, From: was.State, To: o.State,
			})
		}
	}
	for key := range previous {
		if _, still := current[key]; !still {
			delta.Removed = append(delta.Removed, key)
		}
	}

	sortObjects(delta.Added)
	sort.Slice(delta.Changed, func(i, j int) bool {
		if delta.Changed[i].Kind != delta.Changed[j].Kind {
			return delta.Changed[i].Kind < delta.Changed[j].Kind
		}
		return delta.Changed[i].Identity < delta.Changed[j].Identity
	})
	sort.Strings(delta.Removed)

	return delta, nil
}

// Reconstruct replays deltas 1..N into the snapshot that version N produces.
//
// This is what baseline compares a live database against: the catalog state a
// database claiming version N is supposed to have.
func Reconstruct(deltas []*Delta, upTo int64) (*Snapshot, error) {
	ordered := append([]*Delta(nil), deltas...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })

	objects := make(map[string]Object)
	var environment Environment
	var applied int64

	for _, delta := range ordered {
		if delta.Version > upTo {
			break
		}
		if applied != 0 && delta.Version != applied+1 {
			return nil, fmt.Errorf(
				"schemafp: fingerprints jump from version %d to %d; the series must be contiguous "+
					"or a reconstruction silently describes a schema that never existed",
				applied, delta.Version,
			)
		}
		if applied == 0 {
			environment = delta.Environment
		} else if err := environment.compatibleWith(delta.Environment); err != nil {
			return nil, err
		}

		for _, o := range delta.Added {
			if _, exists := objects[o.Key()]; exists {
				return nil, fmt.Errorf(
					"schemafp: version %d adds %s %s, which already exists",
					delta.Version, o.Kind, o.Identity,
				)
			}
			objects[o.Key()] = o
		}
		for _, change := range delta.Changed {
			key := Object{Kind: change.Kind, Identity: change.Identity}.Key()
			existing, exists := objects[key]
			if !exists {
				return nil, fmt.Errorf(
					"schemafp: version %d changes %s %s, which does not exist yet",
					delta.Version, change.Kind, change.Identity,
				)
			}
			if existing.State != change.From {
				return nil, fmt.Errorf(
					"schemafp: version %d expects %s %s to be %q, but it is %q — the series is inconsistent",
					delta.Version, change.Kind, change.Identity, change.From, existing.State,
				)
			}
			existing.State = change.To
			objects[key] = existing
		}
		for _, key := range delta.Removed {
			if _, exists := objects[key]; !exists {
				return nil, fmt.Errorf(
					"schemafp: version %d removes %q, which does not exist", delta.Version, key,
				)
			}
			delete(objects, key)
		}

		applied = delta.Version
	}

	if applied < upTo {
		return nil, fmt.Errorf(
			"schemafp: fingerprints reach version %d but version %d was requested", applied, upTo,
		)
	}

	snapshot := &Snapshot{Environment: environment, Objects: make([]Object, 0, len(objects))}
	for _, o := range objects {
		snapshot.Objects = append(snapshot.Objects, o)
	}
	sortObjects(snapshot.Objects)
	return snapshot, nil
}

// Empty reports whether the delta records nothing at all.
//
// A migration with an empty delta changed no catalog state. That is not
// necessarily wrong — a data-only migration is legitimate — but it does mean
// the fingerprint cannot attest to it, which is a limit worth being able to
// name.
func (d *Delta) Empty() bool {
	return len(d.Added) == 0 && len(d.Changed) == 0 && len(d.Removed) == 0
}

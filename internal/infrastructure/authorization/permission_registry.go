package authorization

import (
	"fmt"
	"sync"
)

// PermissionDef declares a permission: a unique name mapped to one or more
// Casbin objects (path patterns, e.g. "/api/v1/orders/*") and one action.
// Objects is plural because keyMatch2 patterns like "/api/v1/orders/*" do NOT
// match the collection path "/api/v1/orders"; a single permission typically
// carries both the collection and the item pattern.
type PermissionDef struct {
	Name    string
	Objects []string
	Action  Action
}

// validActions is the closed set of actions a permission may use.
var validActions = map[Action]struct{}{
	ActionCreate: {},
	ActionRead:   {},
	ActionUpdate: {},
	ActionDelete: {},
	ActionList:   {},
	ActionManage: {},
	ActionExport: {},
	ActionImport: {},
}

type objectActionPair struct {
	object string
	action Action
}

// PermissionRegistry is the instance-scoped source of permission definitions.
// Each application constructs its own registry (seeded with core permissions)
// so parallel applications and tests never observe each other's registrations.
// Every (object, action) pair is owned by exactly one permission name:
// removing one permission must never silently revoke another.
type PermissionRegistry struct {
	mu     sync.RWMutex
	byName map[string]PermissionDef
	pairs  map[objectActionPair]string // pair -> owning permission name
}

// NewPermissionRegistry returns a registry pre-seeded with the core
// permission set from permissionToCasbin.
func NewPermissionRegistry() *PermissionRegistry {
	r := &PermissionRegistry{
		byName: make(map[string]PermissionDef),
		pairs:  make(map[objectActionPair]string),
	}
	for name, m := range permissionToCasbin {
		def := PermissionDef{
			Name:    name,
			Objects: []string{string(m.Resource)},
			Action:  m.Action,
		}
		// Core seed is maintained with unique pairs (enforced by the
		// registry tests); commit directly.
		r.commit(def)
	}
	return r
}

// Register validates and adds the given permission definitions as one atomic
// batch: if any definition is invalid or conflicts with the registry (or with
// another definition in the batch), nothing is added.
func (r *PermissionRegistry) Register(defs ...PermissionDef) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate the whole batch against the registry and against itself
	// before committing anything.
	batchNames := make(map[string]struct{}, len(defs))
	batchPairs := make(map[objectActionPair]string, len(defs))

	for _, def := range defs {
		if def.Name == "" {
			return fmt.Errorf("permission with empty name (objects %v)", def.Objects)
		}
		if _, exists := r.byName[def.Name]; exists {
			return fmt.Errorf("permission %q already registered", def.Name)
		}
		if _, dup := batchNames[def.Name]; dup {
			return fmt.Errorf("permission %q appears twice in one batch", def.Name)
		}
		if len(def.Objects) == 0 {
			return fmt.Errorf("permission %q declares no objects", def.Name)
		}
		if def.Action == "" {
			return fmt.Errorf("permission %q has empty action", def.Name)
		}
		if _, ok := validActions[def.Action]; !ok {
			return fmt.Errorf("permission %q uses unknown action %q", def.Name, def.Action)
		}
		seen := make(map[string]struct{}, len(def.Objects))
		for _, obj := range def.Objects {
			if obj == "" {
				return fmt.Errorf("permission %q contains an empty object", def.Name)
			}
			if _, dup := seen[obj]; dup {
				return fmt.Errorf("permission %q lists object %q twice", def.Name, obj)
			}
			seen[obj] = struct{}{}

			pair := objectActionPair{object: obj, action: def.Action}
			if owner, taken := r.pairs[pair]; taken {
				return fmt.Errorf("permission %q: (object %q, action %q) already owned by %q", def.Name, obj, def.Action, owner)
			}
			if owner, taken := batchPairs[pair]; taken {
				return fmt.Errorf("permission %q: (object %q, action %q) already claimed by %q in this batch", def.Name, obj, def.Action, owner)
			}
			batchPairs[pair] = def.Name
		}
		batchNames[def.Name] = struct{}{}
	}

	for _, def := range defs {
		r.commit(def)
	}
	return nil
}

// commit adds a validated definition. Callers hold r.mu (or have exclusive
// access during construction).
func (r *PermissionRegistry) commit(def PermissionDef) {
	objects := make([]string, len(def.Objects))
	copy(objects, def.Objects)
	def.Objects = objects
	r.byName[def.Name] = def
	for _, obj := range def.Objects {
		r.pairs[objectActionPair{object: obj, action: def.Action}] = def.Name
	}
}

// Lookup returns the definition for a permission name. The returned Objects
// slice is a defensive copy — callers cannot corrupt the registry through it.
func (r *PermissionRegistry) Lookup(name string) (PermissionDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.byName[name]
	if !ok {
		return PermissionDef{}, false
	}
	return copyDef(def), true
}

// All returns defensive copies of every registered definition.
func (r *PermissionRegistry) All() []PermissionDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PermissionDef, 0, len(r.byName))
	for _, def := range r.byName {
		out = append(out, copyDef(def))
	}
	return out
}

func copyDef(def PermissionDef) PermissionDef {
	objects := make([]string, len(def.Objects))
	copy(objects, def.Objects)
	def.Objects = objects
	return def
}

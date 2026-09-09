package authorization

import (
	"fmt"

	"github.com/mr-kaynak/go-core/internal/core/errors"
)

// PolicyStore is the minimal policy mutation/listing surface the resync and
// permission-sync paths need. *CasbinService implements it; tests inject fakes.
type PolicyStore interface {
	AddPolicy(subject, domain, object string, action Action, effect string) error
	RemovePolicy(subject, domain, object string, action Action, effect string) error
	ListPolicies() ([][]string, error)
}

// RoleAssignment is one role→permission grant row from the database — the
// source of truth the managed policy subset is derived from.
type RoleAssignment struct {
	RoleName       string
	PermissionName string
}

// reservedDefaultPolicies are the code-seeded policies that have NO database
// representation. Seeding (initializeDefaultPolicies) and resync exclusion
// share this single constant so the two can never diverge: some of these
// tuples (api_client) use (object, action) pairs that ALSO appear in the
// permission registry, so a registry-derived predicate alone would wrongly
// classify them as managed and resync would delete them.
type policyTuple struct {
	Subject string
	Domain  string
	Object  string
	Action  Action
}

var reservedDefaultPolicies = []policyTuple{
	{"role:guest", DomainDefault, string(ResourceHealth), ActionRead},
	{"role:guest", DomainDefault, string(ResourceAuth), ActionCreate},
	{"role:api_client", DomainDefault, string(ResourceNotification), ActionCreate},
	{"role:api_client", DomainDefault, string(ResourceTemplate), ActionRead},
	{"role:user", DomainDefault, string(ResourceUserSelf), ActionRead},
	{"role:user", DomainDefault, string(ResourceUserSelf), ActionUpdate},
	{"role:user", DomainDefault, string(ResourceUserProfile), ActionRead},
	{"role:user", DomainDefault, string(ResourceUserProfile), ActionUpdate},
	{"role:system_admin", DomainDefault, "*", ActionManage},
}

// IsReservedDefaultPolicy reports whether the tuple is one of the code-seeded
// defaults that resync must never touch.
func IsReservedDefaultPolicy(subject, domain, object string, action Action) bool {
	for _, p := range reservedDefaultPolicies {
		if p.Subject == subject && p.Domain == domain && p.Object == object && p.Action == action {
			return true
		}
	}
	return false
}

// isAlreadyApplied reports whether err means the mutation was a no-op because
// the store already reflects the desired state (idempotent success): AddPolicy
// returns Conflict for an existing policy, RemovePolicy NotFound for an
// absent one.
func isAlreadyApplied(err error) bool {
	if err == nil {
		return true
	}
	if pd := errors.GetProblemDetail(err); pd != nil {
		return pd.Code == errors.CodeConflict || pd.Code == errors.CodeNotFound
	}
	return false
}

// ResyncManagedPolicies reconciles the MANAGED policy subset against the
// database's role-permission assignments (the source of truth).
//
// Managed subset membership (decidable): subject has the "role:" prefix,
// domain is DomainDefault, effect is "allow", the (object, action) pair is
// derived from a registry permission mapping, AND the tuple is not a reserved
// default. Membership does not depend on the role still existing in the DB —
// policies of deleted roles stay managed and are removed as stale.
//
// The reconciliation is two-way within the subset: missing desired policies
// are added, present-but-undesired ones are removed. Policies outside the
// subset (reserved defaults, non-role subjects, unmapped objects) are never
// touched. Already-present adds and already-absent removes count as success.
func ResyncManagedPolicies(store PolicyStore, registry *PermissionRegistry, assignments []RoleAssignment) (added, removed int, err error) {
	desired := desiredPolicySet(registry, assignments)

	present, err := currentAllowPolicies(store)
	if err != nil {
		return 0, 0, err
	}

	inSubset := managedSubsetPredicate(registry)

	// Remove stale managed policies.
	for t := range present {
		if !inSubset(t) {
			continue
		}
		if _, want := desired[t]; want {
			continue
		}
		rmErr := store.RemovePolicy(t.Subject, t.Domain, t.Object, t.Action, "allow")
		if !isAlreadyApplied(rmErr) {
			return added, removed, fmt.Errorf("resync: failed to remove stale policy (%s, %s, %s): %w",
				t.Subject, t.Object, t.Action, rmErr)
		}
		removed++
	}

	// Add missing desired policies.
	for t := range desired {
		if _, ok := present[t]; ok {
			continue
		}
		addErr := store.AddPolicy(t.Subject, t.Domain, t.Object, t.Action, "allow")
		if !isAlreadyApplied(addErr) {
			return added, removed, fmt.Errorf("resync: failed to add policy (%s, %s, %s): %w",
				t.Subject, t.Object, t.Action, addErr)
		}
		added++
	}

	return added, removed, nil
}

// desiredPolicySet expands every (role, permission) DB row through the
// registry into the full set of policy tuples the store should hold.
// Assignments referencing unknown permissions derive nothing (app-level rows).
func desiredPolicySet(registry *PermissionRegistry, assignments []RoleAssignment) map[policyTuple]struct{} {
	desired := make(map[policyTuple]struct{})
	for _, a := range assignments {
		def, ok := registry.Lookup(a.PermissionName)
		if !ok {
			continue
		}
		for _, obj := range def.Objects {
			desired[policyTuple{
				Subject: "role:" + a.RoleName,
				Domain:  DomainDefault,
				Object:  obj,
				Action:  def.Action,
			}] = struct{}{}
		}
	}
	return desired
}

// managedSubsetPredicate returns the decidable membership test for the policy
// subset resync owns (see ResyncManagedPolicies doc).
func managedSubsetPredicate(registry *PermissionRegistry) func(policyTuple) bool {
	managedPair := make(map[objectActionPair]struct{})
	for _, def := range registry.All() {
		for _, obj := range def.Objects {
			managedPair[objectActionPair{object: obj, action: def.Action}] = struct{}{}
		}
	}
	return func(t policyTuple) bool {
		if len(t.Subject) < 5 || t.Subject[:5] != "role:" {
			return false
		}
		if t.Domain != DomainDefault {
			return false
		}
		if _, ok := managedPair[objectActionPair{object: t.Object, action: t.Action}]; !ok {
			return false
		}
		return !IsReservedDefaultPolicy(t.Subject, t.Domain, t.Object, t.Action)
	}
}

// currentAllowPolicies snapshots the store's allow policies as tuples.
func currentAllowPolicies(store PolicyStore) (map[policyTuple]struct{}, error) {
	current, err := store.ListPolicies()
	if err != nil {
		return nil, fmt.Errorf("resync: failed to list policies: %w", err)
	}
	present := make(map[policyTuple]struct{}, len(current))
	for _, p := range current {
		if len(p) < 5 || p[4] != "allow" {
			continue
		}
		present[policyTuple{Subject: p[0], Domain: p[1], Object: p[2], Action: Action(p[3])}] = struct{}{}
	}
	return present, nil
}

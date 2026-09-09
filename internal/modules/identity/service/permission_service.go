package service

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// PermissionService handles permission-related business logic.
//
// Permission grant/revoke and resync follow the managed-policy contract:
// the database is the source of truth, Casbin is derived state that is kept
// in sync synchronously (failures surface as API errors) and can always be
// reconciled via ResyncPolicies. Mutations and resync are serialized by an
// instance-local mutex; guarantees apply to THIS instance's enforcer only —
// multi-instance permission mutation against a shared DB is unsupported in
// this phase (single serving instance deployment).
type PermissionService struct {
	permRepo repository.PermissionRepository
	roleRepo repository.RoleRepository
	store    authorization.PolicyStore
	registry *authorization.PermissionRegistry
	logger   *logger.Logger

	// syncMu serializes grant, revoke and resync so a resync can never
	// interleave with a mutation and resurrect just-revoked policies.
	syncMu sync.Mutex
}

// NewPermissionService creates a new PermissionService.
func NewPermissionService(
	permRepo repository.PermissionRepository,
	roleRepo repository.RoleRepository,
	casbinService *authorization.CasbinService,
	registry *authorization.PermissionRegistry,
) *PermissionService {
	var store authorization.PolicyStore
	if casbinService != nil {
		store = casbinService
	}
	return newPermissionServiceWithStore(permRepo, roleRepo, store, registry)
}

// newPermissionServiceWithStore is the injection seam used by tests to supply
// a fake policy store.
func newPermissionServiceWithStore(
	permRepo repository.PermissionRepository,
	roleRepo repository.RoleRepository,
	store authorization.PolicyStore,
	registry *authorization.PermissionRegistry,
) *PermissionService {
	return &PermissionService{
		permRepo: permRepo,
		roleRepo: roleRepo,
		store:    store,
		registry: registry,
		logger:   logger.Get().WithFields(logger.Fields{"service": "permission"}),
	}
}

// ListPermissions returns a paginated list of permissions, optionally filtered by category.
func (s *PermissionService) ListPermissions(
	ctx context.Context, category string, offset, limit int,
) ([]domain.Permission, int64, error) {
	if category != "" {
		perms, count, err := s.permRepo.GetByCategoryPaginated(ctx, category, offset, limit)
		if err != nil {
			s.logger.Error("Failed to fetch permissions by category", "category", category, "error", err)
			return nil, 0, errors.NewInternalError("Failed to fetch permissions")
		}
		return perms, count, nil
	}

	perms, err := s.permRepo.GetAll(ctx, offset, limit)
	if err != nil {
		s.logger.Error("Failed to fetch permissions", "error", err)
		return nil, 0, errors.NewInternalError("Failed to fetch permissions")
	}
	count, err := s.permRepo.Count(ctx)
	if err != nil {
		s.logger.Error("Failed to count permissions", "error", err)
		return nil, 0, errors.NewInternalError("Failed to fetch permissions count")
	}
	return perms, count, nil
}

// GetPermission returns a single permission by ID.
func (s *PermissionService) GetPermission(ctx context.Context, id uuid.UUID) (*domain.Permission, error) {
	return s.permRepo.GetByID(ctx, id)
}

// CreatePermission creates a new permission after checking for duplicates.
func (s *PermissionService) CreatePermission(ctx context.Context, name, description, category string) (*domain.Permission, error) {
	existing, err := s.permRepo.GetByName(ctx, name)
	if err != nil {
		pd := errors.GetProblemDetail(err)
		if pd == nil || pd.Code != errors.CodeNotFound {
			s.logger.Error("Failed to check existing permission", "name", name, "error", err)
			return nil, errors.NewInternalError("Failed to create permission")
		}
	}
	if existing != nil {
		return nil, errors.NewConflict("Permission with name '" + name + "' already exists")
	}

	perm := &domain.Permission{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		Category:    category,
	}
	if err := s.permRepo.Create(ctx, perm); err != nil {
		s.logger.Error("Failed to create permission", "name", name, "error", err)
		return nil, errors.NewInternalError("Failed to create permission")
	}
	return perm, nil
}

// UpdatePermission applies partial updates to an existing permission.
func (s *PermissionService) UpdatePermission(
	ctx context.Context, id uuid.UUID, name, description, category string,
) (*domain.Permission, error) {
	perm, err := s.permRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if name != "" {
		perm.Name = name
	}
	if description != "" {
		perm.Description = description
	}
	if category != "" {
		perm.Category = category
	}

	if err := s.permRepo.Update(ctx, perm); err != nil {
		s.logger.Error("Failed to update permission", "id", id, "error", err)
		return nil, errors.NewInternalError("Failed to update permission")
	}
	return perm, nil
}

// DeletePermission removes a permission by ID.
func (s *PermissionService) DeletePermission(ctx context.Context, id uuid.UUID) error {
	if err := s.permRepo.Delete(ctx, id); err != nil {
		s.logger.Error("Failed to delete permission", "id", id, "error", err)
		return errors.NewInternalError("Failed to delete permission")
	}
	return nil
}

// GetRolePermissions returns all permissions assigned to a role.
func (s *PermissionService) GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]domain.Permission, error) {
	perms, err := s.permRepo.GetRolePermissions(ctx, roleID)
	if err != nil {
		s.logger.Error("Failed to fetch role permissions", "role_id", roleID, "error", err)
		return nil, errors.NewInternalError("Failed to fetch role permissions")
	}
	return perms, nil
}

// AddPermissionToRole assigns a permission to a role and synchronously syncs
// Casbin. Ordering (grant = DB first, then Casbin): the DB row is the durable
// intent; if any Casbin write then fails, already-written policies are rolled
// back best-effort and an error is returned — the committed DB intent is
// repaired by a retry (idempotent) or by ResyncPolicies. There is no
// over-privilege window: no authority exists before its policy is written.
func (s *PermissionService) AddPermissionToRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	perm, err := s.permRepo.GetByID(ctx, permissionID)
	if err != nil {
		return errors.NewNotFound("Permission", permissionID.String())
	}
	permName := ""
	if perm != nil {
		permName = perm.Name
	}

	if err := s.permRepo.AddPermissionToRole(ctx, roleID, permissionID); err != nil {
		s.logger.Error("Failed to add permission to role", "role_id", roleID, "permission_id", permissionID, "error", err)
		return errors.NewInternalError("Failed to add permission to role")
	}

	tuples, subject, ok, err := s.policyTuples(ctx, roleID, permName)
	if err != nil {
		return err
	}
	if !ok {
		// Permission has no registry mapping (app-level permission without
		// route enforcement); nothing to sync.
		return nil
	}

	// Real failure rolls back what this call added, best-effort; the DB
	// grant remains as recorded intent (repaired by retry or resync).
	return s.applyPolicySet(subject, tuples, "grant", s.store.AddPolicy, s.store.RemovePolicy)
}

// RemovePermissionFromRole removes a permission from a role, fail-closed:
// Casbin policies are removed FIRST (already-absent counts as success) so
// enforcement is never more permissive than the reported outcome; the DB row
// is removed after. A partial Casbin failure restores removed policies
// best-effort and returns an error; a DB failure after Casbin removal returns
// an error (enforcement is already restricted — the caller saw the failure,
// and DB intent still records the grant until a successful retry).
func (s *PermissionService) RemovePermissionFromRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	perm, err := s.permRepo.GetByID(ctx, permissionID)
	if err != nil {
		return errors.NewNotFound("Permission", permissionID.String())
	}
	permName := ""
	if perm != nil {
		permName = perm.Name
	}

	tuples, subject, ok, err := s.policyTuples(ctx, roleID, permName)
	if err != nil {
		return err
	}
	if ok {
		if err := s.applyPolicySet(subject, tuples, "revocation", s.store.RemovePolicy, s.store.AddPolicy); err != nil {
			return err
		}
	}

	if err := s.permRepo.RemovePermissionFromRole(ctx, roleID, permissionID); err != nil {
		s.logger.Error("Failed to remove permission from role", "role_id", roleID, "permission_id", permissionID, "error", err)
		return errors.NewInternalError("Failed to remove permission from role")
	}
	return nil
}

// ResyncPolicies reconciles the managed Casbin policy subset against the
// database's role-permission assignments. It shares the mutation mutex so it
// can never interleave with a grant/revoke. Safe no-op without a policy store.
func (s *PermissionService) ResyncPolicies(ctx context.Context) (added, removed int, err error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if s.store == nil || s.registry == nil {
		s.logger.Warn("Policy resync skipped: no policy store or registry configured")
		return 0, 0, nil
	}

	assignments, err := s.permRepo.GetAllRoleAssignments(ctx)
	if err != nil {
		s.logger.Error("Policy resync: failed to load role assignments", "error", err)
		return 0, 0, errors.NewInternalError("Failed to load role assignments")
	}

	added, removed, err = authorization.ResyncManagedPolicies(s.store, s.registry, assignments)
	if err != nil {
		s.logger.Error("Policy resync failed", "error", err)
		return added, removed, errors.NewInternalError("Policy resync failed")
	}
	s.logger.Info("Policy resync completed", "added", added, "removed", removed)
	return added, removed, nil
}

// policyTuples resolves the registry definition and Casbin subject for a
// permission/role pair. ok=false means the permission has no registry mapping
// or no policy store is configured (nothing to sync).
func (s *PermissionService) policyTuples(
	ctx context.Context, roleID uuid.UUID, permName string,
) (authorization.PermissionDef, string, bool, error) {
	if s.store == nil || s.registry == nil || s.roleRepo == nil {
		return authorization.PermissionDef{}, "", false, nil
	}
	def, ok := s.registry.Lookup(permName)
	if !ok {
		s.logger.Warn("No registry mapping for permission; skipping Casbin sync", "permission", permName)
		return authorization.PermissionDef{}, "", false, nil
	}
	role, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		s.logger.Error("Casbin sync: failed to fetch role", "role_id", roleID, "error", err)
		return authorization.PermissionDef{}, "", false, errors.NewInternalError("Failed to resolve role for permission sync")
	}
	return def, "role:" + role.Name, true, nil
}

// policyMutation is the shape shared by PolicyStore.AddPolicy/RemovePolicy.
type policyMutation func(subject, domain, object string, action authorization.Action, effect string) error

// applyPolicySet applies a mutation to every object of a permission. On the
// first real failure it undoes the objects THIS CALL actually changed
// (best-effort) and returns an error. Already-applied states
// (Conflict/NotFound) count as idempotent success but are NOT rolled back —
// they reflect pre-existing state this call did not create, and reversing
// them would corrupt it (a failed grant must never delete a policy that
// existed before the call).
func (s *PermissionService) applyPolicySet(
	subject string,
	def authorization.PermissionDef,
	opName string,
	apply, undo policyMutation,
) error {
	var done []string
	for _, obj := range def.Objects {
		opErr := apply(subject, authorization.DomainDefault, obj, def.Action, "allow")
		if opErr == nil {
			done = append(done, obj)
			continue
		}
		if isPolicyNoop(opErr) {
			// Pre-existing state: success for this operation, but nothing to
			// compensate — deliberately NOT added to done.
			continue
		}
		for _, d := range done {
			rbErr := undo(subject, authorization.DomainDefault, d, def.Action, "allow")
			if rbErr != nil && !isPolicyNoop(rbErr) {
				s.logger.Error("Policy rollback failed; state repaired on next retry/resync",
					"op", opName, "role_subject", subject, "object", d, "error", rbErr)
			}
		}
		s.logger.Error("Casbin sync failed", "op", opName, "role_subject", subject, "object", obj, "error", opErr)
		return errors.NewInternalError("Failed to synchronize permission " + opName)
	}
	return nil
}

// isPolicyNoop reports whether a policy mutation error means the store already
// reflects the desired state (add: Conflict, remove: NotFound) — idempotent
// success for sync purposes.
func isPolicyNoop(err error) bool {
	if err == nil {
		return true
	}
	if pd := errors.GetProblemDetail(err); pd != nil {
		return pd.Code == errors.CodeConflict || pd.Code == errors.CodeNotFound
	}
	return false
}

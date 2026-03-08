package authorization

import (
	"fmt"
	"strings"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"gorm.io/gorm"
)

// Action represents the type of action
type Action string

// Standard CRUD actions
const (
	ActionCreate Action = "create"
	ActionRead   Action = "read"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
	ActionList   Action = "list"
	ActionManage Action = "manage" // Full control
)

// Resource represents API resources
type Resource string

// API Resources
const (
	ResourceUser         Resource = "/api/v1/users/*"
	ResourceUserProfile  Resource = "/api/v1/users/profile"
	ResourceUserSelf     Resource = "/api/v1/users/me"
	ResourceAuth         Resource = "/api/v1/auth/*"
	ResourceRole         Resource = "/api/v1/roles/*"
	ResourcePermission   Resource = "/api/v1/permissions/*"
	ResourceTemplate     Resource = "/api/v1/templates/*"
	ResourceNotification Resource = "/api/v1/notifications/*"
	ResourceAdmin        Resource = "/api/v1/admin/*"
	ResourceDashboard    Resource = "/api/v1/admin/dashboard"
	ResourceAudit        Resource = "/api/v1/admin/audit/*"
	ResourceMetrics      Resource = "/metrics"
	ResourceHealth       Resource = "/livez"
)

// ActionExport represents a data export action (distinct from read).
const ActionExport Action = "export"

// Domain represents different tenants/domains
const (
	DomainDefault = "default"
	DomainSystem  = "system"
)

// CasbinService handles authorization using Casbin
type CasbinService struct {
	enforcer *casbin.Enforcer
	adapter  *gormadapter.Adapter
	logger   *logger.Logger
	mu       sync.RWMutex
}

// NewCasbinService creates a new Casbin authorization service
func NewCasbinService(cfg *config.Config, db *gorm.DB) (*CasbinService, error) {
	// Create adapter
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create Casbin adapter: %w", err)
	}

	// Load model
	modelText := getModelText()
	m, err := model.NewModelFromString(modelText)
	if err != nil {
		return nil, fmt.Errorf("failed to load Casbin model: %w", err)
	}

	// Create enforcer
	enforcer, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create Casbin enforcer: %w", err)
	}

	// Enable auto-save
	enforcer.EnableAutoSave(true)

	service := &CasbinService{
		enforcer: enforcer,
		adapter:  adapter,
		logger:   logger.Get().WithFields(logger.Fields{"service": "casbin"}),
	}

	// Initialize default policies
	if err := service.initializeDefaultPolicies(); err != nil {
		service.logger.Warn("Failed to initialize default policies", "error", err)
	}

	service.logger.Info("Casbin authorization service initialized")
	return service, nil
}

// Enforce checks if a subject has permission to perform an action on an object
func (s *CasbinService) Enforce(subject, domain, object string, action Action) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allowed, err := s.enforcer.Enforce(subject, domain, object, string(action))
	if err != nil {
		s.logger.Error("Failed to enforce policy",
			"subject", subject,
			"domain", domain,
			"object", object,
			"action", action,
			"error", err,
		)
		return false, err
	}

	s.logger.Debug("Authorization check",
		"subject", subject,
		"domain", domain,
		"object", object,
		"action", action,
		"allowed", allowed,
	)

	return allowed, nil
}

// EnforceUser checks if a user has permission
func (s *CasbinService) EnforceUser(userID uuid.UUID, domain string, object string, action Action) (bool, error) {
	return s.Enforce(userID.String(), domain, object, action)
}

// EnforceWithRoles checks if a user with specific roles has permission
func (s *CasbinService) EnforceWithRoles(userID uuid.UUID, roles []string, domain, object string, action Action) (bool, error) {
	// Check direct user permission
	allowed, err := s.EnforceUser(userID, domain, object, action)
	if err != nil || allowed {
		return allowed, err
	}

	// Check role-based permissions (policies use "role:" prefix)
	for _, role := range roles {
		roleSubject := role
		if !strings.HasPrefix(role, "role:") {
			roleSubject = "role:" + role
		}
		allowed, err = s.Enforce(roleSubject, domain, object, action)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}

	return false, nil
}

// AddPolicy adds a new policy
func (s *CasbinService) AddPolicy(subject, domain, object string, action Action, effect string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	added, err := s.enforcer.AddPolicy(subject, domain, object, string(action), effect)
	if err != nil {
		return fmt.Errorf("failed to add policy: %w", err)
	}

	if !added {
		return errors.NewConflict("policy already exists")
	}

	s.logger.Info("Policy added",
		"subject", subject,
		"domain", domain,
		"object", object,
		"action", action,
		"effect", effect,
	)

	return nil
}

// RemovePolicy removes a policy
func (s *CasbinService) RemovePolicy(subject, domain, object string, action Action, effect string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed, err := s.enforcer.RemovePolicy(subject, domain, object, string(action), effect)
	if err != nil {
		return fmt.Errorf("failed to remove policy: %w", err)
	}

	if !removed {
		return errors.NewNotFound("policy", "policy not found")
	}

	s.logger.Info("Policy removed",
		"subject", subject,
		"domain", domain,
		"object", object,
		"action", action,
	)

	return nil
}

// AddRoleForUser adds a role to a user in a domain
func (s *CasbinService) AddRoleForUser(userID uuid.UUID, role, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	added, err := s.enforcer.AddRoleForUser(userID.String(), role, domain)
	if err != nil {
		return fmt.Errorf("failed to add role for user: %w", err)
	}

	if !added {
		return errors.NewConflict("user already has this role")
	}

	s.logger.Info("Role added for user",
		"user_id", userID,
		"role", role,
		"domain", domain,
	)

	return nil
}

// RemoveRoleForUser removes a role from a user in a domain
func (s *CasbinService) RemoveRoleForUser(userID uuid.UUID, role, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed, err := s.enforcer.DeleteRoleForUser(userID.String(), role, domain)
	if err != nil {
		return fmt.Errorf("failed to remove role for user: %w", err)
	}

	if !removed {
		return errors.NewNotFound("role", "user does not have this role")
	}

	s.logger.Info("Role removed from user",
		"user_id", userID,
		"role", role,
		"domain", domain,
	)

	return nil
}

// AddRoleInheritance adds role inheritance (role1 inherits from role2)
func (s *CasbinService) AddRoleInheritance(role1, role2 string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	added, err := s.enforcer.AddNamedGroupingPolicy("g3", role1, role2)
	if err != nil {
		return fmt.Errorf("failed to add role inheritance: %w", err)
	}

	if !added {
		return errors.NewConflict("role inheritance already exists")
	}

	s.logger.Info("Role inheritance added",
		"role1", role1,
		"role2", role2,
	)

	return nil
}

// RemoveRoleInheritance removes role inheritance
func (s *CasbinService) RemoveRoleInheritance(role1, role2 string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed, err := s.enforcer.RemoveNamedGroupingPolicy("g3", role1, role2)
	if err != nil {
		return fmt.Errorf("failed to remove role inheritance: %w", err)
	}

	if !removed {
		return errors.NewNotFound("role inheritance", "role inheritance not found")
	}

	s.logger.Info("Role inheritance removed",
		"role1", role1,
		"role2", role2,
	)

	return nil
}

// GetRolesForUser gets all roles for a user in a domain
func (s *CasbinService) GetRolesForUser(userID uuid.UUID, domain string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roles := s.enforcer.GetRolesForUserInDomain(userID.String(), domain)

	return roles, nil
}

// GetUsersForRole gets all users with a specific role in a domain
func (s *CasbinService) GetUsersForRole(role, domain string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := s.enforcer.GetUsersForRoleInDomain(role, domain)

	return users, nil
}

// GetPermissionsForUser gets all permissions for a user
func (s *CasbinService) GetPermissionsForUser(userID uuid.UUID, domain string) ([][]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get direct permissions
	directPerms, err := s.enforcer.GetPermissionsForUser(userID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get direct permissions: %w", err)
	}

	// Get permissions through roles
	roles := s.enforcer.GetRolesForUserInDomain(userID.String(), domain)
	rolePerms := make([][]string, 0, len(roles))
	for _, role := range roles {
		perms, err := s.enforcer.GetPermissionsForUser(role)
		if err != nil {
			return nil, fmt.Errorf("failed to get permissions for role %s: %w", role, err)
		}
		rolePerms = append(rolePerms, perms...)
	}

	// Combine permissions
	allPerms := make([][]string, 0, len(directPerms)+len(rolePerms))
	allPerms = append(allPerms, directPerms...)
	allPerms = append(allPerms, rolePerms...)

	return allPerms, nil
}

// AddResourceGroup adds a resource to a resource group
func (s *CasbinService) AddResourceGroup(resource, group, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	added, err := s.enforcer.AddNamedGroupingPolicy("g2", resource, group, domain)
	if err != nil {
		return fmt.Errorf("failed to add resource group: %w", err)
	}

	if !added {
		return errors.NewConflict("resource already in group")
	}

	return nil
}

// RemoveResourceGroup removes a resource from a resource group
func (s *CasbinService) RemoveResourceGroup(resource, group, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed, err := s.enforcer.RemoveNamedGroupingPolicy("g2", resource, group, domain)
	if err != nil {
		return fmt.Errorf("failed to remove resource group: %w", err)
	}

	if !removed {
		return errors.NewNotFound("resource", "resource not in group")
	}

	return nil
}

// ClearUserPermissions removes all permissions and roles for a user
func (s *CasbinService) ClearUserPermissions(userID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove all roles
	if _, err := s.enforcer.DeleteRolesForUser(userID.String()); err != nil {
		return fmt.Errorf("failed to clear user roles: %w", err)
	}

	// Remove all direct permissions
	if _, err := s.enforcer.DeletePermissionsForUser(userID.String()); err != nil {
		return fmt.Errorf("failed to clear user permissions: %w", err)
	}

	s.logger.Info("Cleared all permissions for user", "user_id", userID)
	return nil
}

// ReloadPolicy reloads policies from database
func (s *CasbinService) ReloadPolicy() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.enforcer.LoadPolicy(); err != nil {
		return fmt.Errorf("failed to reload policies: %w", err)
	}

	s.logger.Info("Policies reloaded from database")
	return nil
}

// SavePolicy saves current policies to database
func (s *CasbinService) SavePolicy() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.enforcer.SavePolicy(); err != nil {
		return fmt.Errorf("failed to save policies: %w", err)
	}

	s.logger.Info("Policies saved to database")
	return nil
}

// initializeDefaultPolicies sets up default policies
func (s *CasbinService) initializeDefaultPolicies() error { //nolint:unparam // error return kept for interface consistency
	// Check if policies already exist — bootstrap handles the main sync
	policies, _ := s.enforcer.GetPolicy()
	if len(policies) > 0 {
		return nil // Bootstrap already synced policies
	}

	// Only seed policies for roles that are NOT managed via DB permissions
	// (guest and api_client don't have DB user/role representations)
	_ = s.AddPolicy("role:guest", DomainDefault, string(ResourceHealth), ActionRead, "allow")
	_ = s.AddPolicy("role:guest", DomainDefault, string(ResourceAuth), ActionCreate, "allow")

	_ = s.AddPolicy("role:api_client", DomainDefault, string(ResourceNotification), ActionCreate, "allow")
	_ = s.AddPolicy("role:api_client", DomainDefault, string(ResourceTemplate), ActionRead, "allow")

	// User self-service policies (not tied to DB permissions — always available to authenticated users)
	_ = s.AddPolicy("role:user", DomainDefault, string(ResourceUserSelf), ActionRead, "allow")
	_ = s.AddPolicy("role:user", DomainDefault, string(ResourceUserSelf), ActionUpdate, "allow")
	_ = s.AddPolicy("role:user", DomainDefault, string(ResourceUserProfile), ActionRead, "allow")
	_ = s.AddPolicy("role:user", DomainDefault, string(ResourceUserProfile), ActionUpdate, "allow")

	// System admin wildcard — kept as safety net
	_ = s.AddPolicy("role:system_admin", DomainDefault, "*", ActionManage, "allow")

	s.logger.Info("Default policies initialized (minimal — bootstrap handles DB-synced policies)")
	return nil
}

// getModelText returns the Casbin model as a string
//
//nolint:lll // Casbin matcher expression must remain single-line for parser compatibility
func getModelText() string {
	return `
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act, eft

[role_definition]
g = _, _, _
g2 = _, _, _
g3 = _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = (g(r.sub, p.sub, r.dom) || g3(r.sub, p.sub)) && r.dom == p.dom && (p.obj == "*" || g2(r.obj, p.obj, r.dom) || keyMatch2(r.obj, p.obj)) && (p.act == "manage" || r.act == p.act)
`
}

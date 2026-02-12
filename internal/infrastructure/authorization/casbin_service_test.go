package authorization

import (
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
)

func newInMemoryCasbinService(t *testing.T) *CasbinService {
	t.Helper()

	m, err := model.NewModelFromString(getModelText())
	if err != nil {
		t.Fatalf("failed to build casbin model: %v", err)
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		t.Fatalf("failed to create casbin enforcer: %v", err)
	}

	return &CasbinService{
		enforcer: e,
		logger:   logger.Get().WithFields(logger.Fields{"service": "casbin-test"}),
	}
}

func TestCasbinServiceEnforceAllowAndDeny(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	subject := "role:tester"

	if err := svc.AddPolicy(subject, DomainDefault, "/api/users/1", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to add allow policy: %v", err)
	}
	allowed, err := svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
	if err != nil {
		t.Fatalf("enforce returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected allow policy to permit access")
	}

	if err := svc.AddPolicy(subject, DomainDefault, "/api/users/1", ActionRead, "deny"); err != nil {
		t.Fatalf("failed to add deny policy: %v", err)
	}
	allowed, err = svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
	if err != nil {
		t.Fatalf("enforce returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected explicit deny policy to block access")
	}
}

func TestCasbinServiceAddAndRemovePolicy(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	subject := "role:auditor"

	if err := svc.AddPolicy(subject, DomainDefault, "/api/users/1", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to add policy: %v", err)
	}
	allowed, _ := svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
	if !allowed {
		t.Fatalf("expected policy to be enforced before removal")
	}

	if err := svc.RemovePolicy(subject, DomainDefault, "/api/users/1", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to remove policy: %v", err)
	}
	allowed, _ = svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
	if allowed {
		t.Fatalf("expected access to be denied after policy removal")
	}
}

func TestCasbinServiceUserRoleAssignmentAndRemoval(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	userID := uuid.New()
	role := "role:support"

	if err := svc.AddPolicy(role, DomainDefault, "/api/tickets/1", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to add role policy: %v", err)
	}
	if err := svc.AddRoleForUser(userID, role, DomainDefault); err != nil {
		t.Fatalf("failed to add role for user: %v", err)
	}

	allowed, err := svc.EnforceUser(userID, DomainDefault, "/api/tickets/1", ActionRead)
	if err != nil {
		t.Fatalf("enforce returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected assigned role to grant access")
	}

	if err := svc.RemoveRoleForUser(userID, role, DomainDefault); err != nil {
		t.Fatalf("failed to remove role for user: %v", err)
	}
	allowed, err = svc.EnforceUser(userID, DomainDefault, "/api/tickets/1", ActionRead)
	if err != nil {
		t.Fatalf("enforce returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected access to be denied after role removal")
	}
}

func TestCasbinServiceRoleInheritance(t *testing.T) {
	svc := newInMemoryCasbinService(t)

	if err := svc.AddPolicy("role:parent", DomainDefault, "/api/reports/*", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to add parent role policy: %v", err)
	}
	if err := svc.AddRoleInheritance("role:child", "role:parent"); err != nil {
		t.Fatalf("failed to add role inheritance: %v", err)
	}

	allowed, err := svc.Enforce("role:child", DomainDefault, "/api/reports/42", ActionRead)
	if err != nil {
		t.Fatalf("enforce returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected child role to inherit permission from parent")
	}
}

func TestCasbinServiceRoleAndPermissionQueries(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	userID := uuid.New()
	role := "role:manager"

	if err := svc.AddPolicy(role, DomainDefault, "/api/templates/1", ActionUpdate, "allow"); err != nil {
		t.Fatalf("failed to add policy: %v", err)
	}
	if err := svc.AddRoleForUser(userID, role, DomainDefault); err != nil {
		t.Fatalf("failed to assign role: %v", err)
	}

	roles, err := svc.GetRolesForUser(userID, DomainDefault)
	if err != nil {
		t.Fatalf("GetRolesForUser failed: %v", err)
	}
	if len(roles) != 1 || roles[0] != role {
		t.Fatalf("expected assigned role in user role list, got %v", roles)
	}

	users, err := svc.GetUsersForRole(role, DomainDefault)
	if err != nil {
		t.Fatalf("GetUsersForRole failed: %v", err)
	}
	if len(users) != 1 || users[0] != userID.String() {
		t.Fatalf("expected assigned user in role user list, got %v", users)
	}

	perms, err := svc.GetPermissionsForUser(userID, DomainDefault)
	if err != nil {
		t.Fatalf("GetPermissionsForUser failed: %v", err)
	}
	if len(perms) == 0 {
		t.Fatalf("expected inherited role permissions for user")
	}
}

func TestCasbinServiceClearUserPermissions(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	userID := uuid.New()
	role := "role:ops"

	if err := svc.AddPolicy(role, DomainDefault, "/api/ops/*", ActionManage, "allow"); err != nil {
		t.Fatalf("failed to add role policy: %v", err)
	}
	if err := svc.AddRoleForUser(userID, role, DomainDefault); err != nil {
		t.Fatalf("failed to assign role: %v", err)
	}

	allowed, _ := svc.EnforceUser(userID, DomainDefault, "/api/ops/task", ActionManage)
	if !allowed {
		t.Fatalf("expected access to be allowed before cleanup")
	}

	if err := svc.ClearUserPermissions(userID); err != nil {
		t.Fatalf("failed to clear user permissions: %v", err)
	}

	allowed, _ = svc.EnforceUser(userID, DomainDefault, "/api/ops/task", ActionManage)
	if allowed {
		t.Fatalf("expected access to be denied after cleanup")
	}
}

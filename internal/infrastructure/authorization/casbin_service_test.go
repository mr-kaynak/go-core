package authorization

import (
	"net/http"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
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
	if err := svc.AddResourceGroup("/api/users/1", "/api/users/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}

	t.Run("AllowPolicy", func(t *testing.T) {
		if err := svc.AddPolicy(subject, DomainDefault, "/api/users/*", ActionRead, "allow"); err != nil {
			t.Fatalf("failed to add allow policy: %v", err)
		}
		allowed, err := svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
		if err != nil {
			t.Fatalf("enforce returned error: %v", err)
		}
		if !allowed {
			t.Fatalf("expected allow policy to permit access")
		}
	})

	t.Run("DenyOverridesAllow", func(t *testing.T) {
		if err := svc.AddPolicy(subject, DomainDefault, "/api/users/*", ActionRead, "deny"); err != nil {
			t.Fatalf("failed to add deny policy: %v", err)
		}
		allowed, err := svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
		if err != nil {
			t.Fatalf("enforce returned error: %v", err)
		}
		if allowed {
			t.Fatalf("expected explicit deny policy to block access")
		}
	})
}

func TestCasbinServiceAddAndRemovePolicy(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	subject := "role:auditor"
	if err := svc.AddResourceGroup("/api/users/1", "/api/users/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}

	t.Run("AddAndEnforce", func(t *testing.T) {
		if err := svc.AddPolicy(subject, DomainDefault, "/api/users/*", ActionRead, "allow"); err != nil {
			t.Fatalf("failed to add policy: %v", err)
		}
		allowed, _ := svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
		if !allowed {
			t.Fatalf("expected policy to be enforced before removal")
		}
	})

	t.Run("RemoveAndVerifyDenied", func(t *testing.T) {
		if err := svc.RemovePolicy(subject, DomainDefault, "/api/users/*", ActionRead, "allow"); err != nil {
			t.Fatalf("failed to remove policy: %v", err)
		}
		allowed, _ := svc.Enforce(subject, DomainDefault, "/api/users/1", ActionRead)
		if allowed {
			t.Fatalf("expected access to be denied after policy removal")
		}
	})
}

func TestCasbinServiceUserRoleAssignmentAndRemoval(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	userID := uuid.New()
	role := "role:support"
	if err := svc.AddResourceGroup("/api/tickets/1", "/api/tickets/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}
	if err := svc.AddPolicy(role, DomainDefault, "/api/tickets/*", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to add role policy: %v", err)
	}

	t.Run("AssignRoleGrantsAccess", func(t *testing.T) {
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
	})

	t.Run("RemoveRoleRevokesAccess", func(t *testing.T) {
		if err := svc.RemoveRoleForUser(userID, role, DomainDefault); err != nil {
			t.Fatalf("failed to remove role for user: %v", err)
		}
		allowed, err := svc.EnforceUser(userID, DomainDefault, "/api/tickets/1", ActionRead)
		if err != nil {
			t.Fatalf("enforce returned error: %v", err)
		}
		if allowed {
			t.Fatalf("expected access to be denied after role removal")
		}
	})
}

func TestCasbinServiceRoleInheritance(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	if err := svc.AddResourceGroup("/api/reports/42", "/api/reports/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}

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
	if err := svc.AddResourceGroup("/api/templates/1", "/api/templates/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}
	if err := svc.AddPolicy(role, DomainDefault, "/api/templates/*", ActionUpdate, "allow"); err != nil {
		t.Fatalf("failed to add policy: %v", err)
	}
	if err := svc.AddRoleForUser(userID, role, DomainDefault); err != nil {
		t.Fatalf("failed to assign role: %v", err)
	}

	t.Run("GetRolesForUser", func(t *testing.T) {
		roles, err := svc.GetRolesForUser(userID, DomainDefault)
		if err != nil {
			t.Fatalf("GetRolesForUser failed: %v", err)
		}
		if len(roles) != 1 || roles[0] != role {
			t.Fatalf("expected assigned role in user role list, got %v", roles)
		}
	})

	t.Run("GetUsersForRole", func(t *testing.T) {
		users, err := svc.GetUsersForRole(role, DomainDefault)
		if err != nil {
			t.Fatalf("GetUsersForRole failed: %v", err)
		}
		if len(users) != 1 || users[0] != userID.String() {
			t.Fatalf("expected assigned user in role user list, got %v", users)
		}
	})

	t.Run("GetPermissionsForUser", func(t *testing.T) {
		perms, err := svc.GetPermissionsForUser(userID, DomainDefault)
		if err != nil {
			t.Fatalf("GetPermissionsForUser failed: %v", err)
		}
		if len(perms) == 0 {
			t.Fatalf("expected inherited role permissions for user")
		}
	})
}

func TestCasbinServiceClearUserPermissions(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	userID := uuid.New()
	role := "role:ops"
	if err := svc.AddResourceGroup("/api/ops/task", "/api/ops/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}

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

func TestCasbinServiceRoleInheritance_CrossDomainViaG3(t *testing.T) {
	svc := newInMemoryCasbinService(t)

	// g3 is domain-less: child inherits parent regardless of domain.
	// Policy is scoped to "default" domain.
	if err := svc.AddResourceGroup("/api/data/1", "/api/data/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}
	if err := svc.AddPolicy("role:senior", DomainDefault, "/api/data/*", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to add policy: %v", err)
	}
	if err := svc.AddRoleInheritance("role:junior", "role:senior"); err != nil {
		t.Fatalf("failed to add role inheritance: %v", err)
	}

	t.Run("InheritedRoleGrantsAccessInSameDomain", func(t *testing.T) {
		allowed, err := svc.Enforce("role:junior", DomainDefault, "/api/data/1", ActionRead)
		if err != nil {
			t.Fatalf("enforce error: %v", err)
		}
		if !allowed {
			t.Fatal("expected junior to inherit senior permission in default domain")
		}
	})

	t.Run("NoAccessWithoutDomainPolicy", func(t *testing.T) {
		// g3 inheritance is domain-less but matcher still requires r.dom == p.dom.
		// There is no policy for "system" domain, so access must be denied.
		allowed, err := svc.Enforce("role:junior", DomainSystem, "/api/data/1", ActionRead)
		if err != nil {
			t.Fatalf("enforce error: %v", err)
		}
		if allowed {
			t.Fatal("expected junior to be denied in system domain where no policy exists")
		}
	})
}

func TestCasbinServiceDuplicatePolicy(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	subject := "role:dup"

	if err := svc.AddPolicy(subject, DomainDefault, "/api/dup/*", ActionRead, "allow"); err != nil {
		t.Fatalf("first add should succeed: %v", err)
	}

	err := svc.AddPolicy(subject, DomainDefault, "/api/dup/*", ActionRead, "allow")
	if err == nil {
		t.Fatal("expected conflict error on duplicate policy")
	}
	pd := errors.GetProblemDetail(err)
	if pd == nil || pd.Status != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %v", err)
	}
}

func TestCasbinServiceEnforceWithRoles(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	userID := uuid.New()
	role := "role:viewer"

	if err := svc.AddResourceGroup("/api/items/5", "/api/items/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}
	if err := svc.AddPolicy(role, DomainDefault, "/api/items/*", ActionRead, "allow"); err != nil {
		t.Fatalf("failed to add policy: %v", err)
	}

	t.Run("DirectUserPermission", func(t *testing.T) {
		// User has no direct permission and no role assigned — should be denied.
		allowed, err := svc.EnforceWithRoles(userID, nil, DomainDefault, "/api/items/5", ActionRead)
		if err != nil {
			t.Fatalf("enforce error: %v", err)
		}
		if allowed {
			t.Fatal("expected denial when user has no direct permission and no roles")
		}
	})

	t.Run("RoleBasedPermission", func(t *testing.T) {
		// Pass the role explicitly — should be granted.
		allowed, err := svc.EnforceWithRoles(userID, []string{role}, DomainDefault, "/api/items/5", ActionRead)
		if err != nil {
			t.Fatalf("enforce error: %v", err)
		}
		if !allowed {
			t.Fatal("expected access via explicit role list")
		}
	})

	t.Run("WrongRoleDenied", func(t *testing.T) {
		allowed, err := svc.EnforceWithRoles(userID, []string{"role:nobody"}, DomainDefault, "/api/items/5", ActionRead)
		if err != nil {
			t.Fatalf("enforce error: %v", err)
		}
		if allowed {
			t.Fatal("expected denial when passing a role without matching policy")
		}
	})
}

func TestCasbinServiceRemoveNonexistentPolicy(t *testing.T) {
	svc := newInMemoryCasbinService(t)

	err := svc.RemovePolicy("role:ghost", DomainDefault, "/api/none/*", ActionRead, "allow")
	if err == nil {
		t.Fatal("expected not found error when removing nonexistent policy")
	}
	pd := errors.GetProblemDetail(err)
	if pd == nil || pd.Status != http.StatusNotFound {
		t.Fatalf("expected 404 not found, got %v", err)
	}
}

func TestCasbinServiceDuplicateRoleAssignment(t *testing.T) {
	svc := newInMemoryCasbinService(t)
	userID := uuid.New()
	role := "role:engineer"

	if err := svc.AddRoleForUser(userID, role, DomainDefault); err != nil {
		t.Fatalf("first assignment should succeed: %v", err)
	}

	err := svc.AddRoleForUser(userID, role, DomainDefault)
	if err == nil {
		t.Fatal("expected conflict error on duplicate role assignment")
	}
	pd := errors.GetProblemDetail(err)
	if pd == nil || pd.Status != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %v", err)
	}
}

func TestCasbinServiceRemoveRoleInheritance(t *testing.T) {
	svc := newInMemoryCasbinService(t)

	if err := svc.AddResourceGroup("/api/docs/1", "/api/docs/*", DomainDefault); err != nil {
		t.Fatalf("failed to add resource group: %v", err)
	}
	if err := svc.AddPolicy("role:lead", DomainDefault, "/api/docs/*", ActionUpdate, "allow"); err != nil {
		t.Fatalf("failed to add policy: %v", err)
	}
	if err := svc.AddRoleInheritance("role:member", "role:lead"); err != nil {
		t.Fatalf("failed to add inheritance: %v", err)
	}

	t.Run("InheritedPermissionBeforeRemoval", func(t *testing.T) {
		allowed, err := svc.Enforce("role:member", DomainDefault, "/api/docs/1", ActionUpdate)
		if err != nil {
			t.Fatalf("enforce error: %v", err)
		}
		if !allowed {
			t.Fatal("expected member to inherit lead permission")
		}
	})

	t.Run("PermissionDroppedAfterRemoval", func(t *testing.T) {
		if err := svc.RemoveRoleInheritance("role:member", "role:lead"); err != nil {
			t.Fatalf("failed to remove inheritance: %v", err)
		}
		allowed, err := svc.Enforce("role:member", DomainDefault, "/api/docs/1", ActionUpdate)
		if err != nil {
			t.Fatalf("enforce error: %v", err)
		}
		if allowed {
			t.Fatal("expected member to lose inherited permission after inheritance removal")
		}
	})
}

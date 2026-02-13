package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/infrastructure/authorization"
)

// PolicyHandler handles policy-related HTTP requests
type PolicyHandler struct {
	casbinService policyAuthorizer
}

//nolint:dupl // mirrored in test double to keep behavior-focused tests decoupled from concrete Casbin service
type policyAuthorizer interface {
	AddPolicy(subject, domain, object string, action authorization.Action, effect string) error
	RemovePolicy(subject, domain, object string, action authorization.Action, effect string) error
	AddRoleForUser(userID uuid.UUID, role, domain string) error
	RemoveRoleForUser(userID uuid.UUID, role, domain string) error
	GetRolesForUser(userID uuid.UUID, domain string) ([]string, error)
	GetPermissionsForUser(userID uuid.UUID, domain string) ([][]string, error)
	GetUsersForRole(role, domain string) ([]string, error)
	AddResourceGroup(resource, group, domain string) error
	RemoveResourceGroup(resource, group, domain string) error
	Enforce(subject, domain, object string, action authorization.Action) (bool, error)
	ReloadPolicy() error
	SavePolicy() error
}

// NewPolicyHandler creates a new policy handler
func NewPolicyHandler(casbinService policyAuthorizer) *PolicyHandler {
	return &PolicyHandler{
		casbinService: casbinService,
	}
}

// ensureService returns an error when the underlying policy service is nil.
// This happens when Casbin initialisation fails during server startup
// (graceful degradation).
func (h *PolicyHandler) ensureService() error {
	if h.casbinService == nil {
		return errors.NewInternal("Policy service unavailable")
	}
	return nil
}

// PolicyRequest represents a policy add/remove request
type PolicyRequest struct {
	Subject string `json:"subject" validate:"required"`
	Domain  string `json:"domain" validate:"required"`
	Object  string `json:"object" validate:"required"`
	Action  string `json:"action" validate:"required"`
	Effect  string `json:"effect" validate:"required,oneof=allow deny"`
}

// UserRoleRequest represents a user role assignment request
type UserRoleRequest struct {
	Role   string `json:"role" validate:"required"`
	Domain string `json:"domain"`
}

// ResourceGroupRequest represents a resource group request
type ResourceGroupRequest struct {
	Resource string `json:"resource" validate:"required"`
	Group    string `json:"group" validate:"required"`
	Domain   string `json:"domain"`
}

// CheckPermissionRequest represents a permission check request
type CheckPermissionRequest struct {
	Subject string `json:"subject" validate:"required"`
	Domain  string `json:"domain"`
	Object  string `json:"object" validate:"required"`
	Action  string `json:"action" validate:"required"`
}

// BulkPolicyRequest represents a bulk policy request
type BulkPolicyRequest struct {
	Policies []PolicyRequest `json:"policies" validate:"required,min=1"`
}

// RegisterRoutes registers policy routes (all require authentication + admin role)
func (h *PolicyHandler) RegisterRoutes(router fiber.Router, handlers ...fiber.Handler) {
	policies := router.Group("/policies", handlers...)

	// Policy management
	policies.Post("/", h.AddPolicy)
	policies.Delete("/", h.RemovePolicy)
	policies.Get("/reload", h.ReloadPolicies)
	policies.Post("/save", h.SavePolicies)

	// User role management
	policies.Post("/users/:user_id/roles", h.AddRoleToUser)
	policies.Delete("/users/:user_id/roles", h.RemoveRoleFromUser)
	policies.Get("/users/:user_id/roles", h.GetUserRoles)
	policies.Get("/users/:user_id/permissions", h.GetUserPermissions)

	// Role management
	policies.Get("/roles/:role/users", h.GetUsersForRole)

	// Resource groups
	policies.Post("/resource-groups", h.AddResourceGroup)
	policies.Delete("/resource-groups", h.RemoveResourceGroup)

	// Enforcement check
	policies.Post("/check", h.CheckPermission)
}

// AddPolicy adds a new policy
// @Summary Add a policy
// @Description Add a new Casbin policy rule
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body PolicyRequest true "Policy definition"
// @Success 200 {object} fiber.Map "Policy added"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies [post]
func (h *PolicyHandler) AddPolicy(c *fiber.Ctx) error {
	var req PolicyRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := h.ensureService(); err != nil {
		return err
	}
	if err := h.casbinService.AddPolicy(req.Subject, req.Domain, req.Object,
		authorization.Action(req.Action), req.Effect); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Policy added successfully",
		"policy": fiber.Map{
			"subject": req.Subject,
			"domain":  req.Domain,
			"object":  req.Object,
			"action":  req.Action,
			"effect":  req.Effect,
		},
	})
}

// RemovePolicy removes a policy
// @Summary Remove a policy
// @Description Remove an existing Casbin policy rule
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body PolicyRequest true "Policy to remove"
// @Success 200 {object} fiber.Map "Policy removed"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies [delete]
func (h *PolicyHandler) RemovePolicy(c *fiber.Ctx) error {
	var req PolicyRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if err := h.ensureService(); err != nil {
		return err
	}
	if err := h.casbinService.RemovePolicy(req.Subject, req.Domain, req.Object,
		authorization.Action(req.Action), req.Effect); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Policy removed successfully",
	})
}

// handleUserRole is a shared helper for AddRoleToUser and RemoveRoleFromUser
func (h *PolicyHandler) handleUserRole(c *fiber.Ctx, action func(uuid.UUID, string, string) error, successMsg string) error {
	userID, err := uuid.Parse(c.Params("user_id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	var req UserRoleRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if req.Domain == "" {
		req.Domain = authorization.DomainDefault
	}

	if err := action(userID, req.Role, req.Domain); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": successMsg,
		"user_id": userID,
		"role":    req.Role,
		"domain":  req.Domain,
	})
}

// AddRoleToUser adds a role to a user
// @Summary Add role to user
// @Description Assign a role to a user in a domain
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param user_id path string true "User UUID"
// @Param request body UserRoleRequest true "Role assignment"
// @Success 200 {object} fiber.Map "Role added"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/users/{user_id}/roles [post]
func (h *PolicyHandler) AddRoleToUser(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleUserRole(c, h.casbinService.AddRoleForUser, "Role added to user successfully")
}

// RemoveRoleFromUser removes a role from a user
// @Summary Remove role from user
// @Description Remove a role from a user in a domain
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param user_id path string true "User UUID"
// @Param request body UserRoleRequest true "Role to remove"
// @Success 200 {object} fiber.Map "Role removed"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/users/{user_id}/roles [delete]
func (h *PolicyHandler) RemoveRoleFromUser(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleUserRole(c, h.casbinService.RemoveRoleForUser, "Role removed from user successfully")
}

// GetUserRoles gets all roles for a user
// @Summary Get user roles
// @Description Get all roles assigned to a user
// @Tags Policies
// @Security Bearer
// @Produce json
// @Param user_id path string true "User UUID"
// @Param domain query string false "Domain filter" default(default)
// @Success 200 {object} fiber.Map "User roles"
// @Failure 400 {object} errors.ProblemDetail "Invalid user ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/users/{user_id}/roles [get]
func (h *PolicyHandler) GetUserRoles(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("user_id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	domain := c.Query("domain", authorization.DomainDefault)

	if err := h.ensureService(); err != nil {
		return err
	}
	roles, err := h.casbinService.GetRolesForUser(userID, domain)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"user_id": userID,
		"domain":  domain,
		"roles":   roles,
	})
}

// GetUserPermissions gets all permissions for a user
// @Summary Get user permissions
// @Description Get all permissions for a user in a domain
// @Tags Policies
// @Security Bearer
// @Produce json
// @Param user_id path string true "User UUID"
// @Param domain query string false "Domain filter" default(default)
// @Success 200 {object} fiber.Map "User permissions"
// @Failure 400 {object} errors.ProblemDetail "Invalid user ID"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/users/{user_id}/permissions [get]
func (h *PolicyHandler) GetUserPermissions(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("user_id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	domain := c.Query("domain", authorization.DomainDefault)

	if err := h.ensureService(); err != nil {
		return err
	}
	permissions, err := h.casbinService.GetPermissionsForUser(userID, domain)
	if err != nil {
		return err
	}

	// Format permissions for response
	var formattedPerms []fiber.Map
	for _, perm := range permissions {
		if len(perm) >= 5 {
			formattedPerms = append(formattedPerms, fiber.Map{
				"subject": perm[0],
				"domain":  perm[1],
				"object":  perm[2],
				"action":  perm[3],
				"effect":  perm[4],
			})
		}
	}

	return c.JSON(fiber.Map{
		"user_id":     userID,
		"domain":      domain,
		"permissions": formattedPerms,
	})
}

// GetUsersForRole gets all users with a specific role
// @Summary Get users for role
// @Description Get all users with a specific role
// @Tags Policies
// @Security Bearer
// @Produce json
// @Param role path string true "Role name"
// @Param domain query string false "Domain filter" default(default)
// @Success 200 {object} fiber.Map "Users with role"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/roles/{role}/users [get]
func (h *PolicyHandler) GetUsersForRole(c *fiber.Ctx) error {
	role := c.Params("role")
	domain := c.Query("domain", authorization.DomainDefault)

	if err := h.ensureService(); err != nil {
		return err
	}
	users, err := h.casbinService.GetUsersForRole(role, domain)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"role":   role,
		"domain": domain,
		"users":  users,
	})
}

// handleResourceGroup is a shared helper for AddResourceGroup and RemoveResourceGroup
func (h *PolicyHandler) handleResourceGroup(c *fiber.Ctx, action func(string, string, string) error, successMsg string) error {
	var req ResourceGroupRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if req.Domain == "" {
		req.Domain = authorization.DomainDefault
	}

	if err := action(req.Resource, req.Group, req.Domain); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message":  successMsg,
		"resource": req.Resource,
		"group":    req.Group,
		"domain":   req.Domain,
	})
}

// AddResourceGroup adds a resource to a resource group
// @Summary Add resource to group
// @Description Add a resource to a resource group
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body ResourceGroupRequest true "Resource group assignment"
// @Success 200 {object} fiber.Map "Resource added to group"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/resource-groups [post]
func (h *PolicyHandler) AddResourceGroup(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleResourceGroup(c, h.casbinService.AddResourceGroup, "Resource added to group successfully")
}

// RemoveResourceGroup removes a resource from a resource group
// @Summary Remove resource from group
// @Description Remove a resource from a resource group
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body ResourceGroupRequest true "Resource group to remove"
// @Success 200 {object} fiber.Map "Resource removed from group"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/resource-groups [delete]
func (h *PolicyHandler) RemoveResourceGroup(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleResourceGroup(c, h.casbinService.RemoveResourceGroup, "Resource removed from group successfully")
}

// CheckPermission checks if a subject has permission
// @Summary Check permission
// @Description Check if a subject has permission to perform an action
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body CheckPermissionRequest true "Permission check"
// @Success 200 {object} fiber.Map "Permission check result"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/check [post]
func (h *PolicyHandler) CheckPermission(c *fiber.Ctx) error {
	var req CheckPermissionRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if req.Domain == "" {
		req.Domain = authorization.DomainDefault
	}

	if err := h.ensureService(); err != nil {
		return err
	}
	allowed, err := h.casbinService.Enforce(req.Subject, req.Domain, req.Object,
		authorization.Action(req.Action))
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"allowed": allowed,
		"check": fiber.Map{
			"subject": req.Subject,
			"domain":  req.Domain,
			"object":  req.Object,
			"action":  req.Action,
		},
	})
}

// ReloadPolicies reloads policies from database
// @Summary Reload policies
// @Description Reload all policies from database
// @Tags Policies
// @Security Bearer
// @Produce json
// @Success 200 {object} fiber.Map "Policies reloaded"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/reload [get]
func (h *PolicyHandler) ReloadPolicies(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	if err := h.casbinService.ReloadPolicy(); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Policies reloaded successfully",
	})
}

// SavePolicies saves current policies to database
// @Summary Save policies
// @Description Save current policies to database
// @Tags Policies
// @Security Bearer
// @Produce json
// @Success 200 {object} fiber.Map "Policies saved"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/save [post]
func (h *PolicyHandler) SavePolicies(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	if err := h.casbinService.SavePolicy(); err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"message": "Policies saved successfully",
	})
}

// BulkAddPolicies adds multiple policies at once
// @Summary Bulk add policies
// @Description Add multiple policies at once
// @Tags Policies
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body BulkPolicyRequest true "Bulk policy request"
// @Success 200 {object} fiber.Map "Bulk operation result"
// @Failure 400 {object} errors.ProblemDetail "Invalid request"
// @Failure 401 {object} errors.ProblemDetail "Unauthorized"
// @Failure 403 {object} errors.ProblemDetail "Forbidden"
// @Router /policies/bulk [post]
func (h *PolicyHandler) BulkAddPolicies(c *fiber.Ctx) error {
	var req BulkPolicyRequest

	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	successCount := 0
	failedCount := 0
	var errMessages []string

	if err := h.ensureService(); err != nil {
		return err
	}

	for _, policy := range req.Policies {
		if err := h.casbinService.AddPolicy(policy.Subject, policy.Domain, policy.Object,
			authorization.Action(policy.Action), policy.Effect); err != nil {
			failedCount++
			errMessages = append(errMessages, err.Error())
		} else {
			successCount++
		}
	}

	return c.JSON(fiber.Map{
		"message": "Bulk policy addition completed",
		"success": successCount,
		"failed":  failedCount,
		"errors":  errMessages,
	})
}

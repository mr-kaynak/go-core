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

// RegisterRoutes registers policy routes
func (h *PolicyHandler) RegisterRoutes(router fiber.Router) {
	policies := router.Group("/policies")

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
func (h *PolicyHandler) AddPolicy(c *fiber.Ctx) error {
	var req struct {
		Subject string `json:"subject" validate:"required"`
		Domain  string `json:"domain" validate:"required"`
		Object  string `json:"object" validate:"required"`
		Action  string `json:"action" validate:"required"`
		Effect  string `json:"effect" validate:"required,oneof=allow deny"`
	}

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
func (h *PolicyHandler) RemovePolicy(c *fiber.Ctx) error {
	var req struct {
		Subject string `json:"subject" validate:"required"`
		Domain  string `json:"domain" validate:"required"`
		Object  string `json:"object" validate:"required"`
		Action  string `json:"action" validate:"required"`
		Effect  string `json:"effect" validate:"required"`
	}

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

	var req struct {
		Role   string `json:"role" validate:"required"`
		Domain string `json:"domain"`
	}

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
func (h *PolicyHandler) AddRoleToUser(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleUserRole(c, h.casbinService.AddRoleForUser, "Role added to user successfully")
}

// RemoveRoleFromUser removes a role from a user
func (h *PolicyHandler) RemoveRoleFromUser(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleUserRole(c, h.casbinService.RemoveRoleForUser, "Role removed from user successfully")
}

// GetUserRoles gets all roles for a user
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
	var req struct {
		Resource string `json:"resource" validate:"required"`
		Group    string `json:"group" validate:"required"`
		Domain   string `json:"domain"`
	}

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
func (h *PolicyHandler) AddResourceGroup(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleResourceGroup(c, h.casbinService.AddResourceGroup, "Resource added to group successfully")
}

// RemoveResourceGroup removes a resource from a resource group
func (h *PolicyHandler) RemoveResourceGroup(c *fiber.Ctx) error {
	if err := h.ensureService(); err != nil {
		return err
	}
	return h.handleResourceGroup(c, h.casbinService.RemoveResourceGroup, "Resource removed from group successfully")
}

// CheckPermission checks if a subject has permission
func (h *PolicyHandler) CheckPermission(c *fiber.Ctx) error {
	var req struct {
		Subject string `json:"subject" validate:"required"`
		Domain  string `json:"domain"`
		Object  string `json:"object" validate:"required"`
		Action  string `json:"action" validate:"required"`
	}

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
func (h *PolicyHandler) BulkAddPolicies(c *fiber.Ctx) error {
	var req struct {
		Policies []struct {
			Subject string `json:"subject" validate:"required"`
			Domain  string `json:"domain" validate:"required"`
			Object  string `json:"object" validate:"required"`
			Action  string `json:"action" validate:"required"`
			Effect  string `json:"effect" validate:"required,oneof=allow deny"`
		} `json:"policies" validate:"required,min=1"`
	}

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

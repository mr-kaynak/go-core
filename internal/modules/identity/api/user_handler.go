package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/validation"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// --- Request / Response DTOs ---

// UpdateProfileRequest represents a profile update request.
type UpdateProfileRequest struct {
	FirstName string          `json:"first_name" validate:"max=50"`
	LastName  string          `json:"last_name" validate:"max=50"`
	Phone     string          `json:"phone" validate:"omitempty,phone"`
	Metadata  domain.Metadata `json:"metadata"`
}

// ChangePasswordRequest represents a password change request.
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,password"`
}

// AdminCreateUserRequest represents an admin user creation request.
type AdminCreateUserRequest struct {
	Email     string `json:"email" validate:"required,email"`
	Username  string `json:"username" validate:"required,username"`
	Password  string `json:"password" validate:"required,password"`
	FirstName string `json:"first_name" validate:"max=50"`
	LastName  string `json:"last_name" validate:"max=50"`
	Phone     string `json:"phone" validate:"omitempty,phone"`
	Verified  bool   `json:"verified"`
}

// AdminUpdateUserRequest represents an admin user update request.
type AdminUpdateUserRequest struct {
	Email     string          `json:"email" validate:"omitempty,email"`
	Username  string          `json:"username" validate:"omitempty,username"`
	FirstName string          `json:"first_name" validate:"max=50"`
	LastName  string          `json:"last_name" validate:"max=50"`
	Phone     string          `json:"phone" validate:"omitempty,phone"`
	Metadata  domain.Metadata `json:"metadata"`
}

// UpdateStatusRequest represents a status change request.
type UpdateStatusRequest struct {
	Status string `json:"status" validate:"required,oneof=active inactive locked"`
}

// AssignRoleRequest represents a role assignment request.
type AssignRoleRequest struct {
	RoleID uuid.UUID `json:"role_id" validate:"required"`
}

// ListUsersResponse is the standardized paginated response for users.
type ListUsersResponse struct {
	Items      []*domain.User         `json:"items"`
	Pagination apiresponse.Pagination `json:"pagination"`
}

// ListAuditLogsResponse is the standardized paginated response for audit logs.
type ListAuditLogsResponse struct {
	Items      []*domain.AuditLog     `json:"items"`
	Pagination apiresponse.Pagination `json:"pagination"`
}

// --- Handler ---

// UserHandler handles user management HTTP requests.
type UserHandler struct {
	userService  *service.UserService
	authService  *service.AuthService
	auditService *service.AuditService
}

// NewUserHandler creates a new user handler.
func NewUserHandler(userService *service.UserService, authService *service.AuthService) *UserHandler {
	return &UserHandler{
		userService: userService,
		authService: authService,
	}
}

// SetAuditService sets the optional audit service.
func (h *UserHandler) SetAuditService(as *service.AuditService) {
	h.auditService = as
}

func (h *UserHandler) audit(c *fiber.Ctx, userID *uuid.UUID, action, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		h.auditService.LogAction(userID, action, auditResourceUser, resourceID, c.IP(), c.Get("User-Agent"), meta)
	}
}

// --- Route Registration ---

// RegisterSelfServiceRoutes registers user self-service routes.
func (h *UserHandler) RegisterSelfServiceRoutes(api fiber.Router, authMw fiber.Handler) {
	users := api.Group("/users")
	users.Use(authMw)

	users.Get("/profile", h.GetProfile)
	users.Put("/profile", h.UpdateProfile)
	users.Delete("/profile", h.DeleteAccount)
	users.Put("/change-password", h.ChangePassword)
	users.Get("/sessions", h.GetSessions)
	users.Delete("/sessions", h.RevokeAllSessions)
	users.Delete("/sessions/:id", h.RevokeSession)
	users.Get("/audit-logs", h.GetMyAuditLogs)
}

// RegisterAdminRoutes registers admin user management routes on an existing admin group.
func (h *UserHandler) RegisterAdminRoutes(admin fiber.Router) {
	admin.Get("/users", h.AdminListUsers)
	admin.Post("/users", h.AdminCreateUser)
	admin.Get("/users/:id", h.AdminGetUser)
	admin.Put("/users/:id", h.AdminUpdateUser)
	admin.Delete("/users/:id", h.AdminDeleteUser)
	admin.Put("/users/:id/status", h.AdminUpdateStatus)
	admin.Post("/users/:id/roles", h.AdminAssignRole)
	admin.Delete("/users/:id/roles/:roleId", h.AdminRemoveRole)
	admin.Post("/users/:id/unlock", h.AdminUnlockUser)
	admin.Post("/users/:id/reset-password", h.AdminResetPassword)
	admin.Post("/users/:id/disable-2fa", h.AdminDisable2FA)
	admin.Get("/audit-logs", h.AdminListAuditLogs)
}

// --- Self-Service Handlers ---

// GetProfile returns the authenticated user's profile.
// @Summary      Get current user profile
// @Description  Returns the authenticated user's profile information
// @Tags         Users
// @Produce      json
// @Security     Bearer
// @Success      200 {object} domain.User
// @Failure      401 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /users/profile [get]
func (h *UserHandler) GetProfile(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	user, err := h.userService.AdminGetUser(claims.UserID)
	if err != nil {
		return err
	}

	return c.JSON(user)
}

// UpdateProfile updates the authenticated user's profile.
// @Summary      Update current user profile
// @Description  Updates the authenticated user's profile fields
// @Tags         Users
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body UpdateProfileRequest true "Profile update data"
// @Success      200 {object} ListAuditLogsResponse
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Router       /users/profile [put]
func (h *UserHandler) UpdateProfile(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	var req UpdateProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	user, err := h.userService.UpdateProfile(claims.UserID, req.FirstName, req.LastName, req.Phone, req.Metadata)
	if err != nil {
		return err
	}

	h.audit(c, &claims.UserID, service.ActionProfileUpdate, claims.UserID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "Profile updated successfully",
		"user":    user,
	})
}

// DeleteAccount soft-deletes the authenticated user's account.
// @Summary      Delete current user account
// @Description  Soft-deletes the authenticated user's account and revokes all sessions
// @Tags         Users
// @Produce      json
// @Security     Bearer
// @Success      200 {object} ListUsersResponse
// @Failure      401 {object} errors.ProblemDetail
// @Router       /users/profile [delete]
func (h *UserHandler) DeleteAccount(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	if err := h.userService.DeleteAccount(claims.UserID); err != nil {
		return err
	}

	h.audit(c, &claims.UserID, service.ActionAccountDelete, claims.UserID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "Account deleted successfully",
	})
}

// ChangePassword changes the authenticated user's password.
// @Summary      Change password
// @Description  Changes the authenticated user's password (requires current password)
// @Tags         Users
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body ChangePasswordRequest true "Password change data"
// @Success      200 {object} ListAuditLogsResponse
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Router       /users/change-password [put]
func (h *UserHandler) ChangePassword(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	var req ChangePasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	if err := h.authService.ChangePassword(claims.UserID, req.OldPassword, req.NewPassword); err != nil {
		return err
	}

	h.audit(c, &claims.UserID, service.ActionPasswordChange, claims.UserID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "Password changed successfully",
	})
}

// GetSessions returns the authenticated user's active sessions.
// @Summary      List active sessions
// @Description  Returns the authenticated user's active sessions
// @Tags         Users
// @Produce      json
// @Security     Bearer
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Router       /users/sessions [get]
func (h *UserHandler) GetSessions(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	sessions, err := h.userService.GetSessions(claims.UserID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"sessions": sessions,
	})
}

// RevokeAllSessions revokes all of the authenticated user's sessions.
// @Summary      Revoke all sessions
// @Description  Revokes all active sessions for the authenticated user
// @Tags         Users
// @Produce      json
// @Security     Bearer
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Router       /users/sessions [delete]
func (h *UserHandler) RevokeAllSessions(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	if err := h.userService.RevokeAllSessions(claims.UserID); err != nil {
		return err
	}

	h.audit(c, &claims.UserID, service.ActionSessionRevokeAll, claims.UserID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "All sessions revoked successfully",
	})
}

// RevokeSession revokes a single session.
// @Summary      Revoke a session
// @Description  Revokes a specific session by ID
// @Tags         Users
// @Produce      json
// @Security     Bearer
// @Param        id path string true "Session ID"
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /users/sessions/{id} [delete]
func (h *UserHandler) RevokeSession(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	sessionID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid session ID")
	}

	if err := h.userService.RevokeSession(claims.UserID, sessionID); err != nil {
		return err
	}

	h.audit(c, &claims.UserID, service.ActionSessionRevoke, sessionID.String(), nil)
	return c.JSON(fiber.Map{
		"message": "Session revoked successfully",
	})
}

// GetMyAuditLogs returns the authenticated user's audit logs.
// @Summary      Get my audit logs
// @Description  Returns the authenticated user's audit logs with pagination
// @Tags         Users
// @Produce      json
// @Security     Bearer
// @Param        page  query int false "Page number" default(1)
// @Param        limit query int false "Items per page" default(20)
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Router       /users/audit-logs [get]
func (h *UserHandler) GetMyAuditLogs(c *fiber.Ctx) error {
	claims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	logs, total, err := h.auditService.GetUserLogsWithTotal(claims.UserID, offset, limit)
	if err != nil {
		return errors.NewInternalError("Failed to fetch audit logs")
	}

	return c.JSON(apiresponse.NewPaginatedResponse(logs, page, limit, total))
}

// --- Admin Handlers ---

// AdminListUsers lists all users with filtering and pagination.
// @Summary      List all users
// @Description  Returns a paginated list of users with optional filtering. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        page        query int    false "Page number"    default(1)
// @Param        limit       query int    false "Items per page" default(10)
// @Param        sort_by     query string false "Sort field"
// @Param        order       query string false "Sort order (asc/desc)"
// @Param        search      query string false "Search term"
// @Param        only_active query bool   false "Only active users"
// @Param        roles       query string false "Comma-separated role names"
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/users [get]
func (h *UserHandler) AdminListUsers(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	offset := (page - 1) * limit

	filter := repository.UserListFilter{
		Offset:     offset,
		Limit:      limit,
		SortBy:     c.Query("sort_by"),
		Order:      c.Query("order"),
		Search:     c.Query("search"),
		OnlyActive: c.Query("only_active") == "true",
	}
	if rolesParam := c.Query("roles"); rolesParam != "" {
		filter.Roles = strings.Split(rolesParam, ",")
	}

	users, total, err := h.userService.AdminListUsers(filter)
	if err != nil {
		return err
	}

	return c.JSON(apiresponse.NewPaginatedResponse(users, page, limit, total))
}

// AdminGetUser returns a single user's details.
// @Summary      Get user details
// @Description  Returns a single user's details with roles. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id path string true "User ID"
// @Success      200 {object} domain.User
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id} [get]
func (h *UserHandler) AdminGetUser(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	user, err := h.userService.AdminGetUser(id)
	if err != nil {
		return err
	}

	return c.JSON(user)
}

// AdminCreateUser creates a new user.
// @Summary      Create user
// @Description  Creates a new user account. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body AdminCreateUserRequest true "User creation data"
// @Success      201 {object} fiber.Map
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      409 {object} errors.ProblemDetail
// @Router       /admin/users [post]
func (h *UserHandler) AdminCreateUser(c *fiber.Ctx) error {
	var req AdminCreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	user, err := h.userService.AdminCreateUser(req.Email, req.Username, req.Password, req.FirstName, req.LastName, req.Phone, req.Verified)
	if err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminCreateUser, user.ID.String(), map[string]interface{}{
			"email": req.Email,
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "User created successfully",
		"user":    user,
	})
}

// AdminUpdateUser updates a user's profile.
// @Summary      Update user
// @Description  Updates a user's profile fields. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        id      path string                true "User ID"
// @Param        request body AdminUpdateUserRequest true "User update data"
// @Success      200 {object} fiber.Map
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Failure      409 {object} errors.ProblemDetail
// @Router       /admin/users/{id} [put]
func (h *UserHandler) AdminUpdateUser(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	var req AdminUpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	user, err := h.userService.AdminUpdateUser(id, req.Email, req.Username, req.FirstName, req.LastName, req.Phone, req.Metadata)
	if err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminUpdateUser, id.String(), nil)
	}

	return c.JSON(fiber.Map{
		"message": "User updated successfully",
		"user":    user,
	})
}

// AdminDeleteUser soft-deletes a user.
// @Summary      Delete user
// @Description  Soft-deletes a user account. Requires admin role. Admin cannot delete themselves.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id path string true "User ID"
// @Success      200 {object} fiber.Map
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id} [delete]
func (h *UserHandler) AdminDeleteUser(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	adminClaims, err := GetUserFromContext(c)
	if err != nil {
		return err
	}

	if err := h.userService.AdminDeleteUser(id, adminClaims.UserID); err != nil {
		return err
	}

	h.audit(c, &adminClaims.UserID, service.ActionAdminDeleteUser, id.String(), nil)
	return c.JSON(fiber.Map{
		"message": "User deleted successfully",
	})
}

// AdminUpdateStatus changes a user's status.
// @Summary      Update user status
// @Description  Changes a user's status (active/inactive/locked). Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        id      path string              true "User ID"
// @Param        request body UpdateStatusRequest  true "New status"
// @Success      200 {object} fiber.Map
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id}/status [put]
func (h *UserHandler) AdminUpdateStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	var req UpdateStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	user, err := h.userService.AdminUpdateStatus(id, req.Status)
	if err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminStatusChange, id.String(), map[string]interface{}{
			"new_status": req.Status,
		})
	}

	return c.JSON(fiber.Map{
		"message": "User status updated successfully",
		"user":    user,
	})
}

// AdminAssignRole assigns a role to a user.
// @Summary      Assign role to user
// @Description  Assigns a role to a user. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        id      path string            true "User ID"
// @Param        request body AssignRoleRequest  true "Role ID"
// @Success      200 {object} fiber.Map
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id}/roles [post]
func (h *UserHandler) AdminAssignRole(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	var req AssignRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}
	if err := validation.Struct(req); err != nil {
		return err
	}

	if err := h.userService.AdminAssignRole(id, req.RoleID); err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminRoleAssign, id.String(), map[string]interface{}{
			"role_id": req.RoleID.String(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Role assigned successfully",
	})
}

// AdminRemoveRole removes a role from a user.
// @Summary      Remove role from user
// @Description  Removes a role from a user. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id     path string true "User ID"
// @Param        roleId path string true "Role ID"
// @Success      200 {object} fiber.Map
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id}/roles/{roleId} [delete]
func (h *UserHandler) AdminRemoveRole(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	roleID, err := uuid.Parse(c.Params("roleId"))
	if err != nil {
		return errors.NewBadRequest("Invalid role ID")
	}

	if err := h.userService.AdminRemoveRole(id, roleID); err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminRoleRemove, id.String(), map[string]interface{}{
			"role_id": roleID.String(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Role removed successfully",
	})
}

// AdminUnlockUser unlocks a locked user account.
// @Summary      Unlock user account
// @Description  Unlocks a locked user account and resets failed login attempts. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id path string true "User ID"
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id}/unlock [post]
func (h *UserHandler) AdminUnlockUser(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	if err := h.userService.AdminUnlockUser(id); err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminUnlockUser, id.String(), nil)
	}

	return c.JSON(fiber.Map{
		"message": "User account unlocked successfully",
	})
}

// AdminResetPassword sends a password reset email for a user.
// @Summary      Reset user password
// @Description  Sends a password reset email for the specified user. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id path string true "User ID"
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id}/reset-password [post]
func (h *UserHandler) AdminResetPassword(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	if err := h.userService.AdminResetPassword(id); err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminResetPassword, id.String(), nil)
	}

	return c.JSON(fiber.Map{
		"message": "Password reset email sent successfully",
	})
}

// AdminDisable2FA force-disables 2FA for a user.
// @Summary      Disable user 2FA
// @Description  Force-disables two-factor authentication for a user. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id path string true "User ID"
// @Success      200 {object} fiber.Map
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      404 {object} errors.ProblemDetail
// @Router       /admin/users/{id}/disable-2fa [post]
func (h *UserHandler) AdminDisable2FA(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errors.NewBadRequest("Invalid user ID")
	}

	if err := h.userService.AdminDisable2FA(id); err != nil {
		return err
	}

	adminClaims, _ := GetUserFromContext(c)
	if adminClaims != nil {
		h.audit(c, &adminClaims.UserID, service.ActionAdminDisable2FA, id.String(), nil)
	}

	return c.JSON(fiber.Map{
		"message": "Two-factor authentication disabled successfully",
	})
}

// AdminListAuditLogs returns all audit logs with filtering.
// @Summary      List all audit logs
// @Description  Returns all audit logs with optional filtering. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        page        query int    false "Page number"    default(1)
// @Param        limit       query int    false "Items per page" default(20)
// @Param        user_id     query string false "Filter by user ID"
// @Param        action      query string false "Filter by action"
// @Param        resource    query string false "Filter by resource"
// @Param        resource_id query string false "Filter by resource ID"
// @Success      200 {object} fiber.Map
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/audit-logs [get]
func (h *UserHandler) AdminListAuditLogs(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	filter := repository.AuditLogListFilter{
		Action:     c.Query("action"),
		Resource:   c.Query("resource"),
		ResourceID: c.Query("resource_id"),
		Offset:     offset,
		Limit:      limit,
	}

	if userIDStr := c.Query("user_id"); userIDStr != "" {
		uid, err := uuid.Parse(userIDStr)
		if err != nil {
			return errors.NewBadRequest("Invalid user_id parameter")
		}
		filter.UserID = &uid
	}

	logs, total, err := h.auditService.ListAllLogs(filter)
	if err != nil {
		return errors.NewInternalError("Failed to fetch audit logs")
	}

	return c.JSON(apiresponse.NewPaginatedResponse(logs, page, limit, total))
}

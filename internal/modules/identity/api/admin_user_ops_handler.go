package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	helpers "github.com/mr-kaynak/go-core/internal/api/helpers"
	apiresponse "github.com/mr-kaynak/go-core/internal/api/response"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
)

// AdminUserOpsHandler handles admin session, API key, and bulk/export user operations.
type AdminUserOpsHandler struct {
	adminService  *service.AdminService
	apiKeyService *service.APIKeyService
	userService   *service.UserService
	auditService  *service.AuditService
	logger        *logger.Logger
}

// --- Bulk User Operation DTOs ---

// BulkUpdateStatusRequest is the request body for bulk user status update.
type BulkUpdateStatusRequest struct {
	UserIDs []uuid.UUID `json:"user_ids"`
	Status  string      `json:"status"`
}

// BulkAssignRoleRequest is the request body for bulk user role assignment.
type BulkAssignRoleRequest struct {
	UserIDs []uuid.UUID `json:"user_ids"`
	RoleID  uuid.UUID   `json:"role_id"`
}

// BulkOperationResult represents the result of a bulk operation.
type BulkOperationResult struct {
	SuccessCount int                  `json:"success_count"`
	FailureCount int                  `json:"failure_count"`
	Failures     []BulkOperationError `json:"failures,omitempty"`
}

// BulkOperationError represents a single failure in a bulk operation.
type BulkOperationError struct {
	UserID uuid.UUID `json:"user_id"`
	Error  string    `json:"error"`
}

// audit logs an admin action to the audit service.
func (h *AdminUserOpsHandler) audit(c fiber.Ctx, action, resource, resourceID string, meta map[string]interface{}) {
	if h.auditService != nil {
		userID := fiber.Locals[uuid.UUID](c, "userID")
		h.auditService.LogAction(c.Context(), &userID, action, resource, resourceID, c.IP(), c.UserAgent(), meta)
	}
}

// --- API Key Management Handlers ---

// ListAllAPIKeys returns all API keys paginated, with hash stripped from the response.
// @Summary      List all API keys
// @Description  Returns a paginated list of all API keys with sensitive hash stripped. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        page  query int false "Page number"    default(1)
// @Param        limit query int false "Items per page" default(20)
// @Success      200 {object} apiresponse.PaginatedResponse[APIKeySafeResponse]
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/api-keys [get]
func (h *AdminUserOpsHandler) ListAllAPIKeys(c fiber.Ctx) error {
	page, limit, offset := helpers.ParsePagination(c, 20)

	keys, total, err := h.apiKeyService.ListAll(c.Context(), offset, limit)
	if err != nil {
		return err
	}

	safeKeys := make([]APIKeySafeResponse, 0, len(keys))
	for _, key := range keys {
		safeKeys = append(safeKeys, toAPIKeySafeResponse(key))
	}

	return c.JSON(apiresponse.NewPaginatedResponse(safeKeys, page, limit, total))
}

// RevokeAPIKey revokes an API key by ID and logs an audit event.
// @Summary      Revoke an API key
// @Description  Revokes an API key by its ID. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        id path string true "API Key UUID"
// @Success      200 {object} MessageResponse
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/api-keys/{id} [delete]
func (h *AdminUserOpsHandler) RevokeAPIKey(c fiber.Ctx) error {
	keyID, err := helpers.ParseUUIDParam(c, "id", "Invalid API key ID format")
	if err != nil {
		return err
	}

	if err := h.apiKeyService.AdminRevoke(c.Context(), keyID); err != nil {
		return err
	}

	h.audit(c, service.ActionAPIKeyRevoked, "api_key", keyID.String(), nil)

	return c.JSON(fiber.Map{
		"message": "API key revoked successfully",
	})
}

// --- Session Management Handlers ---

// ListActiveSessions returns all active sessions paginated, with token values stripped from the response.
// @Summary      List active sessions
// @Description  Returns a paginated list of all active sessions with token values stripped. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        page    query int    false "Page number"    default(1)
// @Param        limit   query int    false "Items per page" default(20)
// @Param        user_id query string false "Filter by user ID"
// @Success      200 {object} apiresponse.PaginatedResponse[SessionSafeResponse]
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/sessions [get]
func (h *AdminUserOpsHandler) ListActiveSessions(c fiber.Ctx) error {
	page, limit, offset := helpers.ParsePagination(c, 20)

	var userIDFilter *uuid.UUID
	if uidStr := c.Query("user_id"); uidStr != "" {
		uid, err := uuid.Parse(uidStr)
		if err != nil {
			return errors.NewBadRequest("invalid user_id format")
		}
		userIDFilter = &uid
	}

	tokens, total, err := h.adminService.ListActiveSessions(c.Context(), offset, limit, userIDFilter)
	if err != nil {
		return err
	}

	safeSessions := make([]SessionSafeResponse, 0, len(tokens))
	for _, token := range tokens {
		safeSessions = append(safeSessions, toSessionSafeResponse(token))
	}

	return c.JSON(apiresponse.NewPaginatedResponse(safeSessions, page, limit, total))
}

// ForceLogoutUser revokes all refresh tokens for a user and logs an audit event.
// @Summary      Force logout user
// @Description  Revokes all active sessions for a specific user. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        userId path string true "User UUID"
// @Success      200 {object} MessageResponse
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/sessions/user/{userId} [delete]
func (h *AdminUserOpsHandler) ForceLogoutUser(c fiber.Ctx) error {
	userID, err := helpers.ParseUUIDParam(c, "userId", "Invalid user ID format")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c, 3*time.Second)
	defer cancel()

	if err := h.adminService.ForceLogoutUser(ctx, userID); err != nil {
		return err
	}

	h.audit(c, service.ActionAdminSessionRevokeAll, "user", userID.String(), map[string]interface{}{
		"target_user_id": userID.String(),
	})

	return c.JSON(fiber.Map{
		"message": "All sessions revoked successfully",
	})
}

// --- User Export & Bulk Operations ---

// ExportUsers exports all users in JSON or CSV format.
// @Summary      Export users
// @Description  Exports all users as a downloadable JSON or CSV file. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        format query string false "Export format (json or csv)" default(json)
// @Success      200 {file} file "JSON or CSV file download"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      500 {object} errors.ProblemDetail
// @Router       /admin/users/export [get]
func (h *AdminUserOpsHandler) ExportUsers(c fiber.Ctx) error {
	format := c.Query("format", "json")
	if format != "json" && format != "csv" {
		return errors.NewBadRequest("Invalid format. Supported formats: json, csv")
	}

	filter := domain.UserListFilter{
		Offset: 0,
		Limit:  maxExportLimit,
	}
	users, _, err := h.userService.AdminListUsers(c, filter)
	if err != nil {
		return err
	}

	if format == "csv" {
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)

		header := []string{"id", "email", "username", "first_name", "last_name", "status", "created_at"}
		if err := writer.Write(header); err != nil {
			return errors.NewInternalError("Failed to write CSV header")
		}

		for _, u := range users {
			row := []string{
				u.ID.String(),
				u.Email,
				u.Username,
				u.FirstName,
				u.LastName,
				string(u.Status),
				u.CreatedAt.Format(time.RFC3339),
			}
			if err := writer.Write(row); err != nil {
				return errors.NewInternalError("Failed to write CSV row")
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return errors.NewInternalError("Failed to flush CSV writer")
		}

		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", "attachment; filename=users_export.csv")
		return c.Send(buf.Bytes())
	}

	// JSON format (default)
	data, err := json.Marshal(users)
	if err != nil {
		return errors.NewInternalError("Failed to marshal users to JSON")
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=users_export.json")
	return c.Send(data)
}

// BulkUpdateStatus updates the status of multiple users at once.
// Returns 200 if all succeed, 207 Multi-Status if partial or all fail, 400 for invalid input.
// @Summary      Bulk update user status
// @Description  Updates the status of multiple users at once. Returns 207 Multi-Status on partial failure. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body BulkUpdateStatusRequest true "User IDs and target status"
// @Success      200 {object} BulkOperationResult
// @Success      207 {object} BulkOperationResult "Partial failure"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/users/bulk-status [post]
func (h *AdminUserOpsHandler) BulkUpdateStatus(c fiber.Ctx) error {
	var req BulkUpdateStatusRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if len(req.UserIDs) == 0 {
		return errors.NewBadRequest("user_ids is required and cannot be empty")
	}
	if len(req.UserIDs) > maxBulkOperations {
		return errors.NewBadRequest("user_ids exceeds maximum batch size of 1000")
	}

	validStatuses := map[string]bool{"active": true, "inactive": true, "locked": true}
	if !validStatuses[req.Status] {
		return errors.NewBadRequest("Invalid status. Allowed values: active, inactive, locked")
	}

	result := BulkOperationResult{}
	for _, userID := range req.UserIDs {
		_, err := h.userService.AdminUpdateStatus(c, userID, req.Status)
		if err != nil {
			result.FailureCount++
			result.Failures = append(result.Failures, BulkOperationError{
				UserID: userID,
				Error:  err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	if result.FailureCount > 0 {
		return c.Status(http.StatusMultiStatus).JSON(result)
	}
	return c.JSON(result)
}

// BulkAssignRole assigns a role to multiple users at once.
// Returns 200 if all succeed, 207 Multi-Status if partial or all fail, 400 for invalid input.
// @Summary      Bulk assign role
// @Description  Assigns a role to multiple users at once. Returns 207 Multi-Status on partial failure. Requires admin role.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Param        request body BulkAssignRoleRequest true "User IDs and target role"
// @Success      200 {object} BulkOperationResult
// @Success      207 {object} BulkOperationResult "Partial failure"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Router       /admin/users/bulk-role [post]
func (h *AdminUserOpsHandler) BulkAssignRole(c fiber.Ctx) error {
	var req BulkAssignRoleRequest
	if err := c.Bind().Body(&req); err != nil {
		return errors.NewBadRequest("Invalid request body")
	}

	if len(req.UserIDs) == 0 {
		return errors.NewBadRequest("user_ids is required and cannot be empty")
	}
	if len(req.UserIDs) > maxBulkOperations {
		return errors.NewBadRequest("user_ids exceeds maximum batch size of 1000")
	}

	// Validate role exists before iterating over users
	if err := h.adminService.ValidateRoleExists(c.Context(), req.RoleID); err != nil {
		return errors.NewBadRequest("Invalid role_id: role not found")
	}

	callerRoles, _ := c.Locals("roles").([]string)
	result := BulkOperationResult{}
	for _, userID := range req.UserIDs {
		err := h.userService.AdminAssignRole(c.Context(), userID, req.RoleID, callerRoles)
		if err != nil {
			result.FailureCount++
			result.Failures = append(result.Failures, BulkOperationError{
				UserID: userID,
				Error:  err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	if result.FailureCount > 0 {
		return c.Status(http.StatusMultiStatus).JSON(result)
	}
	return c.JSON(result)
}

// ExportAuditLogs exports audit logs as a JSON file with Content-Disposition header.
// Supports filtering by start_date, end_date, action, and user_id query parameters.
// @Summary      Export audit logs
// @Description  Exports audit logs as a downloadable JSON file. Supports filtering by date range, action and user. Requires admin role.
// @Tags         Admin
// @Produce      json
// @Security     Bearer
// @Param        start_date query string false "Start date (YYYY-MM-DD)"
// @Param        end_date   query string false "End date (YYYY-MM-DD)"
// @Param        action     query string false "Filter by action type"
// @Param        user_id    query string false "Filter by user UUID"
// @Success      200 {file} file "JSON file download"
// @Failure      400 {object} errors.ProblemDetail
// @Failure      401 {object} errors.ProblemDetail
// @Failure      403 {object} errors.ProblemDetail
// @Failure      500 {object} errors.ProblemDetail
// @Router       /admin/audit-logs/export [get]
func (h *AdminUserOpsHandler) ExportAuditLogs(c fiber.Ctx) error {
	filter := domain.AuditLogListFilter{
		Action: c.Query("action"),
		Offset: 0,
		Limit:  maxExportLimit,
	}

	if userIDStr := c.Query("user_id"); userIDStr != "" {
		uid, err := uuid.Parse(userIDStr)
		if err != nil {
			return errors.NewBadRequest("Invalid user_id parameter")
		}
		filter.UserID = &uid
	}

	const dateLayout = "2006-01-02"

	if startDateStr := c.Query("start_date"); startDateStr != "" {
		t, err := time.Parse(dateLayout, startDateStr)
		if err != nil {
			return errors.NewBadRequest("Invalid start_date format, expected YYYY-MM-DD")
		}
		filter.StartDate = &t
	}

	if endDateStr := c.Query("end_date"); endDateStr != "" {
		t, err := time.Parse(dateLayout, endDateStr)
		if err != nil {
			return errors.NewBadRequest("Invalid end_date format, expected YYYY-MM-DD")
		}
		filter.EndDate = &t
	}

	logs, _, err := h.auditService.ListAllLogs(c.Context(), filter)
	if err != nil {
		return errors.NewInternalError("Failed to fetch audit logs for export")
	}

	data, err := json.Marshal(logs)
	if err != nil {
		return errors.NewInternalError("Failed to marshal audit logs to JSON")
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=audit_logs_export.json")
	return c.Send(data)
}

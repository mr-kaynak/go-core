package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// Audit action type constants
const (
	ActionLogin              = "user.login"
	ActionLogout             = "user.logout"
	ActionFailedLogin        = "user.failed_login"
	ActionPasswordChange     = "user.password_change"
	ActionRoleChange         = "user.role_change"
	Action2FAEnable          = "user.2fa_enable"
	Action2FADisable         = "user.2fa_disable"
	ActionAPIKeyCreated      = "api_key.created"
	ActionAPIKeyRevoked      = "api_key.revoked"
	ActionAPIKeyRoleAssigned = "api_key.role_assigned"
	ActionAPIKeyRoleRemoved  = "api_key.role_removed"

	ActionProfileUpdate         = "user.profile_update"
	ActionAccountDelete         = "user.account_delete"
	ActionSessionRevoke         = "user.session_revoke"
	ActionSessionRevokeAll      = "user.session_revoke_all"
	ActionAdminCreateUser       = "admin.create_user"
	ActionAdminUpdateUser       = "admin.update_user"
	ActionAdminDeleteUser       = "admin.delete_user"
	ActionAdminStatusChange     = "admin.status_change"
	ActionAdminRoleAssign       = "admin.role_assign"
	ActionAdminRoleRemove       = "admin.role_remove"
	ActionAdminUnlockUser       = "admin.unlock_user"
	ActionAdminResetPassword    = "admin.reset_password"
	ActionAdminDisable2FA       = "admin.disable_2fa"
	ActionAdminSessionRevokeAll = "admin.session_revoke_all"

	// Auth actions
	ActionRegister             = "user.register"
	ActionEmailVerify          = "user.email_verify"
	ActionResendVerification   = "user.resend_verification"
	ActionPasswordResetRequest = "user.password_reset_request"
	ActionTokenRefresh         = "user.token_refresh"

	// Role management actions
	ActionRoleCreate          = "role.create"
	ActionRoleUpdate          = "role.update"
	ActionRoleDelete          = "role.delete"
	ActionRoleHierarchySet    = "role.hierarchy_set"
	ActionRoleHierarchyRemove = "role.hierarchy_remove"

	// Permission management actions
	ActionPermissionCreate         = "permission.create"
	ActionPermissionUpdate         = "permission.update"
	ActionPermissionDelete         = "permission.delete"
	ActionPermissionAddToRole      = "permission.add_to_role"
	ActionPermissionRemoveFromRole = "permission.remove_from_role"

	// Policy management actions
	ActionPolicyAdd            = "policy.add"
	ActionPolicyRemove         = "policy.remove"
	ActionPolicyUserRoleAdd    = "policy.user_role_add"
	ActionPolicyUserRoleRemove = "policy.user_role_remove"
)

// AuditLogHook is a callback invoked after an audit log entry is persisted.
type AuditLogHook func(
	id uuid.UUID, userID *uuid.UUID,
	action, resource, resourceID, ipAddress, userAgent string,
	metadata map[string]interface{}, createdAt time.Time,
)

// AuditService handles audit log operations
type AuditService struct {
	auditRepo    repository.AuditLogRepository
	logger       *logger.Logger
	onLogCreated AuditLogHook
}

// NewAuditService creates a new audit service
func NewAuditService(auditRepo repository.AuditLogRepository) *AuditService {
	return &AuditService{
		auditRepo: auditRepo,
		logger:    logger.Get().WithFields(logger.Fields{"service": "audit"}),
	}
}

// SetOnLogCreated registers a callback that is invoked (in a goroutine) after
// every successful audit log write.
func (s *AuditService) SetOnLogCreated(hook AuditLogHook) {
	s.onLogCreated = hook
}

// LogAction creates a new audit log entry
func (s *AuditService) LogAction(
	userID *uuid.UUID, action, resource, resourceID, ipAddress, userAgent string, metadata map[string]interface{},
) {
	metadataStr := "{}"
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to marshal audit metadata")
		} else {
			metadataStr = string(data)
		}
	}

	auditLog := &domain.AuditLog{
		ID:         uuid.New(),
		UserID:     userID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IPAddress:  ipAddress,
		UserAgent:  userAgent,
		Metadata:   metadataStr,
		CreatedAt:  time.Now(),
	}

	if err := s.auditRepo.Create(auditLog); err != nil {
		s.logger.WithError(err).Error("Failed to create audit log",
			"action", action,
			"resource", resource,
			"resource_id", resourceID,
		)
		return
	}

	if s.onLogCreated != nil {
		hook := s.onLogCreated
		go hook(auditLog.ID, auditLog.UserID, action, resource, resourceID, ipAddress, userAgent, metadata, auditLog.CreatedAt)
	}

	s.logger.Debug("Audit log created",
		"action", action,
		"resource", resource,
		"resource_id", resourceID,
	)
}

// GetUserLogs retrieves audit logs for a specific user
func (s *AuditService) GetUserLogs(userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, error) {
	return s.auditRepo.GetByUser(userID, offset, limit)
}

// GetUserLogsWithTotal retrieves audit logs for a specific user with total count.
func (s *AuditService) GetUserLogsWithTotal(userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, int64, error) {
	filter := repository.AuditLogListFilter{
		UserID: &userID,
		Offset: offset,
		Limit:  limit,
	}
	return s.auditRepo.ListAll(filter)
}

// GetActionLogs retrieves audit logs by action type
func (s *AuditService) GetActionLogs(action string, offset, limit int) ([]*domain.AuditLog, error) {
	return s.auditRepo.GetByAction(action, offset, limit)
}

// GetResourceLogs retrieves audit logs by resource type
func (s *AuditService) GetResourceLogs(resource, resourceID string, offset, limit int) ([]*domain.AuditLog, error) {
	return s.auditRepo.GetByResource(resource, resourceID, offset, limit)
}

// ListAllLogs retrieves audit logs matching the given filter with total count
func (s *AuditService) ListAllLogs(filter repository.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
	return s.auditRepo.ListAll(filter)
}

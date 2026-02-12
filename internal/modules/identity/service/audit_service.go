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
	ActionLogin          = "user.login"
	ActionLogout         = "user.logout"
	ActionFailedLogin    = "user.failed_login"
	ActionPasswordChange = "user.password_change"
	ActionRoleChange     = "user.role_change"
	Action2FAEnable      = "user.2fa_enable"
	Action2FADisable     = "user.2fa_disable"
	ActionAPIKeyCreated  = "api_key.created"
	ActionAPIKeyRevoked  = "api_key.revoked"
)

// AuditService handles audit log operations
type AuditService struct {
	auditRepo repository.AuditLogRepository
	logger    *logger.Logger
}

// NewAuditService creates a new audit service
func NewAuditService(auditRepo repository.AuditLogRepository) *AuditService {
	return &AuditService{
		auditRepo: auditRepo,
		logger:    logger.Get().WithFields(logger.Fields{"service": "audit"}),
	}
}

// LogAction creates a new audit log entry
func (s *AuditService) LogAction(
	userID *uuid.UUID, action, resource, resourceID, ipAddress, userAgent string, metadata map[string]interface{},
) {
	var metadataStr string
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to marshal audit metadata")
			metadataStr = "{}"
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

// GetActionLogs retrieves audit logs by action type
func (s *AuditService) GetActionLogs(action string, offset, limit int) ([]*domain.AuditLog, error) {
	return s.auditRepo.GetByAction(action, offset, limit)
}

// GetResourceLogs retrieves audit logs by resource type
func (s *AuditService) GetResourceLogs(resource, resourceID string, offset, limit int) ([]*domain.AuditLog, error) {
	return s.auditRepo.GetByResource(resource, resourceID, offset, limit)
}

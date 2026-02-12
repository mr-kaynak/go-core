package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// AuditLogRepository defines the interface for audit log data operations
type AuditLogRepository interface {
	// Create creates a new audit log entry
	Create(log *domain.AuditLog) error

	// GetByUser retrieves audit logs for a specific user with pagination
	GetByUser(userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, error)

	// GetByAction retrieves audit logs by action type with pagination
	GetByAction(action string, offset, limit int) ([]*domain.AuditLog, error)

	// GetByResource retrieves audit logs by resource type and optional resource ID
	GetByResource(resource string, resourceID string, offset, limit int) ([]*domain.AuditLog, error)
}

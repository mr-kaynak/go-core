package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// AuditLogRepository defines the interface for audit log data operations
type AuditLogRepository interface {
	// Create creates a new audit log entry
	Create(ctx context.Context, log *domain.AuditLog) error

	// GetByUser retrieves audit logs for a specific user with pagination
	GetByUser(ctx context.Context, userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, error)

	// GetByAction retrieves audit logs by action type with pagination
	GetByAction(ctx context.Context, action string, offset, limit int) ([]*domain.AuditLog, error)

	// GetByResource retrieves audit logs by resource type and optional resource ID
	GetByResource(ctx context.Context, resource string, resourceID string, offset, limit int) ([]*domain.AuditLog, error)

	// ListAll retrieves audit logs matching the given filter with total count
	ListAll(ctx context.Context, filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error)
}

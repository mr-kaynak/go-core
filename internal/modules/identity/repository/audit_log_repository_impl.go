package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// auditLogRepositoryImpl implements AuditLogRepository using GORM
type auditLogRepositoryImpl struct {
	db *gorm.DB
}

// NewAuditLogRepository creates a new audit log repository
func NewAuditLogRepository(db *gorm.DB) AuditLogRepository {
	return &auditLogRepositoryImpl{
		db: db,
	}
}

// Create creates a new audit log entry
func (r *auditLogRepositoryImpl) Create(log *domain.AuditLog) error {
	return r.db.Create(log).Error
}

// GetByUser retrieves audit logs for a specific user with pagination
func (r *auditLogRepositoryImpl) GetByUser(userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, error) {
	limit = clampLimit(limit)
	var logs []*domain.AuditLog
	err := r.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// GetByAction retrieves audit logs by action type with pagination
func (r *auditLogRepositoryImpl) GetByAction(action string, offset, limit int) ([]*domain.AuditLog, error) {
	limit = clampLimit(limit)
	var logs []*domain.AuditLog
	err := r.db.Where("action = ?", action).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// GetByResource retrieves audit logs by resource type and optional resource ID
func (r *auditLogRepositoryImpl) GetByResource(resource string, resourceID string, offset, limit int) ([]*domain.AuditLog, error) {
	limit = clampLimit(limit)
	var logs []*domain.AuditLog
	query := r.db.Where("resource = ?", resource)
	if resourceID != "" {
		query = query.Where("resource_id = ?", resourceID)
	}
	err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// ListAll retrieves audit logs matching the given filter with total count
func (r *auditLogRepositoryImpl) ListAll(filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
	query := r.db.Model(&domain.AuditLog{})

	if filter.UserID != nil {
		query = query.Where("user_id = ?", *filter.UserID)
	}
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	if filter.Resource != "" {
		query = query.Where("resource = ?", filter.Resource)
	}
	if filter.ResourceID != "" {
		query = query.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.StartDate != nil {
		query = query.Where("created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("created_at <= ?", *filter.EndDate)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []*domain.AuditLog
	err := query.Order("created_at DESC").
		Offset(filter.Offset).
		Limit(clampLimit(filter.Limit)).
		Find(&logs).Error
	return logs, total, err
}

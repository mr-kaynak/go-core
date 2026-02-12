package domain

import (
	"time"

	"github.com/google/uuid"
)

// AuditLog represents an audit trail entry for tracking user actions
type AuditLog struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID     *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	Action     string     `gorm:"not null;index" json:"action"`
	Resource   string     `gorm:"not null" json:"resource"`
	ResourceID string     `json:"resource_id,omitempty"`
	IPAddress  string     `json:"ip_address,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	Metadata   string     `gorm:"type:jsonb" json:"metadata,omitempty"`
	CreatedAt  time.Time  `gorm:"index" json:"created_at"`
}

// TableName specifies the table name for AuditLog
func (AuditLog) TableName() string { return "audit_logs" }

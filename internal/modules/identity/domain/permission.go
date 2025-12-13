package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Permission represents a permission in the system
type Permission struct {
	ID          uuid.UUID      `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Name        string         `gorm:"uniqueIndex;not null" json:"name"`        // e.g., "user.create", "user.delete", "role.manage"
	Description string         `json:"description"`                              // Human-readable description
	Category    string         `gorm:"index" json:"category"`                    // e.g., "user", "role", "dashboard"
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"type:timestamp;index" json:"-"`

	// Relations
	Roles []Role `gorm:"many2many:role_permissions;joinForeignKey:permission_id;joinReferences:role_id" json:"-"`
}

// TableName specifies table name for Permission model
func (Permission) TableName() string {
	return "permissions"
}

// RolePermission is the join table between roles and permissions
type RolePermission struct {
	RoleID       uuid.UUID `gorm:"primaryKey;type:uuid" json:"role_id"`
	PermissionID uuid.UUID `gorm:"primaryKey;type:uuid" json:"permission_id"`
	CreatedAt    time.Time `gorm:"autoCreateTime:milli" json:"created_at"`

	// Relations
	Role       Role       `gorm:"foreignKey:RoleID;references:ID;onDelete:CASCADE" json:"-"`
	Permission Permission `gorm:"foreignKey:PermissionID;references:ID;onDelete:CASCADE" json:"-"`
}

// TableName specifies table name for RolePermission model
func (RolePermission) TableName() string {
	return "role_permissions"
}

// PermissionResponse represents a permission in API responses
type PermissionResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ToResponse converts Permission to PermissionResponse
func (p *Permission) ToResponse() *PermissionResponse {
	return &PermissionResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Category:    p.Category,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

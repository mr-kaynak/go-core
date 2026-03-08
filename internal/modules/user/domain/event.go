package domain

import (
	"time"

	"github.com/google/uuid"
)

// UserEvent represents an event related to a user
type UserEvent struct {
	ID        uuid.UUID         `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Type      string            `json:"type" gorm:"not null;index"`
	UserID    uuid.UUID         `json:"user_id" gorm:"type:uuid;not null;index"`
	Timestamp time.Time         `json:"timestamp" gorm:"not null;index"`
	Data      map[string]string `json:"data" gorm:"type:jsonb"`
}

// Common user event types
const (
	EventUserCreated           = "user.created"
	EventUserUpdated           = "user.updated"
	EventUserDeleted           = "user.deleted"
	EventUserVerified          = "user.verified"
	EventUserDeactivated       = "user.deactivated"
	EventUserReactivated       = "user.reactivated"
	EventUserPasswordChanged   = "user.password.changed"
	EventUserPasswordReset     = "user.password.reset"
	EventUserLoginSuccess      = "user.login.success"
	EventUserLoginFailed       = "user.login.failed"
	EventUserLogout            = "user.logout"
	EventUserRoleChanged       = "user.role.changed"
	EventUserPermissionAdded   = "user.permission.added"
	EventUserPermissionRemoved = "user.permission.removed"
)

// NewUserEvent creates a new user event
func NewUserEvent(eventType string, userID uuid.UUID, data map[string]string) *UserEvent {
	return &UserEvent{
		ID:        uuid.New(),
		Type:      eventType,
		UserID:    userID,
		Timestamp: time.Now(),
		Data:      data,
	}
}

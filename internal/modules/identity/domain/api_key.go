package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// APIKey represents an API key for programmatic access
type APIKey struct {
	ID         uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	KeyHash    string         `gorm:"uniqueIndex;not null" json:"-"`
	KeyPrefix  string         `gorm:"size:8;not null" json:"key_prefix"`
	Name       string         `gorm:"not null" json:"name"`
	Scopes     string         `gorm:"type:text" json:"scopes"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	LastUsedAt *time.Time     `json:"last_used_at,omitempty"`
	Revoked    bool           `gorm:"default:false" json:"revoked"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for APIKey
func (APIKey) TableName() string { return "api_keys" }

// IsValid checks if the API key is still valid (not revoked and not expired)
func (a *APIKey) IsValid() bool {
	if a.Revoked {
		return false
	}
	if a.ExpiresAt != nil && time.Now().After(*a.ExpiresAt) {
		return false
	}
	return true
}

// HashAPIKey creates a SHA256 hash of an API key string
func HashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

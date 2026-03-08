package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/crypto"
	"gorm.io/gorm"
)

// APIKey represents an API key for programmatic access
type APIKey struct {
	ID         uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	KeyHash    string         `gorm:"uniqueIndex;not null" json:"-"`
	KeyPrefix  string         `gorm:"size:8;not null" json:"key_prefix"`
	Name       string         `gorm:"not null" json:"name"`
	Scopes     string         `gorm:"type:text" json:"scopes"` // Deprecated: use Roles instead
	Roles      []Role         `gorm:"many2many:api_key_roles;" json:"roles,omitempty"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	LastUsedAt *time.Time     `json:"last_used_at,omitempty"`
	Revoked    bool           `gorm:"default:false" json:"revoked"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// APIKeyRole is the join table between API keys and roles
type APIKeyRole struct {
	APIKeyID  uuid.UUID `gorm:"primaryKey;type:uuid" json:"api_key_id"`
	RoleID    uuid.UUID `gorm:"primaryKey;type:uuid" json:"role_id"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName specifies the table name for APIKeyRole
func (APIKeyRole) TableName() string { return "api_key_roles" }

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

// GetRoleNames returns a list of role names assigned to this API key
func (a *APIKey) GetRoleNames() []string {
	names := make([]string, 0, len(a.Roles))
	for i := range a.Roles {
		names = append(names, a.Roles[i].Name)
	}
	return names
}

// GetPermissionNames returns deduplicated permission names from all assigned roles
func (a *APIKey) GetPermissionNames() []string {
	var names []string
	seen := make(map[string]bool)
	for i := range a.Roles {
		for j := range a.Roles[i].Permissions {
			name := a.Roles[i].Permissions[j].Name
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	return names
}

// HashAPIKey creates a SHA256 hash of an API key string
func HashAPIKey(key string) string {
	return crypto.HashSHA256Hex(key)
}

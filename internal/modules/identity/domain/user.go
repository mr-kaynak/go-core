package domain

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserStatus represents the status of a user
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusInactive UserStatus = "inactive"
	UserStatusLocked   UserStatus = "locked"
	UserStatusPending  UserStatus = "pending"

	// bcryptHashLength is the standard length of a bcrypt hash string
	bcryptHashLength = 60
)

// Metadata is a custom type for storing JSON metadata in PostgreSQL JSONB
type Metadata map[string]interface{}

// Value implements the driver.Valuer interface for JSONB storage
func (m Metadata) Value() (driver.Value, error) {
	if m == nil {
		return json.Marshal(map[string]interface{}{})
	}
	return json.Marshal(m)
}

// Scan implements the sql.Scanner interface for JSONB retrieval
func (m *Metadata) Scan(value interface{}) error {
	if value == nil {
		*m = make(Metadata)
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		*m = make(Metadata)
		return nil
	}

	if err := json.Unmarshal(bytes, m); err != nil {
		*m = make(Metadata)
		return nil
	}

	return nil
}

// User represents a user in the system
type User struct {
	ID                   uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Email                string         `gorm:"uniqueIndex;not null" json:"email"`
	Username             string         `gorm:"uniqueIndex;not null" json:"username"`
	Password             string         `gorm:"not null" json:"-"`
	FirstName            string         `json:"first_name"`
	LastName             string         `json:"last_name"`
	Phone                string         `json:"phone"`
	AvatarURL            string         `gorm:"size:512" json:"avatar_url,omitempty"`
	Status               UserStatus     `gorm:"type:varchar(20);default:'pending'" json:"status"`
	Verified             bool           `gorm:"default:false" json:"verified"`
	IsVerified           bool           `gorm:"-" json:"is_verified"`
	Roles                []Role         `gorm:"many2many:user_roles;" json:"roles,omitempty"`
	LastLogin            *time.Time     `json:"last_login,omitempty"`
	LastLoginAt          *time.Time     `gorm:"-" json:"last_login_at,omitempty"`
	FailedLoginAttempts  int            `gorm:"default:0" json:"-"`
	LockedUntil          *time.Time     `json:"locked_until,omitempty"`
	TwoFactorSecret      string         `gorm:"size:256" json:"-"`
	TwoFactorEnabled     bool           `gorm:"default:false" json:"two_factor_enabled"`
	TwoFactorBackupCodes string         `gorm:"type:text" json:"-"`
	Metadata             Metadata       `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`

	// BCryptCost is set from config before hashing; not persisted.
	BCryptCost int `gorm:"-" json:"-"`
}

// Role represents a user role
type Role struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	Description string    `json:"description"`
	//nolint:lll // gorm many2many tag requires full specification
	Permissions []Permission   `gorm:"many2many:role_permissions;joinForeignKey:role_id;joinReferences:permission_id" json:"permissions,omitempty"`
	Users       []User         `gorm:"many2many:user_roles;" json:"-"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// RefreshToken represents a refresh token for a user
type RefreshToken struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	User      User      `gorm:"foreignKey:UserID" json:"-"`
	Token     string    `gorm:"uniqueIndex;not null" json:"token"`
	IPAddress string    `gorm:"size:45" json:"ip_address,omitempty"`
	UserAgent string    `gorm:"size:512" json:"user_agent,omitempty"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	Revoked   bool      `gorm:"default:false" json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName specifies the table name for User
func (User) TableName() string {
	return "users"
}

// TableName specifies the table name for Role
func (Role) TableName() string {
	return "roles"
}

// TableName specifies the table name for RefreshToken
func (RefreshToken) TableName() string {
	return "refresh_tokens"
}

// BeforeCreate hook for User
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}

	// Hash password if it's not already hashed
	if u.Password != "" && !u.IsPasswordHashed() {
		if err := u.HashPassword(); err != nil {
			return err
		}
	}

	// Sync alias fields
	u.syncAliasFields()

	return nil
}

// AfterFind hook for User
func (u *User) AfterFind(tx *gorm.DB) error {
	u.syncAliasFields()
	return nil
}

// syncAliasFields syncs alias fields
func (u *User) syncAliasFields() {
	u.IsVerified = u.Verified
	u.LastLoginAt = u.LastLogin
}

// SetPassword sets and hashes the user's password
func (u *User) SetPassword(password string) error {
	u.Password = password
	return u.HashPassword()
}

// HashPassword hashes the user's password using BCryptCost if set, otherwise bcrypt.DefaultCost.
func (u *User) HashPassword() error {
	cost := u.BCryptCost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(u.Password), cost)
	if err != nil {
		return err
	}
	u.Password = string(hashedPassword)
	return nil
}

// ComparePassword compares a password with the user's hashed password
func (u *User) ComparePassword(password string) error {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password))
}

// IsPasswordHashed checks if the password is already hashed
func (u *User) IsPasswordHashed() bool {
	// bcrypt hashes are 60 characters long and start with $2
	// Valid bcrypt hash format: $2a$cost$salt+hash (60 chars total)
	if len(u.Password) < 2 {
		return false
	}

	// Check for valid bcrypt hash format
	if len(u.Password) != bcryptHashLength {
		return false
	}

	// Check for bcrypt prefix ($2a, $2b, or $2x)
	return (u.Password[0] == '$' && u.Password[1] == '2' &&
		(u.Password[2] == 'a' || u.Password[2] == 'b' || u.Password[2] == 'x') &&
		u.Password[3] == '$')
}

// IsActive checks if the user is active
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive && u.Verified
}

// IsLocked checks if the user account is currently locked
func (u *User) IsLocked() bool {
	if u.LockedUntil == nil {
		return false
	}
	return !time.Now().After(*u.LockedUntil)
}

// IncrementFailedLogin increments the failed login counter
func (u *User) IncrementFailedLogin() {
	u.FailedLoginAttempts++
}

// ResetFailedLogin resets the failed login counter
func (u *User) ResetFailedLogin() {
	u.FailedLoginAttempts = 0
	u.LockedUntil = nil
}

// Lock locks the user account for the given duration
func (u *User) Lock(duration time.Duration) {
	t := time.Now().Add(duration)
	u.LockedUntil = &t
	u.Status = UserStatusLocked
}

// GetFullName returns the user's full name
func (u *User) GetFullName() string {
	if u.FirstName == "" && u.LastName == "" {
		return u.Username
	}
	if u.FirstName == "" {
		return u.LastName
	}
	if u.LastName == "" {
		return u.FirstName
	}
	return u.FirstName + " " + u.LastName
}

// HasRole checks if the user has a specific role
func (u *User) HasRole(roleName string) bool {
	for i := range u.Roles {
		if u.Roles[i].Name == roleName {
			return true
		}
	}
	return false
}

// GetPermissions returns all permissions for the user
func (u *User) GetPermissions() []Permission {
	var permissions []Permission
	seen := make(map[uuid.UUID]bool)

	for i := range u.Roles {
		for j := range u.Roles[i].Permissions {
			if !seen[u.Roles[i].Permissions[j].ID] {
				permissions = append(permissions, u.Roles[i].Permissions[j])
				seen[u.Roles[i].Permissions[j].ID] = true
			}
		}
	}

	return permissions
}

// HasPermission checks if the user has a specific permission by name
func (u *User) HasPermission(permissionName string) bool {
	for i := range u.Roles {
		for j := range u.Roles[i].Permissions {
			if u.Roles[i].Permissions[j].Name == permissionName {
				return true
			}
		}
	}
	return false
}

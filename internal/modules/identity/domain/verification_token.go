package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TokenType represents the type of verification token
type TokenType string

const (
	TokenTypeEmailVerification TokenType = "email_verification"
	TokenTypePasswordReset     TokenType = "password_reset"
	TokenTypePhoneVerification TokenType = "phone_verification"
	TokenTypeTwoFactor         TokenType = "two_factor"
)

// VerificationToken represents a verification token
type VerificationToken struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	User      User           `gorm:"foreignKey:UserID" json:"-"`
	Token     string         `gorm:"uniqueIndex;not null" json:"token"`
	Type      TokenType      `gorm:"type:varchar(30);not null" json:"type"`
	Used      bool           `gorm:"default:false" json:"used"`
	UsedAt    *time.Time     `json:"used_at,omitempty"`
	ExpiresAt time.Time      `gorm:"not null" json:"expires_at"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for VerificationToken
func (VerificationToken) TableName() string {
	return "verification_tokens"
}

// BeforeCreate hook for VerificationToken
func (vt *VerificationToken) BeforeCreate(tx *gorm.DB) error {
	if vt.ID == uuid.Nil {
		vt.ID = uuid.New()
	}

	// Generate token if not set
	if vt.Token == "" {
		token, err := GenerateSecureToken()
		if err != nil {
			return err
		}
		vt.Token = token
	}

	// Set expiration time based on type if not set
	if vt.ExpiresAt.IsZero() {
		switch vt.Type {
		case TokenTypeEmailVerification:
			vt.ExpiresAt = time.Now().Add(24 * time.Hour) // 24 hours
		case TokenTypePasswordReset:
			vt.ExpiresAt = time.Now().Add(1 * time.Hour) // 1 hour
		case TokenTypePhoneVerification:
			vt.ExpiresAt = time.Now().Add(10 * time.Minute) // 10 minutes
		case TokenTypeTwoFactor:
			vt.ExpiresAt = time.Now().Add(5 * time.Minute) // 5 minutes
		default:
			vt.ExpiresAt = time.Now().Add(1 * time.Hour) // Default 1 hour
		}
	}

	return nil
}

// IsValid checks if the token is valid (not expired and not used)
func (vt *VerificationToken) IsValid() bool {
	return !vt.Used && time.Now().Before(vt.ExpiresAt)
}

// IsExpired checks if the token has expired
func (vt *VerificationToken) IsExpired() bool {
	return time.Now().After(vt.ExpiresAt)
}

// MarkAsUsed marks the token as used
func (vt *VerificationToken) MarkAsUsed() {
	now := time.Now()
	vt.Used = true
	vt.UsedAt = &now
}

// GenerateSecureToken generates a cryptographically secure random token
func GenerateSecureToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateShortCode generates a short verification code (for SMS/2FA)
func GenerateShortCode() (string, error) {
	bytes := make([]byte, 3)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Convert to 6-digit number
	code := int(bytes[0])<<16 | int(bytes[1])<<8 | int(bytes[2])
	return fmt.Sprintf("%06d", code%1000000), nil
}

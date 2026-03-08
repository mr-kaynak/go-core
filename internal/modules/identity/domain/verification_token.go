package domain

import (
	"crypto/rand"
	"crypto/sha256"
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

	// Token expiration durations
	emailVerificationExpiry = 24 * time.Hour
	passwordResetExpiry     = 1 * time.Hour
	phoneVerificationExpiry = 10 * time.Minute
	twoFactorExpiry         = 5 * time.Minute
	defaultTokenExpiry      = 1 * time.Hour

	// secureTokenBytes is the number of random bytes used for secure token generation
	secureTokenBytes = 32
	// shortCodeBytes is the number of random bytes used for short code generation
	shortCodeBytes = 3
	// shortCodeModulus is the modulus used to generate a 6-digit short code
	shortCodeModulus = 1000000
)

// VerificationToken represents a verification token
type VerificationToken struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	User      User           `gorm:"foreignKey:UserID" json:"-"`
	Token     string         `gorm:"uniqueIndex;not null" json:"-"`
	Type      TokenType      `gorm:"type:varchar(30);not null" json:"type"`
	Used      bool           `gorm:"default:false" json:"used"`
	UsedAt    *time.Time     `json:"used_at,omitempty"`
	ExpiresAt time.Time      `gorm:"not null" json:"expires_at"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// RawToken holds the unhashed token value after creation. It is never
	// persisted — only available in-memory for sending to the user once.
	RawToken string `gorm:"-" json:"-"`
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

	// Generate token if not set, hash before storage
	if vt.Token == "" {
		raw, err := GenerateSecureToken()
		if err != nil {
			return err
		}
		vt.RawToken = raw
		vt.Token = HashVerificationToken(raw)
	}

	// Set expiration time based on type if not set
	if vt.ExpiresAt.IsZero() {
		switch vt.Type {
		case TokenTypeEmailVerification:
			vt.ExpiresAt = time.Now().Add(emailVerificationExpiry)
		case TokenTypePasswordReset:
			vt.ExpiresAt = time.Now().Add(passwordResetExpiry)
		case TokenTypePhoneVerification:
			vt.ExpiresAt = time.Now().Add(phoneVerificationExpiry)
		case TokenTypeTwoFactor:
			vt.ExpiresAt = time.Now().Add(twoFactorExpiry)
		default:
			vt.ExpiresAt = time.Now().Add(defaultTokenExpiry)
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

// HashVerificationToken returns the SHA-256 hex digest of a raw token.
// Used to store only the hash in the database.
func HashVerificationToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// GenerateSecureToken generates a cryptographically secure random token
func GenerateSecureToken() (string, error) {
	bytes := make([]byte, secureTokenBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateShortCode generates a short verification code (for SMS/2FA)
func GenerateShortCode() (string, error) {
	bytes := make([]byte, shortCodeBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Convert to 6-digit number
	code := int(bytes[0])<<16 | int(bytes[1])<<8 | int(bytes[2]) //nolint:gosec // G115: safe, values are single bytes
	return fmt.Sprintf("%06d", code%shortCodeModulus), nil
}

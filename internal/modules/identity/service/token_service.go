package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// TokenBlacklistChecker is an interface for checking token/user blacklist status.
// Defined here to avoid import cycles with the cache package.
type TokenBlacklistChecker interface {
	IsBlacklisted(ctx context.Context, tokenHash string) (bool, error)
	IsUserBlacklisted(ctx context.Context, userID string) (bool, error)
}

// TokenService handles JWT token operations
type TokenService struct {
	cfg       *config.Config
	userRepo  repository.UserRepository
	blacklist TokenBlacklistChecker
	logger    *logger.Logger
}

// NewTokenService creates a new token service
// userRepo is optional for backward compatibility
func NewTokenService(cfg *config.Config, userRepo ...repository.UserRepository) *TokenService {
	ts := &TokenService{
		cfg:    cfg,
		logger: logger.Get().WithFields(logger.Fields{"service": "token"}),
	}
	if len(userRepo) > 0 && userRepo[0] != nil {
		ts.userRepo = userRepo[0]
	}
	return ts
}

// SetBlacklist sets the token blacklist checker (optional, for Redis integration).
func (s *TokenService) SetBlacklist(b TokenBlacklistChecker) {
	s.blacklist = b
}

// Claims represents the JWT claims
type Claims struct {
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email"`
	Username    string    `json:"username"`
	Roles       []string  `json:"roles,omitempty"`
	Permissions []string  `json:"permissions,omitempty"`
	jwt.RegisteredClaims
}

// TokenPair represents access and refresh tokens
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// GenerateTokenPair generates a new access and refresh token pair
func (s *TokenService) GenerateTokenPair(user *domain.User) (*TokenPair, error) {
	// Generate access token
	accessToken, expiresAt, err := s.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshToken, err := s.GenerateRefreshToken(user)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// GenerateAccessToken generates a new access token
func (s *TokenService) GenerateAccessToken(user *domain.User) (string, time.Time, error) {
	// Extract role names
	roleNames := make([]string, 0, len(user.Roles))
	for i := range user.Roles {
		roleNames = append(roleNames, user.Roles[i].Name)
	}

	// Extract permission names
	permissionNames := make([]string, 0)
	seen := make(map[string]bool)
	for i := range user.Roles {
		for j := range user.Roles[i].Permissions {
			if !seen[user.Roles[i].Permissions[j].Name] {
				permissionNames = append(permissionNames, user.Roles[i].Permissions[j].Name)
				seen[user.Roles[i].Permissions[j].Name] = true
			}
		}
	}

	// Set expiration time
	expiresAt := time.Now().Add(s.cfg.JWT.Expiry)

	// Create claims
	claims := Claims{
		UserID:      user.ID,
		Email:       user.Email,
		Username:    user.Username,
		Roles:       roleNames,
		Permissions: permissionNames,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    s.cfg.JWT.Issuer,
			Subject:   user.ID.String(),
			Audience:  jwt.ClaimStrings{audienceAccess},
		},
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign token
	tokenString, err := token.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to sign access token: %w", err)
	}

	return tokenString, expiresAt, nil
}

// GenerateRefreshToken generates a new refresh token
func (s *TokenService) GenerateRefreshToken(user *domain.User) (string, error) {
	// Set expiration time
	expiresAt := time.Now().Add(s.cfg.JWT.RefreshExpiry)

	// Create simple claims for refresh token
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
		Issuer:    s.cfg.JWT.Issuer,
		Subject:   user.ID.String(),
		Audience:  jwt.ClaimStrings{audienceRefresh},
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign token with separate refresh secret
	tokenString, err := token.SignedString(s.refreshSigningKey())
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh token: %w", err)
	}

	// Store token hash in database if repository is available
	if s.userRepo != nil {
		refreshToken := &domain.RefreshToken{
			UserID:    user.ID,
			Token:     hashToken(tokenString),
			ExpiresAt: expiresAt,
			Revoked:   false,
		}
		if err := s.userRepo.CreateRefreshToken(refreshToken); err != nil {
			s.logger.WithError(err).Error("Failed to store refresh token in database")
			// Don't fail token generation, but log the error
		}
	}

	return tokenString, nil
}

// ValidateAccessToken validates an access token and returns the claims
func (s *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	// Parse token with audience validation to prevent refresh token cross-use
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Check signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	}, jwt.WithAudience(audienceAccess))

	if err != nil {
		if err == jwt.ErrTokenExpired {
			return nil, errors.NewUnauthorized("Token has expired")
		}
		if err == jwt.ErrTokenNotValidYet {
			return nil, errors.NewUnauthorized("Token is not valid yet")
		}
		return nil, errors.NewUnauthorized("Invalid token")
	}

	// Check if token is valid
	if !token.Valid {
		return nil, errors.NewUnauthorized("Invalid token")
	}

	// Extract claims
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errors.NewUnauthorized("Invalid token claims")
	}

	// Check blacklist if available (fail-closed: Redis errors reject the token)
	if s.blacklist != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if blocked, err := s.blacklist.IsBlacklisted(ctx, hashToken(tokenString)); blocked {
			if err != nil {
				return nil, errors.NewServiceUnavailable("Token validation temporarily unavailable")
			}
			return nil, errors.NewUnauthorized("Token has been revoked")
		}
		if blocked, err := s.blacklist.IsUserBlacklisted(ctx, claims.UserID.String()); blocked {
			if err != nil {
				return nil, errors.NewServiceUnavailable("Token validation temporarily unavailable")
			}
			return nil, errors.NewUnauthorized("All user tokens have been revoked")
		}
	}

	return claims, nil
}

// BlacklistAccessToken adds an access token to the blacklist.
func (s *TokenService) BlacklistAccessToken(ctx context.Context, tokenString string, expiry time.Duration) error {
	if s.blacklist == nil {
		return nil
	}
	type blacklister interface {
		Blacklist(ctx context.Context, tokenHash string, expiry time.Duration) error
	}
	if bl, ok := s.blacklist.(blacklister); ok {
		return bl.Blacklist(ctx, hashToken(tokenString), expiry)
	}
	return nil
}

// BlacklistAllUserTokens blacklists all tokens for a user.
func (s *TokenService) BlacklistAllUserTokens(ctx context.Context, userID string, expiry time.Duration) error {
	if s.blacklist == nil {
		return nil
	}
	type userBlacklister interface {
		BlacklistUser(ctx context.Context, userID string, expiry time.Duration) error
	}
	if bl, ok := s.blacklist.(userBlacklister); ok {
		return bl.BlacklistUser(ctx, userID, expiry)
	}
	return nil
}

// ValidateRefreshToken validates a refresh token
func (s *TokenService) ValidateRefreshToken(tokenString string) (uuid.UUID, error) {
	// Parse token with audience validation and refresh-specific secret
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Check signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.refreshSigningKey(), nil
	}, jwt.WithAudience(audienceRefresh))

	if err != nil {
		if err == jwt.ErrTokenExpired {
			return uuid.Nil, errors.NewUnauthorized("Refresh token has expired")
		}
		return uuid.Nil, errors.NewUnauthorized("Invalid refresh token")
	}

	// Check if token is valid
	if !token.Valid {
		return uuid.Nil, errors.NewUnauthorized("Invalid refresh token")
	}

	// Extract claims
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return uuid.Nil, errors.NewUnauthorized("Invalid refresh token claims")
	}

	// Parse user ID
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, errors.NewUnauthorized("Invalid user ID in refresh token")
	}

	// Check if token is revoked in database
	if s.userRepo != nil {
		tokenHash := hashToken(tokenString)
		storedToken, err := s.userRepo.GetRefreshToken(tokenHash)
		if err != nil {
			return uuid.Nil, errors.NewUnauthorized("Refresh token not found")
		}
		if storedToken.Revoked {
			return uuid.Nil, errors.NewUnauthorized("Refresh token has been revoked")
		}
	}

	return userID, nil
}

// RevokeRefreshToken revokes a refresh token
func (s *TokenService) RevokeRefreshToken(tokenString string) error {
	if s.userRepo != nil {
		tokenHash := hashToken(tokenString)
		return s.userRepo.RevokeRefreshToken(tokenHash)
	}
	// If no repository, just validate that the token exists
	_, err := s.ValidateRefreshToken(tokenString)
	return err
}

// RevokeAllUserTokens revokes all refresh tokens for a user
func (s *TokenService) RevokeAllUserTokens(userID uuid.UUID) error {
	if s.userRepo != nil {
		return s.userRepo.RevokeAllUserRefreshTokens(userID)
	}
	return nil
}

// Token audience constants to prevent cross-use attacks
const (
	audienceAccess  = "access"
	audienceRefresh = "refresh"
)

// refreshSigningKey returns the signing key for refresh tokens.
// Uses RefreshSecret if configured, otherwise derives from the main secret.
func (s *TokenService) refreshSigningKey() []byte {
	if s.cfg.JWT.RefreshSecret != "" {
		return []byte(s.cfg.JWT.RefreshSecret)
	}
	derived := sha256.Sum256([]byte(s.cfg.JWT.Secret + ":refresh"))
	return derived[:]
}

// hashToken creates a SHA256 hash of a token string
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

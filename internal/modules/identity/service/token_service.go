package service

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// TokenService handles JWT token operations
type TokenService struct {
	cfg *config.Config
}

// NewTokenService creates a new token service
func NewTokenService(cfg *config.Config) *TokenService {
	return &TokenService{
		cfg: cfg,
	}
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
	var roleNames []string
	for _, role := range user.Roles {
		roleNames = append(roleNames, role.Name)
	}

	// Extract permission names
	var permissionNames []string
	seen := make(map[string]bool)
	for _, role := range user.Roles {
		for _, perm := range role.Permissions {
			if !seen[perm.Name] {
				permissionNames = append(permissionNames, perm.Name)
				seen[perm.Name] = true
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
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign token
	tokenString, err := token.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh token: %w", err)
	}

	// You would typically also store this in the database
	// as a domain.RefreshToken entity

	return tokenString, nil
}

// ValidateAccessToken validates an access token and returns the claims
func (s *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	// Parse token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Check signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	})

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

	return claims, nil
}

// ValidateRefreshToken validates a refresh token
func (s *TokenService) ValidateRefreshToken(tokenString string) (uuid.UUID, error) {
	// Parse token
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Check signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	})

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

	// You would typically also check if this refresh token
	// exists in the database and hasn't been revoked

	return userID, nil
}

// RevokeRefreshToken revokes a refresh token
func (s *TokenService) RevokeRefreshToken(tokenString string) error {
	// This would typically mark the refresh token as revoked in the database
	// For now, we'll just validate it exists
	_, err := s.ValidateRefreshToken(tokenString)
	return err
}

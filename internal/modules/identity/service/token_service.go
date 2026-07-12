package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/crypto"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"gorm.io/gorm"
)

// TokenBlacklistChecker is an interface for token/user blacklist operations.
// Defined here to avoid import cycles with the cache package.
type TokenBlacklistChecker interface {
	IsBlacklisted(ctx context.Context, tokenHash string) (bool, error)
	IsUserBlacklisted(ctx context.Context, userID string) (bool, error)
	Blacklist(ctx context.Context, tokenHash string, expiry time.Duration) error
	BlacklistUser(ctx context.Context, userID string, expiry time.Duration) error
	ClearUserBlacklist(ctx context.Context, userID string) error
}

// TokenService handles JWT token operations
type TokenService struct {
	cfg       *config.Config
	userRepo  repository.RefreshTokenManager
	blacklist TokenBlacklistChecker
	logger    *logger.Logger
}

// NewTokenService creates a new token service
// userRepo is optional for backward compatibility
func NewTokenService(cfg *config.Config, userRepo ...repository.RefreshTokenManager) *TokenService {
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

// SessionMeta holds optional metadata captured at token creation time.
type SessionMeta struct {
	IPAddress string
	UserAgent string
}

// GenerateTokenPair generates a new access and refresh token pair
func (s *TokenService) GenerateTokenPair(ctx context.Context, user *domain.User, meta ...SessionMeta) (*TokenPair, error) {
	// Generate access token
	accessToken, expiresAt, err := s.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshToken, err := s.GenerateRefreshToken(ctx, user, meta...)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// GenerateTokenPairWithTx generates a token pair and persists the refresh token
// row using the provided transaction, so it commits atomically with any other
// writes the caller wraps in the same transaction.
func (s *TokenService) GenerateTokenPairWithTx(
	ctx context.Context, tx *gorm.DB, user *domain.User, meta ...SessionMeta,
) (*TokenPair, error) {
	accessToken, expiresAt, err := s.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.generateRefreshTokenString(user)
	if err != nil {
		return nil, err
	}

	if s.userRepo != nil {
		// Scope the repo to the transaction when one is provided; with a nil
		// tx (callers without a real DB, e.g. tests) fall back to the plain
		// repo so the refresh token is still persisted.
		repo := s.userRepo
		inTx := false
		if tx != nil {
			type withTxer interface {
				WithTx(tx *gorm.DB) repository.UserRepository
			}
			if txable, ok := s.userRepo.(withTxer); ok {
				repo = txable.WithTx(tx)
				inTx = true
			}
		}

		rt := &domain.RefreshToken{
			UserID:    user.ID,
			Token:     hashToken(refreshToken),
			ExpiresAt: time.Now().Add(s.cfg.JWT.RefreshExpiry),
			Revoked:   false,
		}
		if len(meta) > 0 {
			rt.IPAddress = meta[0].IPAddress
			rt.UserAgent = meta[0].UserAgent
		}
		if err := repo.CreateRefreshToken(ctx, rt); err != nil {
			if inTx {
				// Inside a transaction the caller expects atomicity — fail hard.
				return nil, fmt.Errorf("failed to store refresh token: %w", err)
			}
			s.logger.WithError(err).Error("Failed to store refresh token in database")
		}
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// generateRefreshTokenString signs a new refresh JWT without persisting it.
// Session metadata is applied at persistence time by the caller.
func (s *TokenService) generateRefreshTokenString(user *domain.User) (string, error) {
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.cfg.JWT.RefreshExpiry)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
		Issuer:    s.cfg.JWT.Issuer,
		Subject:   user.ID.String(),
		Audience:  jwt.ClaimStrings{audienceRefresh},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.refreshSigningKey())
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh token: %w", err)
	}
	return tokenString, nil
}

// GenerateAccessToken generates a new access token
func (s *TokenService) GenerateAccessToken(user *domain.User) (string, time.Time, error) {
	// Extract role and permission names
	roleNames := user.GetRoleNames()
	permissionNames := user.GetPermissionNames()

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
func (s *TokenService) GenerateRefreshToken(ctx context.Context, user *domain.User, meta ...SessionMeta) (string, error) {
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
		if len(meta) > 0 {
			refreshToken.IPAddress = meta[0].IPAddress
			refreshToken.UserAgent = meta[0].UserAgent
		}
		if err := s.userRepo.CreateRefreshToken(ctx, refreshToken); err != nil {
			s.logger.WithError(err).Error("Failed to store refresh token in database")
			// Don't fail token generation, but log the error
		}
	}

	return tokenString, nil
}

// ValidateAccessToken validates an access token and returns the claims
func (s *TokenService) ValidateAccessToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Parse token with audience validation to prevent refresh token cross-use
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Check signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	}, jwt.WithAudience(audienceAccess))

	if err != nil {
		if stderrors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.NewUnauthorized("Token has expired")
		}
		if stderrors.Is(err, jwt.ErrTokenNotValidYet) {
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
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		blocked, err := s.blacklist.IsBlacklisted(ctx, hashToken(tokenString))
		if err != nil {
			return nil, errors.NewServiceUnavailable("Token validation temporarily unavailable")
		}
		if blocked {
			return nil, errors.NewUnauthorized("Token has been revoked")
		}

		userBlocked, err := s.blacklist.IsUserBlacklisted(ctx, claims.UserID.String())
		if err != nil {
			return nil, errors.NewServiceUnavailable("Token validation temporarily unavailable")
		}
		if userBlocked {
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
	return s.blacklist.Blacklist(ctx, hashToken(tokenString), expiry)
}

// ClearUserBlacklist removes the user-level blacklist so newly issued tokens are accepted.
func (s *TokenService) ClearUserBlacklist(ctx context.Context, userID string) error {
	if s.blacklist == nil {
		return nil
	}
	return s.blacklist.ClearUserBlacklist(ctx, userID)
}

// BlacklistAllUserTokens blacklists all tokens for a user.
func (s *TokenService) BlacklistAllUserTokens(ctx context.Context, userID string, expiry time.Duration) error {
	if s.blacklist == nil {
		return nil
	}
	return s.blacklist.BlacklistUser(ctx, userID, expiry)
}

// ValidateRefreshToken validates a refresh token
func (s *TokenService) ValidateRefreshToken(ctx context.Context, tokenString string) (uuid.UUID, error) {
	// Parse token with audience validation and refresh-specific secret
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Check signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.refreshSigningKey(), nil
	}, jwt.WithAudience(audienceRefresh))

	if err != nil {
		if stderrors.Is(err, jwt.ErrTokenExpired) {
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
		storedToken, err := s.userRepo.GetRefreshToken(ctx, tokenHash)
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
func (s *TokenService) RevokeRefreshToken(ctx context.Context, tokenString string) error {
	if s.userRepo != nil {
		tokenHash := hashToken(tokenString)
		return s.userRepo.RevokeRefreshToken(ctx, tokenHash)
	}
	// If no repository, just validate that the token exists
	_, err := s.ValidateRefreshToken(ctx, tokenString)
	return err
}

// RevokeAllUserTokens revokes all refresh tokens for a user
func (s *TokenService) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	if s.userRepo != nil {
		return s.userRepo.RevokeAllUserRefreshTokens(ctx, userID)
	}
	return nil
}

// Token audience constants to prevent cross-use attacks
const (
	audienceAccess    = "access"
	audienceRefresh   = "refresh"
	audienceTwoFactor = "2fa"

	twoFactorTokenExpiry = 5 * time.Minute
)

// GenerateTwoFactorToken generates a short-lived token for completing 2FA login.
func (s *TokenService) GenerateTwoFactorToken(userID uuid.UUID) (string, error) {
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(twoFactorTokenExpiry)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
		Issuer:    s.cfg.JWT.Issuer,
		Subject:   userID.String(),
		Audience:  jwt.ClaimStrings{audienceTwoFactor},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.twoFactorSigningKey())
	if err != nil {
		return "", fmt.Errorf("failed to sign 2FA token: %w", err)
	}
	return tokenString, nil
}

// ValidateTwoFactorToken validates a 2FA token and returns the user ID.
func (s *TokenService) ValidateTwoFactorToken(ctx context.Context, tokenString string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.twoFactorSigningKey(), nil
	}, jwt.WithAudience(audienceTwoFactor))

	if err != nil {
		if stderrors.Is(err, jwt.ErrTokenExpired) {
			return uuid.Nil, errors.NewUnauthorized("Two-factor token has expired, please login again")
		}
		return uuid.Nil, errors.NewUnauthorized("Invalid two-factor token")
	}

	if !token.Valid {
		return uuid.Nil, errors.NewUnauthorized("Invalid two-factor token")
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return uuid.Nil, errors.NewUnauthorized("Invalid two-factor token claims")
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, errors.NewUnauthorized("Invalid user ID in two-factor token")
	}

	// Check blacklist if available (fail-closed: reject token if Redis errors)
	if s.blacklist != nil {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		blocked, blErr := s.blacklist.IsBlacklisted(ctx, hashToken(tokenString))
		if blErr != nil {
			return uuid.Nil, errors.NewServiceUnavailable("Token validation temporarily unavailable")
		}
		if blocked {
			return uuid.Nil, errors.NewUnauthorized("Two-factor token has already been used")
		}
	}

	return userID, nil
}

// refreshSigningKey returns the signing key for refresh tokens.
func (s *TokenService) refreshSigningKey() []byte {
	return []byte(s.cfg.JWT.RefreshSecret)
}

// twoFactorSigningKey returns a dedicated signing key for 2FA tokens,
// derived from the main secret via HMAC to prevent cross-use with access tokens.
func (s *TokenService) twoFactorSigningKey() []byte {
	return crypto.DeriveHMACKey([]byte(s.cfg.JWT.Secret), "2fa-token")
}

// hashToken creates a SHA256 hash of a token string
func hashToken(token string) string {
	return crypto.HashSHA256Hex(token)
}

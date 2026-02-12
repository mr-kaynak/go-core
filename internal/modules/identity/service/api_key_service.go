package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

// CreateAPIKeyRequest represents a request to create an API key
type CreateAPIKeyRequest struct {
	Name      string     `json:"name" validate:"required,min=1,max=100"`
	Scopes    string     `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateAPIKeyResponse represents the response after creating an API key
type CreateAPIKeyResponse struct {
	APIKey *domain.APIKey `json:"api_key"`
	RawKey string         `json:"key"`
}

// APIKeyService handles API key business logic
type APIKeyService struct {
	apiKeyRepo repository.APIKeyRepository
	logger     *logger.Logger
}

// NewAPIKeyService creates a new API key service
func NewAPIKeyService(apiKeyRepo repository.APIKeyRepository) *APIKeyService {
	return &APIKeyService{
		apiKeyRepo: apiKeyRepo,
		logger:     logger.Get().WithFields(logger.Fields{"service": "api_key"}),
	}
}

// Create generates a new API key for a user
func (s *APIKeyService) Create(userID uuid.UUID, req *CreateAPIKeyRequest) (*CreateAPIKeyResponse, error) {
	// Generate the raw API key using UUID
	rawKey := fmt.Sprintf("gck_%s", strings.ReplaceAll(uuid.New().String(), "-", ""))

	// Extract prefix for display
	keyPrefix := rawKey[:8]

	// Hash the key for storage
	keyHash := domain.HashAPIKey(rawKey)

	apiKey := &domain.APIKey{
		ID:        uuid.New(),
		UserID:    userID,
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
		Name:      req.Name,
		Scopes:    req.Scopes,
		ExpiresAt: req.ExpiresAt,
	}

	if err := s.apiKeyRepo.Create(apiKey); err != nil {
		s.logger.WithError(err).Error("Failed to create API key", "user_id", userID)
		return nil, errors.NewInternalError("Failed to create API key")
	}

	s.logger.Info("API key created successfully", "user_id", userID, "key_id", apiKey.ID, "key_name", req.Name)

	return &CreateAPIKeyResponse{
		APIKey: apiKey,
		RawKey: rawKey,
	}, nil
}

// Validate validates an API key and returns the associated key record
func (s *APIKeyService) Validate(rawKey string) (*domain.APIKey, error) {
	keyHash := domain.HashAPIKey(rawKey)

	apiKey, err := s.apiKeyRepo.GetByHash(keyHash)
	if err != nil {
		return nil, errors.NewUnauthorized("Invalid API key")
	}

	if !apiKey.IsValid() {
		if apiKey.Revoked {
			return nil, errors.NewUnauthorized("API key has been revoked")
		}
		return nil, errors.NewUnauthorized("API key has expired")
	}

	// Update last used timestamp asynchronously
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("Panic in UpdateLastUsed goroutine", "key_id", apiKey.ID, "panic", r)
			}
		}()
		if err := s.apiKeyRepo.UpdateLastUsed(apiKey.ID); err != nil {
			s.logger.WithError(err).Warn("Failed to update API key last used timestamp", "key_id", apiKey.ID)
		}
	}()

	return apiKey, nil
}

// Revoke revokes an API key
func (s *APIKeyService) Revoke(keyID uuid.UUID, userID uuid.UUID) error {
	// Verify the key belongs to the user
	apiKey, err := s.apiKeyRepo.GetByID(keyID)
	if err != nil {
		return errors.NewNotFound("API Key", keyID.String())
	}

	if apiKey.UserID != userID {
		return errors.NewForbidden("You do not have permission to revoke this API key")
	}

	if apiKey.Revoked {
		return errors.NewBadRequest("API key is already revoked")
	}

	if err := s.apiKeyRepo.Revoke(keyID); err != nil {
		s.logger.WithError(err).Error("Failed to revoke API key", "key_id", keyID, "user_id", userID)
		return errors.NewInternalError("Failed to revoke API key")
	}

	s.logger.Info("API key revoked successfully", "key_id", keyID, "user_id", userID)
	return nil
}

// List returns all API keys for a user
func (s *APIKeyService) List(userID uuid.UUID) ([]*domain.APIKey, error) {
	keys, err := s.apiKeyRepo.GetUserKeys(userID)
	if err != nil {
		s.logger.WithError(err).Error("Failed to list API keys", "user_id", userID)
		return nil, errors.NewInternalError("Failed to list API keys")
	}
	return keys, nil
}

package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// Health/status string constants used across admin responses.
const (
	statusHealthy     = "healthy"
	statusUnhealthy   = "unhealthy"
	statusUnavailable = "unavailable"

	maxExportLimit    = 10000
	maxBulkOperations = 1000
)

// SessionSafeResponse is a safe representation of a session that excludes the refresh token value.
type SessionSafeResponse struct {
	SessionID uuid.UUID `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
}

// APIKeySafeResponse is a safe representation of an API key that excludes the hash.
type APIKeySafeResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	UserID     uuid.UUID  `json:"user_id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	IsRevoked  bool       `json:"is_revoked"`
}

// toSessionSafeResponse converts a domain RefreshToken to a safe response without the token value.
func toSessionSafeResponse(token *domain.RefreshToken) SessionSafeResponse {
	return SessionSafeResponse{
		SessionID: token.ID,
		UserID:    token.UserID,
		CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt,
		IPAddress: token.IPAddress,
		UserAgent: token.UserAgent,
	}
}

// toAPIKeySafeResponse converts a domain APIKey to a safe response without the hash.
func toAPIKeySafeResponse(key *domain.APIKey) APIKeySafeResponse {
	return APIKeySafeResponse{
		ID:         key.ID,
		Name:       key.Name,
		UserID:     key.UserID,
		CreatedAt:  key.CreatedAt,
		LastUsedAt: key.LastUsedAt,
		IsRevoked:  key.Revoked,
	}
}

// calculateOverallStatus determines the overall system health status.
// All healthy → "healthy", some unhealthy → "degraded", all unhealthy → "unhealthy".
func calculateOverallStatus(components map[string]ComponentHealth) string {
	totalComponents := len(components)
	if totalComponents == 0 {
		return statusHealthy
	}

	unhealthyCount := 0
	for _, comp := range components {
		if comp.Status == statusUnhealthy {
			unhealthyCount++
		}
	}

	switch unhealthyCount {
	case 0:
		return statusHealthy
	case totalComponents:
		return statusUnhealthy
	default:
		return "degraded"
	}
}

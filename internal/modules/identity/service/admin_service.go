package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/config"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
)

// HealthChecker provides health check probes for infrastructure components.
type HealthChecker interface {
	CheckDatabase() (*DatabaseHealth, error)
	CheckRedis() (*RedisHealth, error)
}

// DatabaseHealth holds database connection pool statistics.
type DatabaseHealth struct {
	OpenConnections int
	InUse           int
	Idle            int
	MaxOpen         int
	WaitCount       int64
	WaitDuration    time.Duration
}

// RedisHealth holds Redis health check results.
type RedisHealth struct {
	Connected    bool
	PingDuration time.Duration
}

// redisHealthPinger is the minimal interface needed for Redis health checks.
type redisHealthPinger interface {
	HealthCheck() error
}

type healthChecker struct {
	sqlDB       *sql.DB
	redisClient redisHealthPinger
}

// NewHealthChecker creates a HealthChecker from infrastructure components.
// Both parameters are optional (nil-safe).
func NewHealthChecker(sqlDB *sql.DB, redisClient redisHealthPinger) HealthChecker {
	return &healthChecker{sqlDB: sqlDB, redisClient: redisClient}
}

func (h *healthChecker) CheckDatabase() (*DatabaseHealth, error) {
	if h.sqlDB == nil {
		return nil, nil
	}
	stats := h.sqlDB.Stats()
	return &DatabaseHealth{
		OpenConnections: stats.OpenConnections,
		InUse:           stats.InUse,
		Idle:            stats.Idle,
		MaxOpen:         stats.MaxOpenConnections,
		WaitCount:       stats.WaitCount,
		WaitDuration:    stats.WaitDuration,
	}, nil
}

func (h *healthChecker) CheckRedis() (*RedisHealth, error) {
	if h.redisClient == nil {
		return nil, nil
	}
	start := time.Now()
	err := h.redisClient.HealthCheck()
	dur := time.Since(start)
	if err != nil {
		return &RedisHealth{Connected: false, PingDuration: dur}, err
	}
	return &RedisHealth{Connected: true, PingDuration: dur}, nil
}

// UserStatsResult holds aggregated user statistics for the admin dashboard.
type UserStatsResult struct {
	Total              int64
	Active             int64
	Inactive           int64
	Locked             int64
	TodayRegistrations int64
}

// NotificationStatsResult holds notification statistics for the admin dashboard.
type NotificationStatsResult struct {
	ByStatus map[string]int64
	ByType   map[string]int64
}

// AdminService handles admin-specific business logic that was previously
// embedded in the AdminHandler.
type AdminService struct {
	userRepo         repository.UserRepository
	notificationRepo notificationRepository.NotificationRepository
	tokenService     *TokenService
	cfg              *config.Config
	healthChecker    HealthChecker
	logger           *logger.Logger
}

// NewAdminService creates a new admin service.
func NewAdminService(
	userRepo repository.UserRepository,
	notificationRepo notificationRepository.NotificationRepository,
	tokenService *TokenService,
	cfg *config.Config,
	healthChecker HealthChecker,
) *AdminService {
	return &AdminService{
		userRepo:         userRepo,
		notificationRepo: notificationRepo,
		tokenService:     tokenService,
		cfg:              cfg,
		healthChecker:    healthChecker,
		logger:           logger.Get().WithFields(logger.Fields{"service": "admin"}),
	}
}

// CollectUserStats gathers user statistics for the admin dashboard.
func (s *AdminService) CollectUserStats() (*UserStatsResult, error) {
	stats := &UserStatsResult{}

	total, err := s.userRepo.Count()
	if err != nil {
		return nil, err
	}
	stats.Total = total

	active, err := s.userRepo.CountByStatus("active")
	if err != nil {
		return nil, err
	}
	stats.Active = active

	inactive, err := s.userRepo.CountByStatus("inactive")
	if err != nil {
		return nil, err
	}
	stats.Inactive = inactive

	locked, err := s.userRepo.CountByStatus("locked")
	if err != nil {
		return nil, err
	}
	stats.Locked = locked

	const hoursPerDay = 24
	todayStart := time.Now().Truncate(hoursPerDay * time.Hour)
	todayRegs, err := s.userRepo.CountCreatedAfter(todayStart)
	if err != nil {
		return nil, err
	}
	stats.TodayRegistrations = todayRegs

	return stats, nil
}

// CollectNotificationStats gathers notification counts grouped by status and type.
func (s *AdminService) CollectNotificationStats() (*NotificationStatsResult, error) {
	byStatus, err := s.notificationRepo.CountByStatus()
	if err != nil {
		return nil, err
	}

	byType, err := s.notificationRepo.CountByType()
	if err != nil {
		return nil, err
	}

	return &NotificationStatsResult{ByStatus: byStatus, ByType: byType}, nil
}

// ListActiveSessions returns paginated active sessions system-wide.
func (s *AdminService) ListActiveSessions(offset, limit int) ([]*domain.RefreshToken, int64, error) {
	tokens, err := s.userRepo.GetAllActiveSessions(offset, limit)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.userRepo.CountActiveSessions()
	if err != nil {
		return nil, 0, err
	}

	return tokens, total, nil
}

// ForceLogoutUser revokes all refresh tokens and blacklists access tokens for a user.
func (s *AdminService) ForceLogoutUser(ctx context.Context, userID uuid.UUID) error {
	if err := s.userRepo.RevokeAllUserRefreshTokens(userID); err != nil {
		return err
	}

	if err := s.tokenService.BlacklistAllUserTokens(ctx, userID.String(), s.cfg.JWT.Expiry); err != nil {
		s.logger.WithError(err).Warn("Failed to blacklist user access tokens during force logout")
	}

	return nil
}

// ValidateRoleExists checks if a role exists by ID.
func (s *AdminService) ValidateRoleExists(roleID uuid.UUID) error {
	_, err := s.userRepo.GetRoleByID(roleID)
	return err
}

// ListEmailLogs returns paginated email logs with optional status filtering.
func (s *AdminService) ListEmailLogs(offset, limit int, status string) ([]*notificationDomain.EmailLog, int64, error) {
	return s.notificationRepo.ListEmailLogs(offset, limit, status)
}

// CheckDatabaseHealth returns database connection pool health.
func (s *AdminService) CheckDatabaseHealth() (*DatabaseHealth, error) {
	return s.healthChecker.CheckDatabase()
}

// CheckRedisHealth returns Redis health check results.
func (s *AdminService) CheckRedisHealth() (*RedisHealth, error) {
	return s.healthChecker.CheckRedis()
}

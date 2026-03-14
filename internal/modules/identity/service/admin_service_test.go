package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
	notificationRepository "github.com/mr-kaynak/go-core/internal/modules/notification/repository"
	"github.com/mr-kaynak/go-core/internal/test"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// adminUserRepoStub extends authRepoStub with configurable AdminUserManager
// methods needed by AdminService.
type adminUserRepoStub struct {
	authRepoStub

	countByStatusFn       func(status string) (int64, error)
	countCreatedAfterFn   func(after time.Time) (int64, error)
	getAllActiveSessionsFn func(offset, limit int) ([]*domain.RefreshToken, error)
	countActiveSessionsFn func() (int64, error)
}

func (s *adminUserRepoStub) CountByStatus(status string) (int64, error) {
	if s.countByStatusFn != nil {
		return s.countByStatusFn(status)
	}
	return 0, nil
}

func (s *adminUserRepoStub) CountCreatedAfter(after time.Time) (int64, error) {
	if s.countCreatedAfterFn != nil {
		return s.countCreatedAfterFn(after)
	}
	return 0, nil
}

func (s *adminUserRepoStub) GetAllActiveSessions(offset, limit int) ([]*domain.RefreshToken, error) {
	if s.getAllActiveSessionsFn != nil {
		return s.getAllActiveSessionsFn(offset, limit)
	}
	return nil, nil
}

func (s *adminUserRepoStub) CountActiveSessions() (int64, error) {
	if s.countActiveSessionsFn != nil {
		return s.countActiveSessionsFn()
	}
	return 0, nil
}

// notificationRepoStub implements notificationRepository.NotificationRepository.
type notificationRepoStub struct {
	countByStatusFn  func() (map[string]int64, error)
	countByTypeFn    func() (map[string]int64, error)
	listEmailLogsFn  func(offset, limit int, status string) ([]*notificationDomain.EmailLog, int64, error)
}

var _ notificationRepository.NotificationRepository = (*notificationRepoStub)(nil)

func (s *notificationRepoStub) CountByStatus() (map[string]int64, error) {
	if s.countByStatusFn != nil {
		return s.countByStatusFn()
	}
	return nil, nil
}

func (s *notificationRepoStub) CountByType() (map[string]int64, error) {
	if s.countByTypeFn != nil {
		return s.countByTypeFn()
	}
	return nil, nil
}

func (s *notificationRepoStub) ListEmailLogs(offset, limit int, status string) ([]*notificationDomain.EmailLog, int64, error) {
	if s.listEmailLogsFn != nil {
		return s.listEmailLogsFn(offset, limit, status)
	}
	return nil, 0, nil
}

// Unused methods required to satisfy the full NotificationRepository interface.
func (s *notificationRepoStub) CreateNotification(_ *notificationDomain.Notification) error {
	return nil
}
func (s *notificationRepoStub) UpdateNotification(_ *notificationDomain.Notification) error {
	return nil
}
func (s *notificationRepoStub) DeleteNotification(_ uuid.UUID) error { return nil }
func (s *notificationRepoStub) GetNotification(_ uuid.UUID) (*notificationDomain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetUserNotifications(_ uuid.UUID, _, _ int) ([]*notificationDomain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetPendingNotifications(_ int) ([]*notificationDomain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetFailedNotifications(_ int) ([]*notificationDomain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetScheduledNotifications(_ int) ([]*notificationDomain.Notification, error) {
	return nil, nil
}
func (s *notificationRepoStub) CountUserNotifications(_ uuid.UUID) (int64, error) { return 0, nil }
func (s *notificationRepoStub) GetUserNotificationsSince(_ uuid.UUID, _ time.Time, _ int) ([]*notificationDomain.Notification, bool, error) {
	return nil, false, nil
}
func (s *notificationRepoStub) MarkAsRead(_, _ uuid.UUID) error   { return nil }
func (s *notificationRepoStub) MarkAllAsRead(_ uuid.UUID) error   { return nil }
func (s *notificationRepoStub) CreateEmailLog(_ *notificationDomain.EmailLog) error {
	return nil
}
func (s *notificationRepoStub) UpdateEmailLog(_ *notificationDomain.EmailLog) error {
	return nil
}
func (s *notificationRepoStub) GetEmailLog(_ uuid.UUID) (*notificationDomain.EmailLog, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetEmailLogsByNotification(_ uuid.UUID) ([]*notificationDomain.EmailLog, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetEmailLogsByUser(_ uuid.UUID, _, _ int) ([]*notificationDomain.EmailLog, error) {
	return nil, nil
}
func (s *notificationRepoStub) CreateTemplate(_ *notificationDomain.NotificationTemplate) error {
	return nil
}
func (s *notificationRepoStub) UpdateTemplate(_ *notificationDomain.NotificationTemplate) error {
	return nil
}
func (s *notificationRepoStub) DeleteTemplate(_ uuid.UUID) error { return nil }
func (s *notificationRepoStub) GetTemplate(_ uuid.UUID) (*notificationDomain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetTemplateByName(_ string) (*notificationDomain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetTemplates(_, _ int) ([]*notificationDomain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoStub) GetActiveTemplates(_ notificationDomain.NotificationType) ([]*notificationDomain.NotificationTemplate, error) {
	return nil, nil
}
func (s *notificationRepoStub) CreateUserPreferences(_ *notificationDomain.NotificationPreference) error {
	return nil
}
func (s *notificationRepoStub) UpdateUserPreferences(_ *notificationDomain.NotificationPreference) error {
	return nil
}
func (s *notificationRepoStub) DeleteUserPreferences(_ uuid.UUID) error { return nil }
func (s *notificationRepoStub) GetUserPreferences(_ uuid.UUID) (*notificationDomain.NotificationPreference, error) {
	return nil, nil
}

// healthCheckerStub implements HealthChecker with configurable return values.
type healthCheckerStub struct {
	checkDatabaseFn func() (*DatabaseHealth, error)
	checkRedisFn    func() (*RedisHealth, error)
}

func (s *healthCheckerStub) CheckDatabase() (*DatabaseHealth, error) {
	if s.checkDatabaseFn != nil {
		return s.checkDatabaseFn()
	}
	return nil, nil
}

func (s *healthCheckerStub) CheckRedis() (*RedisHealth, error) {
	if s.checkRedisFn != nil {
		return s.checkRedisFn()
	}
	return nil, nil
}

// redisHealthStub implements redisHealthPinger for testing NewHealthChecker.
type redisHealthStub struct {
	err error
}

func (s *redisHealthStub) HealthCheck() error { return s.err }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAdminService_NewAdminService(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{}
	notifRepo := &notificationRepoStub{}
	tokenSvc := NewTokenService(cfg, &repo.authRepoStub)
	hc := &healthCheckerStub{}

	svc := NewAdminService(repo, notifRepo, tokenSvc, cfg, hc)
	if svc == nil {
		t.Fatal("expected non-nil AdminService")
	}
	if svc.userRepo != repo {
		t.Fatal("expected userRepo to be set")
	}
	if svc.notificationRepo != notifRepo {
		t.Fatal("expected notificationRepo to be set")
	}
	if svc.tokenService != tokenSvc {
		t.Fatal("expected tokenService to be set")
	}
	if svc.cfg != cfg {
		t.Fatal("expected cfg to be set")
	}
	if svc.healthChecker != hc {
		t.Fatal("expected healthChecker to be set")
	}
	if svc.logger == nil {
		t.Fatal("expected logger to be initialized")
	}
}

func TestAdminService_NewAdminService_NilOptionalDeps(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{}
	tokenSvc := NewTokenService(cfg)

	svc := NewAdminService(repo, nil, tokenSvc, cfg, nil)
	if svc == nil {
		t.Fatal("expected non-nil AdminService with nil optional deps")
	}
}

// ---------------------------------------------------------------------------
// CollectUserStats
// ---------------------------------------------------------------------------

func TestAdminService_CollectUserStats_Success(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			countFn: func() (int64, error) { return 100, nil },
		},
		countByStatusFn: func(status string) (int64, error) {
			switch status {
			case "active":
				return 80, nil
			case "inactive":
				return 15, nil
			case "locked":
				return 5, nil
			default:
				return 0, nil
			}
		},
		countCreatedAfterFn: func(after time.Time) (int64, error) {
			return 3, nil
		},
	}
	tokenSvc := NewTokenService(cfg)
	svc := NewAdminService(repo, &notificationRepoStub{}, tokenSvc, cfg, nil)

	stats, err := svc.CollectUserStats()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if stats.Total != 100 {
		t.Fatalf("expected Total=100, got %d", stats.Total)
	}
	if stats.Active != 80 {
		t.Fatalf("expected Active=80, got %d", stats.Active)
	}
	if stats.Inactive != 15 {
		t.Fatalf("expected Inactive=15, got %d", stats.Inactive)
	}
	if stats.Locked != 5 {
		t.Fatalf("expected Locked=5, got %d", stats.Locked)
	}
	if stats.TodayRegistrations != 3 {
		t.Fatalf("expected TodayRegistrations=3, got %d", stats.TodayRegistrations)
	}
}

func TestAdminService_CollectUserStats_CountError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			countFn: func() (int64, error) { return 0, errors.New("db error") },
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectUserStats()
	if err == nil {
		t.Fatal("expected error when Count fails")
	}
}

func TestAdminService_CollectUserStats_CountByStatusActiveError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			countFn: func() (int64, error) { return 10, nil },
		},
		countByStatusFn: func(status string) (int64, error) {
			if status == "active" {
				return 0, errors.New("active count failed")
			}
			return 0, nil
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectUserStats()
	if err == nil {
		t.Fatal("expected error when CountByStatus('active') fails")
	}
}

func TestAdminService_CollectUserStats_CountByStatusInactiveError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			countFn: func() (int64, error) { return 10, nil },
		},
		countByStatusFn: func(status string) (int64, error) {
			if status == "inactive" {
				return 0, errors.New("inactive count failed")
			}
			return 5, nil
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectUserStats()
	if err == nil {
		t.Fatal("expected error when CountByStatus('inactive') fails")
	}
}

func TestAdminService_CollectUserStats_CountByStatusLockedError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			countFn: func() (int64, error) { return 10, nil },
		},
		countByStatusFn: func(status string) (int64, error) {
			if status == "locked" {
				return 0, errors.New("locked count failed")
			}
			return 5, nil
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectUserStats()
	if err == nil {
		t.Fatal("expected error when CountByStatus('locked') fails")
	}
}

func TestAdminService_CollectUserStats_CountCreatedAfterError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			countFn: func() (int64, error) { return 10, nil },
		},
		countByStatusFn: func(status string) (int64, error) { return 5, nil },
		countCreatedAfterFn: func(after time.Time) (int64, error) {
			return 0, errors.New("count created after failed")
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectUserStats()
	if err == nil {
		t.Fatal("expected error when CountCreatedAfter fails")
	}
}

func TestAdminService_CollectUserStats_AllZero(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	stats, err := svc.CollectUserStats()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if stats.Total != 0 || stats.Active != 0 || stats.Inactive != 0 || stats.Locked != 0 || stats.TodayRegistrations != 0 {
		t.Fatal("expected all zeros for default stubs")
	}
}

// ---------------------------------------------------------------------------
// CollectNotificationStats
// ---------------------------------------------------------------------------

func TestAdminService_CollectNotificationStats_Success(t *testing.T) {
	cfg := test.TestConfig()
	notifRepo := &notificationRepoStub{
		countByStatusFn: func() (map[string]int64, error) {
			return map[string]int64{"sent": 50, "pending": 10, "failed": 2}, nil
		},
		countByTypeFn: func() (map[string]int64, error) {
			return map[string]int64{"email": 40, "push": 20, "in_app": 2}, nil
		},
	}
	repo := &adminUserRepoStub{}
	svc := NewAdminService(repo, notifRepo, NewTokenService(cfg), cfg, nil)

	result, err := svc.CollectNotificationStats()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ByStatus["sent"] != 50 {
		t.Fatalf("expected ByStatus[sent]=50, got %d", result.ByStatus["sent"])
	}
	if result.ByStatus["pending"] != 10 {
		t.Fatalf("expected ByStatus[pending]=10, got %d", result.ByStatus["pending"])
	}
	if result.ByStatus["failed"] != 2 {
		t.Fatalf("expected ByStatus[failed]=2, got %d", result.ByStatus["failed"])
	}
	if result.ByType["email"] != 40 {
		t.Fatalf("expected ByType[email]=40, got %d", result.ByType["email"])
	}
	if result.ByType["push"] != 20 {
		t.Fatalf("expected ByType[push]=20, got %d", result.ByType["push"])
	}
}

func TestAdminService_CollectNotificationStats_CountByStatusError(t *testing.T) {
	cfg := test.TestConfig()
	notifRepo := &notificationRepoStub{
		countByStatusFn: func() (map[string]int64, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, notifRepo, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectNotificationStats()
	if err == nil {
		t.Fatal("expected error when CountByStatus fails")
	}
}

func TestAdminService_CollectNotificationStats_CountByTypeError(t *testing.T) {
	cfg := test.TestConfig()
	notifRepo := &notificationRepoStub{
		countByStatusFn: func() (map[string]int64, error) {
			return map[string]int64{}, nil
		},
		countByTypeFn: func() (map[string]int64, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, notifRepo, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectNotificationStats()
	if err == nil {
		t.Fatal("expected error when CountByType fails")
	}
}

// ---------------------------------------------------------------------------
// ListActiveSessions
// ---------------------------------------------------------------------------

func TestAdminService_ListActiveSessions_Success(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()
	sessions := []*domain.RefreshToken{
		{ID: uuid.New(), UserID: userID, Token: "hash1", ExpiresAt: time.Now().Add(time.Hour)},
		{ID: uuid.New(), UserID: userID, Token: "hash2", ExpiresAt: time.Now().Add(2 * time.Hour)},
	}
	repo := &adminUserRepoStub{
		getAllActiveSessionsFn: func(offset, limit int) ([]*domain.RefreshToken, error) {
			if offset != 0 || limit != 20 {
				t.Fatalf("unexpected offset=%d, limit=%d", offset, limit)
			}
			return sessions, nil
		},
		countActiveSessionsFn: func() (int64, error) { return 42, nil },
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	tokens, total, err := svc.ListActiveSessions(0, 20)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if total != 42 {
		t.Fatalf("expected total=42, got %d", total)
	}
}

func TestAdminService_ListActiveSessions_GetAllError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		getAllActiveSessionsFn: func(offset, limit int) ([]*domain.RefreshToken, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, _, err := svc.ListActiveSessions(0, 10)
	if err == nil {
		t.Fatal("expected error when GetAllActiveSessions fails")
	}
}

func TestAdminService_ListActiveSessions_CountError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		getAllActiveSessionsFn: func(offset, limit int) ([]*domain.RefreshToken, error) {
			return []*domain.RefreshToken{}, nil
		},
		countActiveSessionsFn: func() (int64, error) {
			return 0, errors.New("count error")
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, _, err := svc.ListActiveSessions(0, 10)
	if err == nil {
		t.Fatal("expected error when CountActiveSessions fails")
	}
}

func TestAdminService_ListActiveSessions_EmptyResult(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		getAllActiveSessionsFn: func(offset, limit int) ([]*domain.RefreshToken, error) {
			return []*domain.RefreshToken{}, nil
		},
		countActiveSessionsFn: func() (int64, error) { return 0, nil },
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	tokens, total, err := svc.ListActiveSessions(0, 10)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("expected empty result, got %d tokens", len(tokens))
	}
	if total != 0 {
		t.Fatalf("expected total=0, got %d", total)
	}
}

func TestAdminService_ListActiveSessions_Pagination(t *testing.T) {
	cfg := test.TestConfig()
	var capturedOffset, capturedLimit int
	repo := &adminUserRepoStub{
		getAllActiveSessionsFn: func(offset, limit int) ([]*domain.RefreshToken, error) {
			capturedOffset = offset
			capturedLimit = limit
			return []*domain.RefreshToken{}, nil
		},
		countActiveSessionsFn: func() (int64, error) { return 100, nil },
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, _, err := svc.ListActiveSessions(40, 20)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if capturedOffset != 40 || capturedLimit != 20 {
		t.Fatalf("expected offset=40, limit=20, got offset=%d, limit=%d", capturedOffset, capturedLimit)
	}
}

// ---------------------------------------------------------------------------
// ForceLogoutUser
// ---------------------------------------------------------------------------

func TestAdminService_ForceLogoutUser_Success(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()
	revokedCalled := false

	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			revokeAllRefreshTokenFn: func(id uuid.UUID) error {
				if id != userID {
					t.Fatalf("expected userID %s, got %s", userID, id)
				}
				revokedCalled = true
				return nil
			},
		},
	}
	tokenSvc := NewTokenService(cfg, &repo.authRepoStub)
	svc := NewAdminService(repo, &notificationRepoStub{}, tokenSvc, cfg, nil)

	err := svc.ForceLogoutUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !revokedCalled {
		t.Fatal("expected RevokeAllUserRefreshTokens to be called")
	}
}

func TestAdminService_ForceLogoutUser_RevokeError(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			revokeAllRefreshTokenFn: func(id uuid.UUID) error {
				return errors.New("revoke failed")
			},
		},
	}
	tokenSvc := NewTokenService(cfg, &repo.authRepoStub)
	svc := NewAdminService(repo, &notificationRepoStub{}, tokenSvc, cfg, nil)

	err := svc.ForceLogoutUser(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error when RevokeAllUserRefreshTokens fails")
	}
	if err.Error() != "revoke failed" {
		t.Fatalf("expected 'revoke failed', got %q", err.Error())
	}
}

func TestAdminService_ForceLogoutUser_BlacklistErrorIsLoggedNotReturned(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()

	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			revokeAllRefreshTokenFn: func(id uuid.UUID) error { return nil },
		},
	}
	tokenSvc := NewTokenService(cfg, &repo.authRepoStub)
	blacklistUserCalled := false
	tokenSvc.SetBlacklist(&blacklistStub{
		blacklistFn: func(ctx context.Context, tokenHash string, expiry time.Duration) error {
			// BlacklistAllUserTokens calls BlacklistUser, not Blacklist. But we
			// use the blacklistStub which has a BlacklistUser method returning nil
			// by default. We need to check via the stub directly.
			return nil
		},
	})
	// Replace with a stub that fails on BlacklistUser
	tokenSvc.SetBlacklist(&adminBlacklistStub{
		blacklistUserFn: func(ctx context.Context, userID string, expiry time.Duration) error {
			blacklistUserCalled = true
			return errors.New("blacklist down")
		},
	})
	svc := NewAdminService(repo, &notificationRepoStub{}, tokenSvc, cfg, nil)

	err := svc.ForceLogoutUser(context.Background(), userID)
	// ForceLogoutUser logs the blacklist error but does not return it.
	if err != nil {
		t.Fatalf("expected no error (blacklist failure is best-effort), got %v", err)
	}
	if !blacklistUserCalled {
		t.Fatal("expected BlacklistUser to be called")
	}
}

func TestAdminService_ForceLogoutUser_NoBlacklistSet(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()

	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			revokeAllRefreshTokenFn: func(id uuid.UUID) error { return nil },
		},
	}
	tokenSvc := NewTokenService(cfg, &repo.authRepoStub)
	// No blacklist set -- BlacklistAllUserTokens returns nil
	svc := NewAdminService(repo, &notificationRepoStub{}, tokenSvc, cfg, nil)

	err := svc.ForceLogoutUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected no error without blacklist, got %v", err)
	}
}

// adminBlacklistStub provides a configurable BlacklistUser method.
type adminBlacklistStub struct {
	blacklistUserFn func(ctx context.Context, userID string, expiry time.Duration) error
}

func (s *adminBlacklistStub) IsBlacklisted(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *adminBlacklistStub) IsUserBlacklisted(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *adminBlacklistStub) Blacklist(_ context.Context, _ string, _ time.Duration) error {
	return nil
}
func (s *adminBlacklistStub) BlacklistUser(ctx context.Context, userID string, expiry time.Duration) error {
	if s.blacklistUserFn != nil {
		return s.blacklistUserFn(ctx, userID, expiry)
	}
	return nil
}
func (s *adminBlacklistStub) ClearUserBlacklist(_ context.Context, _ string) error { return nil }

// ---------------------------------------------------------------------------
// ValidateRoleExists
// ---------------------------------------------------------------------------

func TestAdminService_ValidateRoleExists_Found(t *testing.T) {
	cfg := test.TestConfig()
	roleID := uuid.New()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			getRoleByIDFn: func(id uuid.UUID) (*domain.Role, error) {
				if id != roleID {
					t.Fatalf("expected roleID %s, got %s", roleID, id)
				}
				return &domain.Role{ID: roleID, Name: "admin"}, nil
			},
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	if err := svc.ValidateRoleExists(roleID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestAdminService_ValidateRoleExists_NotFound(t *testing.T) {
	cfg := test.TestConfig()
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			getRoleByIDFn: func(id uuid.UUID) (*domain.Role, error) {
				return nil, errors.New("role not found")
			},
		},
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	err := svc.ValidateRoleExists(uuid.New())
	if err == nil {
		t.Fatal("expected error for missing role")
	}
}

// ---------------------------------------------------------------------------
// ListEmailLogs
// ---------------------------------------------------------------------------

func TestAdminService_ListEmailLogs_Success(t *testing.T) {
	cfg := test.TestConfig()
	logs := []*notificationDomain.EmailLog{
		{To: "a@test.com", Subject: "Welcome", Status: "sent"},
		{To: "b@test.com", Subject: "Reset", Status: "pending"},
	}
	notifRepo := &notificationRepoStub{
		listEmailLogsFn: func(offset, limit int, status string) ([]*notificationDomain.EmailLog, int64, error) {
			if offset != 0 || limit != 10 || status != "sent" {
				t.Fatalf("unexpected params: offset=%d, limit=%d, status=%q", offset, limit, status)
			}
			return logs, 2, nil
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, notifRepo, NewTokenService(cfg), cfg, nil)

	result, total, err := svc.ListEmailLogs(0, 10, "sent")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(result))
	}
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
}

func TestAdminService_ListEmailLogs_Error(t *testing.T) {
	cfg := test.TestConfig()
	notifRepo := &notificationRepoStub{
		listEmailLogsFn: func(offset, limit int, status string) ([]*notificationDomain.EmailLog, int64, error) {
			return nil, 0, errors.New("db error")
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, notifRepo, NewTokenService(cfg), cfg, nil)

	_, _, err := svc.ListEmailLogs(0, 10, "")
	if err == nil {
		t.Fatal("expected error when ListEmailLogs fails")
	}
}

func TestAdminService_ListEmailLogs_EmptyStatus(t *testing.T) {
	cfg := test.TestConfig()
	var capturedStatus string
	notifRepo := &notificationRepoStub{
		listEmailLogsFn: func(offset, limit int, status string) ([]*notificationDomain.EmailLog, int64, error) {
			capturedStatus = status
			return []*notificationDomain.EmailLog{}, 0, nil
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, notifRepo, NewTokenService(cfg), cfg, nil)

	_, _, err := svc.ListEmailLogs(0, 20, "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if capturedStatus != "" {
		t.Fatalf("expected empty status to be passed through, got %q", capturedStatus)
	}
}

// ---------------------------------------------------------------------------
// CheckDatabaseHealth
// ---------------------------------------------------------------------------

func TestAdminService_CheckDatabaseHealth_Success(t *testing.T) {
	cfg := test.TestConfig()
	expected := &DatabaseHealth{
		OpenConnections: 10,
		InUse:           3,
		Idle:            7,
		MaxOpen:         25,
		WaitCount:       100,
		WaitDuration:    5 * time.Millisecond,
	}
	hc := &healthCheckerStub{
		checkDatabaseFn: func() (*DatabaseHealth, error) {
			return expected, nil
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, &notificationRepoStub{}, NewTokenService(cfg), cfg, hc)

	result, err := svc.CheckDatabaseHealth()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.OpenConnections != 10 {
		t.Fatalf("expected OpenConnections=10, got %d", result.OpenConnections)
	}
	if result.InUse != 3 {
		t.Fatalf("expected InUse=3, got %d", result.InUse)
	}
	if result.Idle != 7 {
		t.Fatalf("expected Idle=7, got %d", result.Idle)
	}
	if result.MaxOpen != 25 {
		t.Fatalf("expected MaxOpen=25, got %d", result.MaxOpen)
	}
	if result.WaitCount != 100 {
		t.Fatalf("expected WaitCount=100, got %d", result.WaitCount)
	}
	if result.WaitDuration != 5*time.Millisecond {
		t.Fatalf("expected WaitDuration=5ms, got %v", result.WaitDuration)
	}
}

func TestAdminService_CheckDatabaseHealth_Error(t *testing.T) {
	cfg := test.TestConfig()
	hc := &healthCheckerStub{
		checkDatabaseFn: func() (*DatabaseHealth, error) {
			return nil, errors.New("db health check failed")
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, &notificationRepoStub{}, NewTokenService(cfg), cfg, hc)

	_, err := svc.CheckDatabaseHealth()
	if err == nil {
		t.Fatal("expected error when CheckDatabase fails")
	}
}

// ---------------------------------------------------------------------------
// CheckRedisHealth
// ---------------------------------------------------------------------------

func TestAdminService_CheckRedisHealth_Success(t *testing.T) {
	cfg := test.TestConfig()
	hc := &healthCheckerStub{
		checkRedisFn: func() (*RedisHealth, error) {
			return &RedisHealth{Connected: true, PingDuration: 2 * time.Millisecond}, nil
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, &notificationRepoStub{}, NewTokenService(cfg), cfg, hc)

	result, err := svc.CheckRedisHealth()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Connected {
		t.Fatal("expected Connected=true")
	}
	if result.PingDuration != 2*time.Millisecond {
		t.Fatalf("expected PingDuration=2ms, got %v", result.PingDuration)
	}
}

func TestAdminService_CheckRedisHealth_NotConnected(t *testing.T) {
	cfg := test.TestConfig()
	hc := &healthCheckerStub{
		checkRedisFn: func() (*RedisHealth, error) {
			return &RedisHealth{Connected: false, PingDuration: 0}, errors.New("connection refused")
		},
	}
	svc := NewAdminService(&adminUserRepoStub{}, &notificationRepoStub{}, NewTokenService(cfg), cfg, hc)

	result, err := svc.CheckRedisHealth()
	if err == nil {
		t.Fatal("expected error when Redis is not connected")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on error")
	}
	if result.Connected {
		t.Fatal("expected Connected=false")
	}
}

// ---------------------------------------------------------------------------
// NewHealthChecker (defaultHealthChecker)
// ---------------------------------------------------------------------------

func TestAdminService_NewHealthChecker_NilDeps(t *testing.T) {
	hc := NewHealthChecker(nil, nil)
	if hc == nil {
		t.Fatal("expected non-nil HealthChecker with nil deps")
	}

	// CheckDatabase returns nil, nil when sqlDB is nil
	dbHealth, err := hc.CheckDatabase()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if dbHealth != nil {
		t.Fatalf("expected nil DatabaseHealth for nil sqlDB, got %+v", dbHealth)
	}

	// CheckRedis returns nil, nil when redisClient is nil
	redisHealth, err := hc.CheckRedis()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if redisHealth != nil {
		t.Fatalf("expected nil RedisHealth for nil redisClient, got %+v", redisHealth)
	}
}

func TestAdminService_NewHealthChecker_RedisSuccess(t *testing.T) {
	stub := &redisHealthStub{err: nil}
	hc := NewHealthChecker(nil, stub)

	result, err := hc.CheckRedis()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil RedisHealth")
	}
	if !result.Connected {
		t.Fatal("expected Connected=true")
	}
	if result.PingDuration < 0 {
		t.Fatalf("expected non-negative PingDuration, got %v", result.PingDuration)
	}
}

func TestAdminService_NewHealthChecker_RedisError(t *testing.T) {
	stub := &redisHealthStub{err: errors.New("connection refused")}
	hc := NewHealthChecker(nil, stub)

	result, err := hc.CheckRedis()
	if err == nil {
		t.Fatal("expected error for failed Redis health check")
	}
	if result == nil {
		t.Fatal("expected non-nil RedisHealth even on error")
	}
	if result.Connected {
		t.Fatal("expected Connected=false on error")
	}
}

// ---------------------------------------------------------------------------
// CollectUserStats — verify CountByStatus is called with correct status values
// ---------------------------------------------------------------------------

func TestAdminService_CollectUserStats_VerifiesStatusValues(t *testing.T) {
	cfg := test.TestConfig()
	statusesSeen := make(map[string]bool)
	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			countFn: func() (int64, error) { return 1, nil },
		},
		countByStatusFn: func(status string) (int64, error) {
			statusesSeen[status] = true
			return 0, nil
		},
		countCreatedAfterFn: func(after time.Time) (int64, error) { return 0, nil },
	}
	svc := NewAdminService(repo, &notificationRepoStub{}, NewTokenService(cfg), cfg, nil)

	_, err := svc.CollectUserStats()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, status := range []string{"active", "inactive", "locked"} {
		if !statusesSeen[status] {
			t.Fatalf("expected CountByStatus to be called with %q", status)
		}
	}
}

// ---------------------------------------------------------------------------
// ForceLogoutUser — verify blacklist duration uses cfg.JWT.Expiry
// ---------------------------------------------------------------------------

func TestAdminService_ForceLogoutUser_BlacklistDurationMatchesConfig(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()

	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			revokeAllRefreshTokenFn: func(id uuid.UUID) error { return nil },
		},
	}
	tokenSvc := NewTokenService(cfg, &repo.authRepoStub)

	var capturedExpiry time.Duration
	tokenSvc.SetBlacklist(&adminBlacklistStub{
		blacklistUserFn: func(ctx context.Context, uid string, expiry time.Duration) error {
			capturedExpiry = expiry
			return nil
		},
	})
	svc := NewAdminService(repo, &notificationRepoStub{}, tokenSvc, cfg, nil)

	err := svc.ForceLogoutUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if capturedExpiry != cfg.JWT.Expiry {
		t.Fatalf("expected blacklist expiry=%v (from cfg.JWT.Expiry), got %v", cfg.JWT.Expiry, capturedExpiry)
	}
}

// ---------------------------------------------------------------------------
// ForceLogoutUser — verify userID propagated to BlacklistAllUserTokens
// ---------------------------------------------------------------------------

func TestAdminService_ForceLogoutUser_PropagatesUserID(t *testing.T) {
	cfg := test.TestConfig()
	userID := uuid.New()

	repo := &adminUserRepoStub{
		authRepoStub: authRepoStub{
			revokeAllRefreshTokenFn: func(id uuid.UUID) error { return nil },
		},
	}
	tokenSvc := NewTokenService(cfg, &repo.authRepoStub)

	var capturedUID string
	tokenSvc.SetBlacklist(&adminBlacklistStub{
		blacklistUserFn: func(ctx context.Context, uid string, expiry time.Duration) error {
			capturedUID = uid
			return nil
		},
	})
	svc := NewAdminService(repo, &notificationRepoStub{}, tokenSvc, cfg, nil)

	err := svc.ForceLogoutUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if capturedUID != userID.String() {
		t.Fatalf("expected userID=%s, got %s", userID, capturedUID)
	}
}

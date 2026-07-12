package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	cryptoutil "github.com/mr-kaynak/go-core/internal/core/crypto"
	coreerrors "github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
	"github.com/mr-kaynak/go-core/internal/modules/identity/service"
	"github.com/mr-kaynak/go-core/internal/test"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

type twoFAUserRepoStub struct {
	getByIDFn func(id uuid.UUID) (*domain.User, error)
	updateFn  func(user *domain.User) error
}

var _ repository.UserRepository = (*twoFAUserRepoStub)(nil)

func (s *twoFAUserRepoStub) WithTx(_ *gorm.DB) repository.UserRepository       { return s }
func (s *twoFAUserRepoStub) Create(_ context.Context, user *domain.User) error { return nil }
func (s *twoFAUserRepoStub) Update(_ context.Context, user *domain.User) error {
	if s.updateFn != nil {
		return s.updateFn(user)
	}
	return nil
}
func (s *twoFAUserRepoStub) Delete(_ context.Context, id uuid.UUID) error { return nil }
func (s *twoFAUserRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
func (s *twoFAUserRepoStub) GetByIDForUpdate(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(id)
	}
	return nil, nil
}
func (s *twoFAUserRepoStub) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) GetAll(_ context.Context, offset, limit int) ([]*domain.User, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) GetByIDs(_ context.Context, ids []uuid.UUID) ([]*domain.User, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) ListFiltered(_ context.Context, _ domain.UserListFilter) ([]*domain.User, int64, error) {
	return nil, 0, nil
}
func (s *twoFAUserRepoStub) Count(_ context.Context) (int64, error) { return 0, nil }
func (s *twoFAUserRepoStub) ExistsByEmail(_ context.Context, email string) (bool, error) {
	return false, nil
}
func (s *twoFAUserRepoStub) ExistsByUsername(_ context.Context, username string) (bool, error) {
	return false, nil
}
func (s *twoFAUserRepoStub) LoadRoles(_ context.Context, user *domain.User) error  { return nil }
func (s *twoFAUserRepoStub) CreateRole(_ context.Context, role *domain.Role) error { return nil }
func (s *twoFAUserRepoStub) UpdateRole(_ context.Context, role *domain.Role) error { return nil }
func (s *twoFAUserRepoStub) DeleteRole(_ context.Context, id uuid.UUID) error      { return nil }
func (s *twoFAUserRepoStub) GetRoleByID(_ context.Context, id uuid.UUID) (*domain.Role, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) GetRoleByName(_ context.Context, name string) (*domain.Role, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) GetAllRoles(_ context.Context) ([]*domain.Role, error) { return nil, nil }
func (s *twoFAUserRepoStub) AssignRole(_ context.Context, userID, roleID uuid.UUID) error {
	return nil
}
func (s *twoFAUserRepoStub) RemoveRole(_ context.Context, userID, roleID uuid.UUID) error {
	return nil
}
func (s *twoFAUserRepoStub) GetUserRoles(_ context.Context, userID uuid.UUID) ([]*domain.Role, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) CreatePermission(_ context.Context, permission *domain.Permission) error {
	return nil
}
func (s *twoFAUserRepoStub) UpdatePermission(_ context.Context, permission *domain.Permission) error {
	return nil
}
func (s *twoFAUserRepoStub) DeletePermission(_ context.Context, id uuid.UUID) error { return nil }
func (s *twoFAUserRepoStub) GetPermissionByID(_ context.Context, id uuid.UUID) (*domain.Permission, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) GetAllPermissions(_ context.Context) ([]*domain.Permission, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) AssignPermissionToRole(_ context.Context, roleID, permissionID uuid.UUID) error {
	return nil
}
func (s *twoFAUserRepoStub) RemovePermissionFromRole(_ context.Context, roleID, permissionID uuid.UUID) error {
	return nil
}
func (s *twoFAUserRepoStub) GetRolePermissions(_ context.Context, roleID uuid.UUID) ([]*domain.Permission, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) CreateRefreshToken(_ context.Context, token *domain.RefreshToken) error {
	return nil
}
func (s *twoFAUserRepoStub) GetRefreshToken(_ context.Context, token string) (*domain.RefreshToken, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) RevokeRefreshToken(_ context.Context, token string) error { return nil }
func (s *twoFAUserRepoStub) RevokeAllUserRefreshTokens(_ context.Context, userID uuid.UUID) error {
	return nil
}
func (s *twoFAUserRepoStub) GetActiveRefreshTokensByUser(_ context.Context, userID uuid.UUID) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) RevokeRefreshTokenByID(_ context.Context, id uuid.UUID) error { return nil }
func (s *twoFAUserRepoStub) CleanExpiredRefreshTokens(_ context.Context) error            { return nil }
func (s *twoFAUserRepoStub) CountByStatus(_ context.Context, status string) (int64, error) {
	return 0, nil
}
func (s *twoFAUserRepoStub) CountCreatedAfter(_ context.Context, after time.Time) (int64, error) {
	return 0, nil
}
func (s *twoFAUserRepoStub) GetAllActiveSessions(_ context.Context, offset, limit int, _ *uuid.UUID) ([]*domain.RefreshToken, error) {
	return nil, nil
}
func (s *twoFAUserRepoStub) CountActiveSessions(_ context.Context, _ *uuid.UUID) (int64, error) {
	return 0, nil
}

type twoFAVerificationRepoStub struct{}

var _ repository.VerificationTokenRepository = (*twoFAVerificationRepoStub)(nil)

func (s *twoFAVerificationRepoStub) WithTx(_ *gorm.DB) repository.VerificationTokenRepository {
	return s
}
func (s *twoFAVerificationRepoStub) Create(_ context.Context, token *domain.VerificationToken) error {
	return nil
}
func (s *twoFAVerificationRepoStub) FindByToken(_ context.Context, token string) (*domain.VerificationToken, error) {
	return nil, nil
}
func (s *twoFAVerificationRepoStub) FindByUserAndType(_ context.Context,
	userID uuid.UUID,
	tokenType domain.TokenType,
) (*domain.VerificationToken, error) {
	return nil, nil
}
func (s *twoFAVerificationRepoStub) Update(_ context.Context, token *domain.VerificationToken) error {
	return nil
}
func (s *twoFAVerificationRepoStub) Delete(_ context.Context, id uuid.UUID) error { return nil }
func (s *twoFAVerificationRepoStub) DeleteExpiredTokens(_ context.Context) error  { return nil }
func (s *twoFAVerificationRepoStub) DeleteByUserAndType(_ context.Context, userID uuid.UUID, tokenType domain.TokenType) error {
	return nil
}
func (s *twoFAVerificationRepoStub) CountByUserAndType(_ context.Context,
	userID uuid.UUID,
	tokenType domain.TokenType,
	since time.Time,
) (int64, error) {
	return 0, nil
}

func newTwoFATestApp() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if pd := coreerrors.GetProblemDetail(err); pd != nil {
				return c.Status(pd.Status).JSON(pd)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}

func twoFARequest(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func newTwoFAServiceForTest(t *testing.T, user *domain.User) *service.AuthService {
	t.Helper()

	repo := &twoFAUserRepoStub{
		getByIDFn: func(id uuid.UUID) (*domain.User, error) {
			if user.ID != id {
				return nil, errors.New("user not found")
			}
			return user, nil
		},
		updateFn: func(updated *domain.User) error {
			*user = *updated
			return nil
		},
	}
	cfg := test.TestConfig()

	return service.NewAuthService(
		cfg,
		nil,
		repo,
		service.NewTokenService(cfg, repo),
		&twoFAVerificationRepoStub{},
		nil,
		nil,
	)
}

func attachClaims(userID uuid.UUID) fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Locals("claims", &service.Claims{UserID: userID})
		return c.Next()
	}
}

func TestTwoFactorHandlerEnable_RequiresAuthentication(t *testing.T) {
	user := test.CreateTestUserWithDefaults()
	h := NewTwoFactorHandler(newTwoFAServiceForTest(t, user))

	app := newTwoFATestApp()
	app.Post("/2fa/enable", h.Enable)

	resp := twoFARequest(t, app, http.MethodPost, "/2fa/enable", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestTwoFactorHandlerEnable_ReturnsOTPURLForAuthenticatedUser(t *testing.T) {
	user := test.CreateTestUserWithDefaults()
	h := NewTwoFactorHandler(newTwoFAServiceForTest(t, user))

	app := newTwoFATestApp()
	app.Post("/2fa/enable", attachClaims(user.ID), h.Enable)

	resp := twoFARequest(t, app, http.MethodPost, "/2fa/enable", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if user.TwoFactorSecret == "" {
		t.Fatalf("expected two-factor secret to be generated and saved")
	}
}

func TestTwoFactorHandlerEnable_RejectsWhenAlreadyEnabled(t *testing.T) {
	user := test.CreateTestUserWithDefaults()
	user.TwoFactorEnabled = true
	h := NewTwoFactorHandler(newTwoFAServiceForTest(t, user))

	app := newTwoFATestApp()
	app.Post("/2fa/enable", attachClaims(user.ID), h.Enable)

	resp := twoFARequest(t, app, http.MethodPost, "/2fa/enable", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}
}

// decryptTestSecret decrypts the encrypted TOTP secret stored in the user for test purposes.
func decryptTestSecret(t *testing.T, encryptedSecret string) string {
	t.Helper()
	cfg := test.TestConfig()
	secret, err := cryptoutil.Decrypt(encryptedSecret, cryptoutil.DeriveKey(cfg.Security.EncryptionKey))
	if err != nil {
		t.Fatalf("failed to decrypt test TOTP secret: %v", err)
	}
	return secret
}

func TestTwoFactorHandlerVerify_ValidAndInvalidAndExpiredCode(t *testing.T) {
	user := test.CreateTestUserWithDefaults()
	h := NewTwoFactorHandler(newTwoFAServiceForTest(t, user))

	app := newTwoFATestApp()
	app.Post("/2fa/enable", attachClaims(user.ID), h.Enable)
	app.Post("/2fa/verify", attachClaims(user.ID), h.Verify)

	enableResp := twoFARequest(t, app, http.MethodPost, "/2fa/enable", "")
	if enableResp.StatusCode != http.StatusOK {
		t.Fatalf("expected enable status 200, got %d", enableResp.StatusCode)
	}

	plainSecret := decryptTestSecret(t, user.TwoFactorSecret)
	validCode, err := totp.GenerateCode(plainSecret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate valid totp code: %v", err)
	}
	validResp := twoFARequest(t, app, http.MethodPost, "/2fa/verify", `{"code":"`+validCode+`"}`)
	if validResp.StatusCode != http.StatusOK {
		t.Fatalf("expected verify status 200 for valid code, got %d", validResp.StatusCode)
	}

	user.TwoFactorEnabled = false
	invalidResp := twoFARequest(t, app, http.MethodPost, "/2fa/verify", `{"code":"000000"}`)
	if invalidResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected verify status 400 for invalid code, got %d", invalidResp.StatusCode)
	}

	expiredCode, err := totp.GenerateCode(plainSecret, time.Now().Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("failed to generate expired totp code: %v", err)
	}
	expiredResp := twoFARequest(t, app, http.MethodPost, "/2fa/verify", `{"code":"`+expiredCode+`"}`)
	if expiredResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected verify status 400 for expired code, got %d", expiredResp.StatusCode)
	}
}

func TestTwoFactorHandlerDisable_AllowsValidCodeAndRejectsInvalidCode(t *testing.T) {
	user := test.CreateTestUserWithDefaults()
	h := NewTwoFactorHandler(newTwoFAServiceForTest(t, user))

	app := newTwoFATestApp()
	app.Post("/2fa/enable", attachClaims(user.ID), h.Enable)
	app.Post("/2fa/verify", attachClaims(user.ID), h.Verify)
	app.Post("/2fa/disable", attachClaims(user.ID), h.Disable)

	enableResp := twoFARequest(t, app, http.MethodPost, "/2fa/enable", "")
	if enableResp.StatusCode != http.StatusOK {
		t.Fatalf("expected enable status 200, got %d", enableResp.StatusCode)
	}

	plainSecret := decryptTestSecret(t, user.TwoFactorSecret)
	verifyCode, err := totp.GenerateCode(plainSecret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate verification code: %v", err)
	}
	verifyResp := twoFARequest(t, app, http.MethodPost, "/2fa/verify", `{"code":"`+verifyCode+`"}`)
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected verify status 200, got %d", verifyResp.StatusCode)
	}
	if !user.TwoFactorEnabled {
		t.Fatalf("expected two-factor to be enabled after verification")
	}

	invalidResp := twoFARequest(t, app, http.MethodPost, "/2fa/disable", `{"code":"000000"}`)
	if invalidResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected disable status 400 for invalid code, got %d", invalidResp.StatusCode)
	}

	disableCode, err := totp.GenerateCode(plainSecret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate disable code: %v", err)
	}
	validResp := twoFARequest(t, app, http.MethodPost, "/2fa/disable", `{"code":"`+disableCode+`"}`)
	if validResp.StatusCode != http.StatusOK {
		t.Fatalf("expected disable status 200 for valid code, got %d", validResp.StatusCode)
	}
	if user.TwoFactorEnabled {
		t.Fatalf("expected two-factor to be disabled")
	}
}

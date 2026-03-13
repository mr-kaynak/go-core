package repository

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

func newTestVerificationTokenRepository(t *testing.T) (*gorm.DB, VerificationTokenRepository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.AutoMigrate(&domain.User{}, &domain.VerificationToken{}); err != nil {
		t.Fatalf("failed to run automigrate: %v", err)
	}

	return db, NewVerificationTokenRepository(db)
}

func TestVerificationTokenRepositoryWithTx(t *testing.T) {
	db, repo := newTestVerificationTokenRepository(t)

	impl := repo.(*verificationTokenRepositoryImpl)

	if repo.WithTx(nil) != repo {
		t.Fatalf("expected WithTx(nil) to return same instance")
	}

	tx := db.Begin()
	defer tx.Rollback()

	txRepo := repo.WithTx(tx)
	if txRepo == repo {
		t.Fatalf("expected WithTx(tx) to return new instance")
	}

	if txImpl, ok := txRepo.(*verificationTokenRepositoryImpl); !ok || txImpl.db != tx {
		t.Fatalf("expected WithTx(tx) to bind repository to transaction db")
	}

	if impl.db == tx {
		t.Fatalf("original repository must not be mutated by WithTx")
	}
}

func TestVerificationTokenRepositoryCreateAndFindByToken(t *testing.T) {
	_, repo := newTestVerificationTokenRepository(t)

	userID := uuid.New()
	token := &domain.VerificationToken{
		UserID: userID,
		Type:   domain.TokenTypeEmailVerification,
	}

	if err := repo.Create(token); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if token.RawToken == "" {
		t.Fatalf("expected RawToken to be populated by BeforeCreate")
	}

	if token.Token == "" {
		t.Fatalf("expected Token hash to be stored by BeforeCreate")
	}

	found, err := repo.FindByToken(token.RawToken)
	if err != nil {
		t.Fatalf("FindByToken failed: %v", err)
	}
	if found.ID != token.ID {
		t.Errorf("FindByToken returned wrong token")
	}
	if found.UserID != userID {
		t.Errorf("FindByToken returned token for wrong user")
	}
}

func TestVerificationTokenRepositoryFindByUserAndType(t *testing.T) {
	db, repo := newTestVerificationTokenRepository(t)

	userID := uuid.New()

	older := &domain.VerificationToken{
		UserID:    userID,
		Type:      domain.TokenTypePasswordReset,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.Create(older); err != nil {
		t.Fatalf("Create older token failed: %v", err)
	}

	// Ensure different CreatedAt ordering
	time.Sleep(10 * time.Millisecond)

	newer := &domain.VerificationToken{
		UserID:    userID,
		Type:      domain.TokenTypePasswordReset,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.Create(newer); err != nil {
		t.Fatalf("Create newer token failed: %v", err)
	}

	found, err := repo.FindByUserAndType(userID, domain.TokenTypePasswordReset)
	if err != nil {
		t.Fatalf("FindByUserAndType failed: %v", err)
	}
	if found.ID != newer.ID {
		t.Errorf("expected most recent unused token, got %v", found.ID)
	}

	// Mark token as used and verify it is no longer returned
	found.MarkAsUsed()
	if err := repo.Update(found); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	_, err = repo.FindByUserAndType(userID, domain.TokenTypePasswordReset)
	if err == nil {
		t.Fatalf("expected no unused tokens after marking as used")
	}

	// Soft delete and ensure it is excluded
	if err := repo.Delete(older.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	if err := db.Model(&domain.VerificationToken{}).Where("id = ?", older.ID).Count(&count).Error; err != nil {
		t.Fatalf("count after delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected older token to be soft deleted, found %d records", count)
	}
}

func TestVerificationTokenRepositoryDeleteAndCleanup(t *testing.T) {
	_, repo := newTestVerificationTokenRepository(t)

	userID := uuid.New()
	active := &domain.VerificationToken{
		UserID:    userID,
		Type:      domain.TokenTypeEmailVerification,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	expired := &domain.VerificationToken{
		UserID:    userID,
		Type:      domain.TokenTypeEmailVerification,
		ExpiresAt: time.Now().Add(-time.Hour),
	}

	if err := repo.Create(active); err != nil {
		t.Fatalf("Create(active) failed: %v", err)
	}
	if err := repo.Create(expired); err != nil {
		t.Fatalf("Create(expired) failed: %v", err)
	}

	if err := repo.DeleteExpiredTokens(); err != nil {
		t.Fatalf("DeleteExpiredTokens failed: %v", err)
	}

	// Only the active token should remain
	count, err := repo.CountByUserAndType(userID, domain.TokenTypeEmailVerification, time.Time{})
	if err != nil {
		t.Fatalf("CountByUserAndType failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active token after cleanup, got %d", count)
	}

	if err := repo.DeleteByUserAndType(userID, domain.TokenTypeEmailVerification); err != nil {
		t.Fatalf("DeleteByUserAndType failed: %v", err)
	}

	count, err = repo.CountByUserAndType(userID, domain.TokenTypeEmailVerification, time.Time{})
	if err != nil {
		t.Fatalf("CountByUserAndType after delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no tokens after DeleteByUserAndType, got %d", count)
	}
}

func TestVerificationTokenRepositoryCountByUserAndTypeSince(t *testing.T) {
	_, repo := newTestVerificationTokenRepository(t)

	userID := uuid.New()

	beforeWindow := &domain.VerificationToken{
		UserID:    userID,
		Type:      domain.TokenTypeTwoFactor,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.Create(beforeWindow); err != nil {
		t.Fatalf("Create(beforeWindow) failed: %v", err)
	}

	since := time.Now()

	withinWindow := &domain.VerificationToken{
		UserID:    userID,
		Type:      domain.TokenTypeTwoFactor,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.Create(withinWindow); err != nil {
		t.Fatalf("Create(withinWindow) failed: %v", err)
	}

	count, err := repo.CountByUserAndType(userID, domain.TokenTypeTwoFactor, since)
	if err != nil {
		t.Fatalf("CountByUserAndType failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 token created since cutoff, got %d", count)
	}
}


package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

func newTestAuditLogRepository(t *testing.T) (*gorm.DB, AuditLogRepository) {
	t.Helper()
	db := setupTestDB(t)
	return db, NewAuditLogRepository(db)
}

func seedAuditLog(t *testing.T, db *gorm.DB, userID *uuid.UUID, action, resource, resourceID string) *domain.AuditLog {
	t.Helper()
	entry := &domain.AuditLog{
		ID:         uuid.New(),
		UserID:     userID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IPAddress:  "127.0.0.1",
		UserAgent:  "test-agent",
	}
	if err := db.Create(entry).Error; err != nil {
		t.Fatalf("failed to seed audit log: %v", err)
	}
	return entry
}

func TestAuditLogRepositoryCreate(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	entry := &domain.AuditLog{
		ID:         uuid.New(),
		UserID:     &userID,
		Action:     "login",
		Resource:   "auth",
		ResourceID: userID.String(),
		IPAddress:  "192.168.1.1",
		UserAgent:  "Mozilla/5.0",
	}

	if err := repo.Create(entry); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify it was stored
	var count int64
	if err := db.Model(&domain.AuditLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit log entry, got %d", count)
	}
}

func TestAuditLogRepositoryCreateWithNilUserID(t *testing.T) {
	_, repo := newTestAuditLogRepository(t)

	entry := &domain.AuditLog{
		ID:       uuid.New(),
		Action:   "system.startup",
		Resource: "system",
	}

	if err := repo.Create(entry); err != nil {
		t.Fatalf("Create with nil user ID failed: %v", err)
	}
}

func TestAuditLogRepositoryGetByUser(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	otherUserID := uuid.New()

	seedAuditLog(t, db, &userID, "login", "auth", "")
	seedAuditLog(t, db, &userID, "update_profile", "users", userID.String())
	seedAuditLog(t, db, &otherUserID, "login", "auth", "")

	logs, err := repo.GetByUser(userID, 0, 10)
	if err != nil {
		t.Fatalf("GetByUser failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs for user, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByUserPagination(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	for i := 0; i < 5; i++ {
		seedAuditLog(t, db, &userID, "action", "resource", "")
	}

	logs, err := repo.GetByUser(userID, 2, 2)
	if err != nil {
		t.Fatalf("GetByUser with offset failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs with offset, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByAction(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	seedAuditLog(t, db, &userID, "login", "auth", "")
	seedAuditLog(t, db, &userID, "login", "auth", "")
	seedAuditLog(t, db, &userID, "logout", "auth", "")

	logs, err := repo.GetByAction("login", 0, 10)
	if err != nil {
		t.Fatalf("GetByAction failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 login logs, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByActionPagination(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	for i := 0; i < 5; i++ {
		seedAuditLog(t, db, &userID, "login", "auth", "")
	}

	logs, err := repo.GetByAction("login", 0, 2)
	if err != nil {
		t.Fatalf("GetByAction with limit failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs with limit, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByResource(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	seedAuditLog(t, db, &userID, "create", "posts", "post-1")
	seedAuditLog(t, db, &userID, "update", "posts", "post-2")
	seedAuditLog(t, db, &userID, "create", "comments", "comment-1")

	// Get all posts logs
	logs, err := repo.GetByResource("posts", "", 0, 10)
	if err != nil {
		t.Fatalf("GetByResource failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 post logs, got %d", len(logs))
	}

	// Get specific resource ID
	logs, err = repo.GetByResource("posts", "post-1", 0, 10)
	if err != nil {
		t.Fatalf("GetByResource with ID failed: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log for post-1, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByResourcePagination(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	for i := 0; i < 5; i++ {
		seedAuditLog(t, db, &userID, "action", "posts", "")
	}

	logs, err := repo.GetByResource("posts", "", 1, 2)
	if err != nil {
		t.Fatalf("GetByResource with offset failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs with offset, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAll(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	seedAuditLog(t, db, &userID, "login", "auth", "")
	seedAuditLog(t, db, &userID, "create", "posts", "post-1")
	seedAuditLog(t, db, &userID, "update", "posts", "post-1")

	// No filters - get all
	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
	if len(logs) != 3 {
		t.Errorf("expected 3 logs, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAllWithUserIDFilter(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	otherID := uuid.New()
	seedAuditLog(t, db, &userID, "login", "auth", "")
	seedAuditLog(t, db, &otherID, "login", "auth", "")

	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		UserID: &userID,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListAll with UserID failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAllWithActionFilter(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	seedAuditLog(t, db, &userID, "login", "auth", "")
	seedAuditLog(t, db, &userID, "logout", "auth", "")

	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		Action: "login",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListAll with Action failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAllWithResourceFilter(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	seedAuditLog(t, db, &userID, "create", "posts", "p1")
	seedAuditLog(t, db, &userID, "create", "comments", "c1")

	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		Resource: "posts",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListAll with Resource failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAllWithResourceIDFilter(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	seedAuditLog(t, db, &userID, "create", "posts", "post-1")
	seedAuditLog(t, db, &userID, "update", "posts", "post-2")

	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		Resource:   "posts",
		ResourceID: "post-1",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListAll with ResourceID failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAllWithDateFilters(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()

	// Insert a log entry with backdated created_at
	oldEntry := &domain.AuditLog{
		ID:       uuid.New(),
		UserID:   &userID,
		Action:   "old_action",
		Resource: "system",
	}
	if err := db.Create(oldEntry).Error; err != nil {
		t.Fatalf("Create old entry failed: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if err := db.Exec("UPDATE audit_logs SET created_at = ? WHERE id = ?", oldTime, oldEntry.ID.String()).Error; err != nil {
		t.Fatalf("failed to backdate: %v", err)
	}

	// Insert a recent entry
	seedAuditLog(t, db, &userID, "new_action", "system", "")

	startDate := time.Now().Add(-1 * time.Hour)
	endDate := time.Now().Add(1 * time.Hour)

	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		StartDate: &startDate,
		EndDate:   &endDate,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListAll with date filters failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1 (recent entry only), got %d", total)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAllPagination(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	for i := 0; i < 5; i++ {
		seedAuditLog(t, db, &userID, "action", "resource", "")
	}

	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		Offset: 2,
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("ListAll with pagination failed: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs in page, got %d", len(logs))
	}
}

func TestAuditLogRepositoryListAllClampLimit(t *testing.T) {
	db, repo := newTestAuditLogRepository(t)

	userID := uuid.New()
	for i := 0; i < 3; i++ {
		seedAuditLog(t, db, &userID, "action", "resource", "")
	}

	// Negative limit should be clamped to default
	logs, total, err := repo.ListAll(domain.AuditLogListFilter{
		Limit: -1,
	})
	if err != nil {
		t.Fatalf("ListAll with negative limit failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
	if len(logs) != 3 {
		t.Errorf("expected 3 logs with clamped limit, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByUserEmpty(t *testing.T) {
	_, repo := newTestAuditLogRepository(t)

	logs, err := repo.GetByUser(uuid.New(), 0, 10)
	if err != nil {
		t.Fatalf("GetByUser for unknown user failed: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 logs for unknown user, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByActionEmpty(t *testing.T) {
	_, repo := newTestAuditLogRepository(t)

	logs, err := repo.GetByAction("nonexistent", 0, 10)
	if err != nil {
		t.Fatalf("GetByAction for unknown action failed: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 logs for unknown action, got %d", len(logs))
	}
}

func TestAuditLogRepositoryGetByResourceEmpty(t *testing.T) {
	_, repo := newTestAuditLogRepository(t)

	logs, err := repo.GetByResource("nonexistent", "", 0, 10)
	if err != nil {
		t.Fatalf("GetByResource for unknown resource failed: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 logs for unknown resource, got %d", len(logs))
	}
}

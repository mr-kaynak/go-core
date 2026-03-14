package service

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

type auditRepoStub struct {
	createFn        func(log *domain.AuditLog) error
	getByUserFn     func(userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, error)
	getByActionFn   func(action string, offset, limit int) ([]*domain.AuditLog, error)
	getByResourceFn func(resource string, resourceID string, offset, limit int) ([]*domain.AuditLog, error)
	listAllFn       func(filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error)
}

var _ repository.AuditLogRepository = (*auditRepoStub)(nil)

func (s *auditRepoStub) Create(log *domain.AuditLog) error {
	if s.createFn != nil {
		return s.createFn(log)
	}
	return nil
}

func (s *auditRepoStub) GetByUser(userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, error) {
	if s.getByUserFn != nil {
		return s.getByUserFn(userID, offset, limit)
	}
	return nil, nil
}

func (s *auditRepoStub) GetByAction(action string, offset, limit int) ([]*domain.AuditLog, error) {
	if s.getByActionFn != nil {
		return s.getByActionFn(action, offset, limit)
	}
	return nil, nil
}

func (s *auditRepoStub) GetByResource(resource string, resourceID string, offset, limit int) ([]*domain.AuditLog, error) {
	if s.getByResourceFn != nil {
		return s.getByResourceFn(resource, resourceID, offset, limit)
	}
	return nil, nil
}

func (s *auditRepoStub) ListAll(filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
	if s.listAllFn != nil {
		return s.listAllFn(filter)
	}
	return nil, 0, nil
}

func TestAuditServiceLogAction_SerializesMetadata(t *testing.T) {
	var created *domain.AuditLog
	userID := uuid.New()
	svc := NewAuditService(&auditRepoStub{
		createFn: func(log *domain.AuditLog) error {
			created = log
			return nil
		},
	})

	svc.LogAction(
		&userID,
		ActionLogin,
		"user",
		userID.String(),
		"127.0.0.1",
		"test-agent",
		map[string]interface{}{"success": true, "method": "password"},
	)

	if created == nil {
		t.Fatalf("expected audit log to be created")
	}
	if created.UserID == nil || *created.UserID != userID {
		t.Fatalf("expected user id to be set")
	}
	if created.Action != ActionLogin {
		t.Fatalf("expected action %q, got %q", ActionLogin, created.Action)
	}
	if len(created.Metadata) == 0 {
		t.Fatalf("expected metadata to be populated")
	}
	if created.Metadata["success"] != true {
		t.Fatalf("expected metadata key 'success' = true, got %v", created.Metadata["success"])
	}
}

func TestAuditServiceLogAction_NilMetadataBecomesEmptyMap(t *testing.T) {
	var created *domain.AuditLog
	svc := NewAuditService(&auditRepoStub{
		createFn: func(log *domain.AuditLog) error {
			created = log
			return nil
		},
	})

	svc.LogAction(nil, ActionFailedLogin, "user", "", "127.0.0.1", "test-agent", nil)

	if created == nil {
		t.Fatalf("expected audit log to be created")
	}
	// nil metadata is valid; Metadata.Value() serialises it as "{}" at DB layer
	val, err := created.Metadata.Value()
	if err != nil {
		t.Fatalf("expected metadata Value() to succeed, got %v", err)
	}
	bs, ok := val.([]byte)
	if !ok || string(bs) != "{}" {
		t.Fatalf("expected metadata Value() to produce {}, got %v", val)
	}
}

func TestAuditServiceLogAction_CreateFailureIsBestEffort(t *testing.T) {
	svc := NewAuditService(&auditRepoStub{
		createFn: func(log *domain.AuditLog) error {
			return stderrors.New("db down")
		},
	})

	// Should not panic or return error
	svc.LogAction(nil, ActionLogout, "user", "", "127.0.0.1", "agent", nil)
}

func TestAuditServiceGetUserLogs_Delegates(t *testing.T) {
	userID := uuid.New()
	expected := []*domain.AuditLog{{ID: uuid.New(), Action: ActionLogin}}
	svc := NewAuditService(&auditRepoStub{
		getByUserFn: func(id uuid.UUID, offset, limit int) ([]*domain.AuditLog, error) {
			if id != userID || offset != 10 || limit != 20 {
				t.Fatalf("unexpected pagination/user args")
			}
			return expected, nil
		},
	})

	got, err := svc.GetUserLogs(userID, 10, 20)
	if err != nil {
		t.Fatalf("expected get user logs success, got %v", err)
	}
	if len(got) != 1 || got[0].Action != ActionLogin {
		t.Fatalf("unexpected logs response")
	}
}

func TestAuditServiceGetActionLogs_Delegates(t *testing.T) {
	expected := []*domain.AuditLog{{ID: uuid.New(), Action: ActionAPIKeyCreated}}
	svc := NewAuditService(&auditRepoStub{
		getByActionFn: func(action string, offset, limit int) ([]*domain.AuditLog, error) {
			if action != ActionAPIKeyCreated || offset != 0 || limit != 50 {
				t.Fatalf("unexpected action query args")
			}
			return expected, nil
		},
	})

	got, err := svc.GetActionLogs(ActionAPIKeyCreated, 0, 50)
	if err != nil {
		t.Fatalf("expected get action logs success, got %v", err)
	}
	if len(got) != 1 || got[0].Action != ActionAPIKeyCreated {
		t.Fatalf("unexpected logs response")
	}
}

func TestAuditServiceGetResourceLogs_Delegates(t *testing.T) {
	expected := []*domain.AuditLog{{ID: uuid.New(), Resource: "api_key", ResourceID: "k1"}}
	svc := NewAuditService(&auditRepoStub{
		getByResourceFn: func(resource string, resourceID string, offset, limit int) ([]*domain.AuditLog, error) {
			if resource != "api_key" || resourceID != "k1" || offset != 0 || limit != 10 {
				t.Fatalf("unexpected resource query args")
			}
			return expected, nil
		},
	})

	got, err := svc.GetResourceLogs("api_key", "k1", 0, 10)
	if err != nil {
		t.Fatalf("expected get resource logs success, got %v", err)
	}
	if len(got) != 1 || got[0].ResourceID != "k1" {
		t.Fatalf("unexpected logs response")
	}
}

// ---------------------------------------------------------------------------
// SetOnLogCreated tests
// ---------------------------------------------------------------------------

func TestAuditServiceSetOnLogCreated_HookIsInvoked(t *testing.T) {
	hookCalled := make(chan struct{}, 1)
	svc := NewAuditService(&auditRepoStub{
		createFn: func(log *domain.AuditLog) error { return nil },
	})

	svc.SetOnLogCreated(func(
		id uuid.UUID, userID *uuid.UUID,
		action, resource, resourceID, ipAddress, userAgent string,
		metadata map[string]interface{}, createdAt time.Time,
	) {
		if action != ActionLogin {
			t.Errorf("expected action %q, got %q", ActionLogin, action)
		}
		hookCalled <- struct{}{}
	})

	uid := uuid.New()
	svc.LogAction(&uid, ActionLogin, "user", uid.String(), "10.0.0.1", "agent", nil)

	select {
	case <-hookCalled:
	case <-time.After(time.Second):
		t.Fatalf("expected onLogCreated hook to be invoked")
	}
}

func TestAuditServiceSetOnLogCreated_HookNotCalledOnCreateFailure(t *testing.T) {
	hookCalled := false
	svc := NewAuditService(&auditRepoStub{
		createFn: func(log *domain.AuditLog) error {
			return stderrors.New("db down")
		},
	})

	svc.SetOnLogCreated(func(
		id uuid.UUID, userID *uuid.UUID,
		action, resource, resourceID, ipAddress, userAgent string,
		metadata map[string]interface{}, createdAt time.Time,
	) {
		hookCalled = true
	})

	svc.LogAction(nil, ActionLogout, "user", "", "10.0.0.1", "agent", nil)

	// Give goroutine a chance to fire (it shouldn't)
	time.Sleep(50 * time.Millisecond)
	if hookCalled {
		t.Fatalf("expected hook NOT to be called when create fails")
	}
}

// ---------------------------------------------------------------------------
// GetUserLogsWithTotal tests
// ---------------------------------------------------------------------------

func TestAuditServiceGetUserLogsWithTotal_Delegates(t *testing.T) {
	userID := uuid.New()
	expected := []*domain.AuditLog{{ID: uuid.New(), Action: ActionLogin}}
	svc := NewAuditService(&auditRepoStub{
		listAllFn: func(filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
			if filter.UserID == nil || *filter.UserID != userID {
				t.Fatalf("expected user id %s in filter", userID)
			}
			if filter.Offset != 5 || filter.Limit != 10 {
				t.Fatalf("expected offset=5 limit=10, got offset=%d limit=%d", filter.Offset, filter.Limit)
			}
			return expected, 25, nil
		},
	})

	got, total, err := svc.GetUserLogsWithTotal(userID, 5, 10)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(got) != 1 || got[0].Action != ActionLogin {
		t.Fatalf("unexpected logs response")
	}
	if total != 25 {
		t.Fatalf("expected total=25, got %d", total)
	}
}

func TestAuditServiceGetUserLogsWithTotal_RepoFailure(t *testing.T) {
	userID := uuid.New()
	svc := NewAuditService(&auditRepoStub{
		listAllFn: func(filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
			return nil, 0, stderrors.New("db error")
		},
	})

	_, _, err := svc.GetUserLogsWithTotal(userID, 0, 10)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListAllLogs tests
// ---------------------------------------------------------------------------

func TestAuditServiceListAllLogs_Delegates(t *testing.T) {
	expected := []*domain.AuditLog{
		{ID: uuid.New(), Action: ActionAPIKeyCreated},
		{ID: uuid.New(), Action: ActionRoleChange},
	}
	svc := NewAuditService(&auditRepoStub{
		listAllFn: func(filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
			if filter.Action != ActionAPIKeyCreated {
				t.Fatalf("expected action filter %q, got %q", ActionAPIKeyCreated, filter.Action)
			}
			if filter.Offset != 0 || filter.Limit != 50 {
				t.Fatalf("expected offset=0 limit=50, got offset=%d limit=%d", filter.Offset, filter.Limit)
			}
			return expected, 100, nil
		},
	})

	filter := domain.AuditLogListFilter{
		Action: ActionAPIKeyCreated,
		Offset: 0,
		Limit:  50,
	}
	got, total, err := svc.ListAllLogs(filter)
	if err != nil {
		t.Fatalf("expected list all logs success, got %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(got))
	}
	if total != 100 {
		t.Fatalf("expected total=100, got %d", total)
	}
}

func TestAuditServiceListAllLogs_EmptyResult(t *testing.T) {
	svc := NewAuditService(&auditRepoStub{
		listAllFn: func(filter domain.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
			return nil, 0, nil
		},
	})

	got, total, err := svc.ListAllLogs(domain.AuditLogListFilter{Offset: 0, Limit: 10})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 logs, got %d", len(got))
	}
	if total != 0 {
		t.Fatalf("expected total=0, got %d", total)
	}
}

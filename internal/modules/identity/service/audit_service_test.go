package service

import (
	stderrors "errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"github.com/mr-kaynak/go-core/internal/modules/identity/repository"
)

type auditRepoStub struct {
	createFn        func(log *domain.AuditLog) error
	getByUserFn     func(userID uuid.UUID, offset, limit int) ([]*domain.AuditLog, error)
	getByActionFn   func(action string, offset, limit int) ([]*domain.AuditLog, error)
	getByResourceFn func(resource string, resourceID string, offset, limit int) ([]*domain.AuditLog, error)
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

func (s *auditRepoStub) ListAll(_ repository.AuditLogListFilter) ([]*domain.AuditLog, int64, error) {
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
	if created.Metadata == "" {
		t.Fatalf("expected serialized metadata")
	}
}

func TestAuditServiceLogAction_MetadataMarshalFallback(t *testing.T) {
	var created *domain.AuditLog
	svc := NewAuditService(&auditRepoStub{
		createFn: func(log *domain.AuditLog) error {
			created = log
			return nil
		},
	})

	// channel cannot be marshaled to JSON
	svc.LogAction(nil, ActionFailedLogin, "user", "", "127.0.0.1", "test-agent", map[string]interface{}{
		"bad": make(chan int),
	})

	if created == nil {
		t.Fatalf("expected audit log to be created")
	}
	if created.Metadata != "{}" {
		t.Fatalf("expected metadata fallback {}, got %q", created.Metadata)
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

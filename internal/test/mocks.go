package test

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	notifDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

// MockUserRepository is a mock implementation of UserRepository
type MockUserRepository struct {
	GetByIDFunc       func(id uuid.UUID) (*domain.User, error)
	GetByEmailFunc    func(email string) (*domain.User, error)
	GetByUsernameFunc func(username string) (*domain.User, error)
	CreateFunc        func(user *domain.User) (*domain.User, error)
	UpdateFunc        func(user *domain.User) (*domain.User, error)
	DeleteFunc        func(id uuid.UUID) error
	GetAllFunc        func(offset, limit int) ([]*domain.User, error)
	CountFunc         func() (int64, error)
}

func (m *MockUserRepository) GetByID(id uuid.UUID) (*domain.User, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(id)
	}
	return nil, nil
}

func (m *MockUserRepository) GetByEmail(email string) (*domain.User, error) {
	if m.GetByEmailFunc != nil {
		return m.GetByEmailFunc(email)
	}
	return nil, nil
}

func (m *MockUserRepository) GetByUsername(username string) (*domain.User, error) {
	if m.GetByUsernameFunc != nil {
		return m.GetByUsernameFunc(username)
	}
	return nil, nil
}

func (m *MockUserRepository) Create(user *domain.User) (*domain.User, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(user)
	}
	return user, nil
}

func (m *MockUserRepository) Update(user *domain.User) (*domain.User, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(user)
	}
	return user, nil
}

func (m *MockUserRepository) Delete(id uuid.UUID) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(id)
	}
	return nil
}

func (m *MockUserRepository) GetAll(offset, limit int) ([]*domain.User, error) {
	if m.GetAllFunc != nil {
		return m.GetAllFunc(offset, limit)
	}
	return []*domain.User{}, nil
}

func (m *MockUserRepository) Count() (int64, error) {
	if m.CountFunc != nil {
		return m.CountFunc()
	}
	return 0, nil
}

// MockVerificationTokenRepository is a mock implementation of VerificationTokenRepository
type MockVerificationTokenRepository struct {
	CreateFunc         func(token *domain.VerificationToken) (*domain.VerificationToken, error)
	GetByTokenFunc     func(token string) (*domain.VerificationToken, error)
	MarkAsVerifiedFunc func(token string) error
	DeleteFunc         func(id uuid.UUID) error
}

func (m *MockVerificationTokenRepository) Create(token *domain.VerificationToken) (*domain.VerificationToken, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(token)
	}
	return token, nil
}

func (m *MockVerificationTokenRepository) GetByToken(token string) (*domain.VerificationToken, error) {
	if m.GetByTokenFunc != nil {
		return m.GetByTokenFunc(token)
	}
	return nil, nil
}

func (m *MockVerificationTokenRepository) MarkAsVerified(token string) error {
	if m.MarkAsVerifiedFunc != nil {
		return m.MarkAsVerifiedFunc(token)
	}
	return nil
}

func (m *MockVerificationTokenRepository) Delete(id uuid.UUID) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(id)
	}
	return nil
}

// MockEmailService is a mock implementation of EmailService
type MockEmailService struct {
	SendFunc                   func(data interface{}) error
	SendVerificationEmailFunc  func(to, username, token string) error
	SendPasswordResetEmailFunc func(to, username, token string) error
	SendWelcomeEmailFunc       func(to, username string) error
	SendNotificationFunc       func(to, subject, message string) error
}

func (m *MockEmailService) Send(data interface{}) error {
	if m.SendFunc != nil {
		return m.SendFunc(data)
	}
	return nil
}

func (m *MockEmailService) SendVerificationEmail(to, username, token string) error {
	if m.SendVerificationEmailFunc != nil {
		return m.SendVerificationEmailFunc(to, username, token)
	}
	return nil
}

func (m *MockEmailService) SendPasswordResetEmail(to, username, token string) error {
	if m.SendPasswordResetEmailFunc != nil {
		return m.SendPasswordResetEmailFunc(to, username, token)
	}
	return nil
}

func (m *MockEmailService) SendWelcomeEmail(to, username string) error {
	if m.SendWelcomeEmailFunc != nil {
		return m.SendWelcomeEmailFunc(to, username)
	}
	return nil
}

func (m *MockEmailService) SendNotification(to, subject, message string) error {
	if m.SendNotificationFunc != nil {
		return m.SendNotificationFunc(to, subject, message)
	}
	return nil
}

// MockNotificationRepository is a mock implementation of NotificationRepository
type MockNotificationRepository struct {
	CreateFunc  func(notif *notifDomain.Notification) (*notifDomain.Notification, error)
	GetByIDFunc func(id uuid.UUID) (*notifDomain.Notification, error)
	UpdateFunc  func(notif *notifDomain.Notification) (*notifDomain.Notification, error)
	DeleteFunc  func(id uuid.UUID) error
}

func (m *MockNotificationRepository) Create(notif *notifDomain.Notification) (*notifDomain.Notification, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(notif)
	}
	return notif, nil
}

func (m *MockNotificationRepository) GetByID(id uuid.UUID) (*notifDomain.Notification, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(id)
	}
	return nil, nil
}

func (m *MockNotificationRepository) Update(notif *notifDomain.Notification) (*notifDomain.Notification, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(notif)
	}
	return notif, nil
}

func (m *MockNotificationRepository) Delete(id uuid.UUID) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(id)
	}
	return nil
}

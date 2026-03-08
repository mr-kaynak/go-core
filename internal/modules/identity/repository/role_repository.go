package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
)

// RoleRepository defines role data access interface
type RoleRepository interface {
	Create(role *domain.Role) error
	GetByID(id uuid.UUID) (*domain.Role, error)
	GetByName(name string) (*domain.Role, error)
	GetAll(offset, limit int) ([]domain.Role, error)
	Count() (int64, error)
	Update(role *domain.Role) error
	Delete(id uuid.UUID) error
}

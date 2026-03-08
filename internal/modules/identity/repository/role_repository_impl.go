package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// roleRepositoryImpl implements RoleRepository
type roleRepositoryImpl struct {
	db *gorm.DB
}

// NewRoleRepository creates a new role repository
func NewRoleRepository(db *gorm.DB) RoleRepository {
	return &roleRepositoryImpl{
		db: db,
	}
}

// Create creates a new role
func (r *roleRepositoryImpl) Create(role *domain.Role) error {
	return r.db.Create(role).Error
}

// GetByID gets a role by ID
func (r *roleRepositoryImpl) GetByID(id uuid.UUID) (*domain.Role, error) {
	var role domain.Role
	if err := r.db.Where("id = ?", id).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetByName gets a role by name
func (r *roleRepositoryImpl) GetByName(name string) (*domain.Role, error) {
	var role domain.Role
	if err := r.db.Where("name = ?", name).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetAll gets all roles with pagination
func (r *roleRepositoryImpl) GetAll(offset, limit int) ([]domain.Role, error) {
	limit = clampLimit(limit)
	var roles []domain.Role
	if err := r.db.Offset(offset).Limit(limit).Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// Count counts total roles
func (r *roleRepositoryImpl) Count() (int64, error) {
	var count int64
	if err := r.db.Model(&domain.Role{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Update updates a role
func (r *roleRepositoryImpl) Update(role *domain.Role) error {
	return r.db.Save(role).Error
}

// Delete deletes a role
func (r *roleRepositoryImpl) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Role{}, id).Error
}

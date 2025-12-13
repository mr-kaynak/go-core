package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

// RoleRepositoryImpl implements RoleRepository
type RoleRepositoryImpl struct {
	db *gorm.DB
}

// NewRoleRepository creates a new role repository
func NewRoleRepository(db *gorm.DB) RoleRepository {
	return &RoleRepositoryImpl{
		db: db,
	}
}

// Create creates a new role
func (r *RoleRepositoryImpl) Create(role *domain.Role) error {
	return r.db.Create(role).Error
}

// GetByID gets a role by ID
func (r *RoleRepositoryImpl) GetByID(id uuid.UUID) (*domain.Role, error) {
	var role domain.Role
	if err := r.db.Where("id = ?", id).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetByName gets a role by name
func (r *RoleRepositoryImpl) GetByName(name string) (*domain.Role, error) {
	var role domain.Role
	if err := r.db.Where("name = ?", name).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetAll gets all roles with pagination
func (r *RoleRepositoryImpl) GetAll(offset, limit int) ([]domain.Role, error) {
	var roles []domain.Role
	if err := r.db.Offset(offset).Limit(limit).Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// Count counts total roles
func (r *RoleRepositoryImpl) Count() (int64, error) {
	var count int64
	if err := r.db.Model(&domain.Role{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Update updates a role
func (r *RoleRepositoryImpl) Update(role *domain.Role) error {
	return r.db.Save(role).Error
}

// Delete deletes a role
func (r *RoleRepositoryImpl) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Role{}, id).Error
}

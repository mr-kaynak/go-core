package repository

import (
	"context"

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
func (r *roleRepositoryImpl) Create(ctx context.Context, role *domain.Role) error {
	db := r.db.WithContext(ctx)
	return db.Create(role).Error
}

// GetByID gets a role by ID
func (r *roleRepositoryImpl) GetByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	db := r.db.WithContext(ctx)
	var role domain.Role
	if err := db.Where("id = ?", id).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetByName gets a role by name
func (r *roleRepositoryImpl) GetByName(ctx context.Context, name string) (*domain.Role, error) {
	db := r.db.WithContext(ctx)
	var role domain.Role
	if err := db.Where("name = ?", name).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// GetAll gets all roles with pagination
func (r *roleRepositoryImpl) GetAll(ctx context.Context, offset, limit int) ([]domain.Role, error) {
	db := r.db.WithContext(ctx)
	limit = clampLimit(limit)
	var roles []domain.Role
	if err := db.Offset(offset).Limit(limit).Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// Count counts total roles
func (r *roleRepositoryImpl) Count(ctx context.Context) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	if err := db.Model(&domain.Role{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Update updates a role
func (r *roleRepositoryImpl) Update(ctx context.Context, role *domain.Role) error {
	db := r.db.WithContext(ctx)
	return db.Save(role).Error
}

// Delete deletes a role
func (r *roleRepositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.Role{}, id).Error
}

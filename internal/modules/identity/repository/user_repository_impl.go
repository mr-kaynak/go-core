package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// allowedSortFields is a whitelist of column names that can be used for sorting
// to prevent SQL injection through the sort_by parameter.
var allowedSortFields = map[string]bool{
	"created_at": true,
	"updated_at": true,
	"email":      true,
	"username":   true,
	"first_name": true,
	"last_name":  true,
}

// userRepositoryImpl implements UserRepository using GORM
type userRepositoryImpl struct {
	db *gorm.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepositoryImpl{
		db: db,
	}
}

// WithTx returns a new repository instance that uses the given transaction.
// This satisfies both UserRepository.WithTx and UserWriter.WithTx since
// *userRepositoryImpl implements both interfaces.
func (r *userRepositoryImpl) WithTx(tx *gorm.DB) UserRepository {
	if tx == nil {
		return r
	}
	return &userRepositoryImpl{db: tx}
}

// Create creates a new user
func (r *userRepositoryImpl) Create(ctx context.Context, user *domain.User) error {
	db := r.db.WithContext(ctx)
	return db.Create(user).Error
}

// Update updates an existing user
func (r *userRepositoryImpl) Update(ctx context.Context, user *domain.User) error {
	db := r.db.WithContext(ctx)
	return db.Save(user).Error
}

// Delete soft deletes a user
func (r *userRepositoryImpl) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.User{}, id).Error
}

// GetByID retrieves a user by ID
func (r *userRepositoryImpl) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	db := r.db.WithContext(ctx)
	var user domain.User
	err := db.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByIDs retrieves multiple users in one query using WHERE id IN (...).
// The returned slice preserves the order of the database result, not the order of ids.
// Missing IDs are silently omitted; callers should build a map by ID for O(1) lookup.
func (r *userRepositoryImpl) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*domain.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	db := r.db.WithContext(ctx)
	var users []*domain.User
	err := db.Where("id IN ?", ids).Find(&users).Error
	return users, err
}

// GetByIDForUpdate retrieves a user by ID with a row-level lock (SELECT ... FOR UPDATE).
// Must be called within a transaction.
func (r *userRepositoryImpl) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	db := r.db.WithContext(ctx)
	var user domain.User
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByEmail retrieves a user by email
func (r *userRepositoryImpl) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	db := r.db.WithContext(ctx)
	var user domain.User
	err := db.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByUsername retrieves a user by username
func (r *userRepositoryImpl) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	db := r.db.WithContext(ctx)
	var user domain.User
	err := db.Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetAll retrieves all users with pagination
func (r *userRepositoryImpl) GetAll(ctx context.Context, offset, limit int) ([]*domain.User, error) {
	db := r.db.WithContext(ctx)
	limit = clampLimit(limit)
	var users []*domain.User
	err := db.Offset(offset).Limit(limit).Find(&users).Error
	return users, err
}

// Count returns the total number of users
func (r *userRepositoryImpl) Count(ctx context.Context) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.User{}).Count(&count).Error
	return count, err
}

// ExistsByEmail checks if a user with the given email exists
func (r *userRepositoryImpl) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.User{}).Where("email = ?", email).Count(&count).Error
	return count > 0, err
}

// ExistsByUsername checks if a user with the given username exists
func (r *userRepositoryImpl) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}

// LoadRoles loads the roles for a user
func (r *userRepositoryImpl) LoadRoles(ctx context.Context, user *domain.User) error {
	db := r.db.WithContext(ctx)
	return db.Preload("Roles.Permissions").First(user, user.ID).Error
}

// CreateRole creates a new role
func (r *userRepositoryImpl) CreateRole(ctx context.Context, role *domain.Role) error {
	db := r.db.WithContext(ctx)
	return db.Create(role).Error
}

// UpdateRole updates an existing role
func (r *userRepositoryImpl) UpdateRole(ctx context.Context, role *domain.Role) error {
	db := r.db.WithContext(ctx)
	return db.Save(role).Error
}

// DeleteRole soft deletes a role
func (r *userRepositoryImpl) DeleteRole(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.Role{}, id).Error
}

// GetRoleByID retrieves a role by ID
func (r *userRepositoryImpl) GetRoleByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	db := r.db.WithContext(ctx)
	var role domain.Role
	err := db.Preload("Permissions").First(&role, id).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// GetRoleByName retrieves a role by name
func (r *userRepositoryImpl) GetRoleByName(ctx context.Context, name string) (*domain.Role, error) {
	db := r.db.WithContext(ctx)
	var role domain.Role
	err := db.Where("name = ?", name).First(&role).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// GetAllRoles retrieves all roles
func (r *userRepositoryImpl) GetAllRoles(ctx context.Context) ([]*domain.Role, error) {
	db := r.db.WithContext(ctx)
	var roles []*domain.Role
	err := db.Preload("Permissions").Find(&roles).Error
	return roles, err
}

// AssignRole assigns a role to a user
func (r *userRepositoryImpl) AssignRole(ctx context.Context, userID, roleID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		user := &domain.User{ID: userID}
		role := &domain.Role{ID: roleID}
		return tx.Model(user).Association("Roles").Append(role)
	})
}

// RemoveRole removes a role from a user
func (r *userRepositoryImpl) RemoveRole(ctx context.Context, userID, roleID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		user := &domain.User{ID: userID}
		role := &domain.Role{ID: roleID}
		return tx.Model(user).Association("Roles").Delete(role)
	})
}

// toPointerSlice converts a slice of values into a slice of pointers to its elements.
func toPointerSlice[T any](items []T) []*T {
	out := make([]*T, len(items))
	for i := range items {
		out[i] = &items[i]
	}
	return out
}

// GetUserRoles retrieves all roles for a user
func (r *userRepositoryImpl) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]*domain.Role, error) {
	db := r.db.WithContext(ctx)
	var user domain.User
	if err := db.Preload("Roles.Permissions").First(&user, userID).Error; err != nil {
		return nil, err
	}
	return toPointerSlice(user.Roles), nil
}

// CreatePermission creates a new permission
func (r *userRepositoryImpl) CreatePermission(ctx context.Context, permission *domain.Permission) error {
	db := r.db.WithContext(ctx)
	return db.Create(permission).Error
}

// UpdatePermission updates an existing permission
func (r *userRepositoryImpl) UpdatePermission(ctx context.Context, permission *domain.Permission) error {
	db := r.db.WithContext(ctx)
	return db.Save(permission).Error
}

// DeletePermission soft deletes a permission
func (r *userRepositoryImpl) DeletePermission(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Delete(&domain.Permission{}, id).Error
}

// GetPermissionByID retrieves a permission by ID
func (r *userRepositoryImpl) GetPermissionByID(ctx context.Context, id uuid.UUID) (*domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var permission domain.Permission
	err := db.First(&permission, id).Error
	if err != nil {
		return nil, err
	}
	return &permission, nil
}

// GetAllPermissions retrieves all permissions
func (r *userRepositoryImpl) GetAllPermissions(ctx context.Context) ([]*domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var permissions []*domain.Permission
	err := db.Find(&permissions).Error
	return permissions, err
}

// AssignPermissionToRole assigns a permission to a role
func (r *userRepositoryImpl) AssignPermissionToRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	role := &domain.Role{ID: roleID}
	permission := &domain.Permission{ID: permissionID}
	return db.Model(role).Association("Permissions").Append(permission)
}

// RemovePermissionFromRole removes a permission from a role
func (r *userRepositoryImpl) RemovePermissionFromRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	role := &domain.Role{ID: roleID}
	permission := &domain.Permission{ID: permissionID}
	return db.Model(role).Association("Permissions").Delete(permission)
}

// GetRolePermissions retrieves all permissions for a role
func (r *userRepositoryImpl) GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]*domain.Permission, error) {
	db := r.db.WithContext(ctx)
	var role domain.Role
	if err := db.Preload("Permissions").First(&role, roleID).Error; err != nil {
		return nil, err
	}
	return toPointerSlice(role.Permissions), nil
}

// CreateRefreshToken creates a new refresh token
func (r *userRepositoryImpl) CreateRefreshToken(ctx context.Context, token *domain.RefreshToken) error {
	db := r.db.WithContext(ctx)
	return db.Create(token).Error
}

// GetRefreshToken retrieves a refresh token
func (r *userRepositoryImpl) GetRefreshToken(ctx context.Context, token string) (*domain.RefreshToken, error) {
	db := r.db.WithContext(ctx)
	var refreshToken domain.RefreshToken
	err := db.Where("token = ? AND revoked = ? AND expires_at > ?", token, false, time.Now()).First(&refreshToken).Error
	if err != nil {
		return nil, err
	}
	return &refreshToken, nil
}

// RevokeRefreshToken revokes a refresh token
func (r *userRepositoryImpl) RevokeRefreshToken(ctx context.Context, token string) error {
	db := r.db.WithContext(ctx)
	result := db.Model(&domain.RefreshToken{}).Where("token = ?", token).Update("revoked", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// RevokeAllUserRefreshTokens revokes all refresh tokens for a user
func (r *userRepositoryImpl) RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Model(&domain.RefreshToken{}).Where("user_id = ?", userID).Update("revoked", true).Error
}

// ListFiltered retrieves users matching the given filter with total count.
func (r *userRepositoryImpl) ListFiltered(ctx context.Context, filter domain.UserListFilter) ([]*domain.User, int64, error) {
	db := r.db.WithContext(ctx)
	query := db.Model(&domain.User{})

	// Search filter
	if filter.Search != "" {
		like := "%" + filter.Search + "%"
		query = query.Where(
			"email ILIKE ? OR username ILIKE ? OR first_name ILIKE ? OR last_name ILIKE ?",
			like, like, like, like,
		)
	}

	// Only active filter
	if filter.OnlyActive {
		query = query.Where("status = ? AND verified = ?", domain.UserStatusActive, true)
	}

	// Roles filter via subquery
	if len(filter.Roles) > 0 {
		query = query.Where(
			"id IN (SELECT user_id FROM user_roles JOIN roles ON roles.id = user_roles.role_id WHERE roles.name IN ? AND roles.deleted_at IS NULL)",
			filter.Roles,
		)
	}

	// Count total before pagination
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Sort
	orderClause := "created_at DESC"
	if filter.SortBy != "" && allowedSortFields[filter.SortBy] {
		dir := "DESC"
		if strings.EqualFold(filter.Order, "asc") {
			dir = "ASC"
		}
		orderClause = fmt.Sprintf("%s %s", filter.SortBy, dir)
	}
	query = query.Order(orderClause)

	// Pagination
	var users []*domain.User
	err := query.Offset(filter.Offset).Limit(clampLimit(filter.Limit)).Find(&users).Error
	return users, total, err
}

// GetActiveRefreshTokensByUser retrieves active (non-revoked, non-expired) refresh tokens for a user
func (r *userRepositoryImpl) GetActiveRefreshTokensByUser(ctx context.Context, userID uuid.UUID) ([]*domain.RefreshToken, error) {
	db := r.db.WithContext(ctx)
	var tokens []*domain.RefreshToken
	err := db.Where("user_id = ? AND revoked = false AND expires_at > ?", userID, time.Now()).
		Order("created_at DESC").
		Find(&tokens).Error
	return tokens, err
}

// RevokeRefreshTokenByID revokes a single refresh token by its ID
func (r *userRepositoryImpl) RevokeRefreshTokenByID(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Model(&domain.RefreshToken{}).Where("id = ?", id).Update("revoked", true).Error
}

// CleanExpiredRefreshTokens removes expired refresh tokens
func (r *userRepositoryImpl) CleanExpiredRefreshTokens(ctx context.Context) error {
	db := r.db.WithContext(ctx)
	return db.Where("expires_at < ? OR revoked = ?", time.Now(), true).Delete(&domain.RefreshToken{}).Error
}

func (r *userRepositoryImpl) CountByStatus(ctx context.Context, status string) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.User{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

func (r *userRepositoryImpl) CountCreatedAfter(ctx context.Context, after time.Time) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.User{}).Where("created_at >= ?", after).Count(&count).Error
	return count, err
}

func (r *userRepositoryImpl) GetAllActiveSessions(
	ctx context.Context, offset, limit int, userID *uuid.UUID,
) ([]*domain.RefreshToken, error) {
	db := r.db.WithContext(ctx)
	limit = clampLimit(limit)
	var tokens []*domain.RefreshToken
	query := db.Where("revoked = false AND expires_at > ?", time.Now())
	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}
	err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&tokens).Error
	return tokens, err
}

func (r *userRepositoryImpl) CountActiveSessions(ctx context.Context, userID *uuid.UUID) (int64, error) {
	db := r.db.WithContext(ctx)
	var count int64
	query := db.Model(&domain.RefreshToken{}).
		Where("revoked = false AND expires_at > ?", time.Now())
	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}
	err := query.Count(&count).Error
	return count, err
}

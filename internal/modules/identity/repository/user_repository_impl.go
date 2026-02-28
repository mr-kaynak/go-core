package repository

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
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

// WithTx returns a new repository instance that uses the given transaction
func (r *userRepositoryImpl) WithTx(tx *gorm.DB) UserRepository {
	if tx == nil {
		return r
	}
	return &userRepositoryImpl{db: tx}
}

// Create creates a new user
func (r *userRepositoryImpl) Create(user *domain.User) error {
	return r.db.Create(user).Error
}

// Update updates an existing user
func (r *userRepositoryImpl) Update(user *domain.User) error {
	return r.db.Save(user).Error
}

// Delete soft deletes a user
func (r *userRepositoryImpl) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.User{}, id).Error
}

// GetByID retrieves a user by ID
func (r *userRepositoryImpl) GetByID(id uuid.UUID) (*domain.User, error) {
	var user domain.User
	err := r.db.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByEmail retrieves a user by email
func (r *userRepositoryImpl) GetByEmail(email string) (*domain.User, error) {
	var user domain.User
	err := r.db.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByUsername retrieves a user by username
func (r *userRepositoryImpl) GetByUsername(username string) (*domain.User, error) {
	var user domain.User
	err := r.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetAll retrieves all users with pagination
func (r *userRepositoryImpl) GetAll(offset, limit int) ([]*domain.User, error) {
	var users []*domain.User
	err := r.db.Offset(offset).Limit(limit).Find(&users).Error
	return users, err
}

// Count returns the total number of users
func (r *userRepositoryImpl) Count() (int64, error) {
	var count int64
	err := r.db.Model(&domain.User{}).Count(&count).Error
	return count, err
}

// ExistsByEmail checks if a user with the given email exists
func (r *userRepositoryImpl) ExistsByEmail(email string) (bool, error) {
	var count int64
	err := r.db.Model(&domain.User{}).Where("email = ?", email).Count(&count).Error
	return count > 0, err
}

// ExistsByUsername checks if a user with the given username exists
func (r *userRepositoryImpl) ExistsByUsername(username string) (bool, error) {
	var count int64
	err := r.db.Model(&domain.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}

// LoadRoles loads the roles for a user
func (r *userRepositoryImpl) LoadRoles(user *domain.User) error {
	return r.db.Preload("Roles.Permissions").First(user, user.ID).Error
}

// CreateRole creates a new role
func (r *userRepositoryImpl) CreateRole(role *domain.Role) error {
	return r.db.Create(role).Error
}

// UpdateRole updates an existing role
func (r *userRepositoryImpl) UpdateRole(role *domain.Role) error {
	return r.db.Save(role).Error
}

// DeleteRole soft deletes a role
func (r *userRepositoryImpl) DeleteRole(id uuid.UUID) error {
	return r.db.Delete(&domain.Role{}, id).Error
}

// GetRoleByID retrieves a role by ID
func (r *userRepositoryImpl) GetRoleByID(id uuid.UUID) (*domain.Role, error) {
	var role domain.Role
	err := r.db.Preload("Permissions").First(&role, id).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// GetRoleByName retrieves a role by name
func (r *userRepositoryImpl) GetRoleByName(name string) (*domain.Role, error) {
	var role domain.Role
	err := r.db.Where("name = ?", name).First(&role).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// GetAllRoles retrieves all roles
func (r *userRepositoryImpl) GetAllRoles() ([]*domain.Role, error) {
	var roles []*domain.Role
	err := r.db.Preload("Permissions").Find(&roles).Error
	return roles, err
}

// AssignRole assigns a role to a user
func (r *userRepositoryImpl) AssignRole(userID, roleID uuid.UUID) error {
	user := &domain.User{ID: userID}
	role := &domain.Role{ID: roleID}
	return r.db.Model(user).Association("Roles").Append(role)
}

// RemoveRole removes a role from a user
func (r *userRepositoryImpl) RemoveRole(userID, roleID uuid.UUID) error {
	user := &domain.User{ID: userID}
	role := &domain.Role{ID: roleID}
	return r.db.Model(user).Association("Roles").Delete(role)
}

// GetUserRoles retrieves all roles for a user
func (r *userRepositoryImpl) GetUserRoles(userID uuid.UUID) ([]*domain.Role, error) {
	var user domain.User
	err := r.db.Preload("Roles.Permissions").First(&user, userID).Error
	if err != nil {
		return nil, err
	}
	// Convert []Role to []*Role
	roles := make([]*domain.Role, len(user.Roles))
	for i := range user.Roles {
		roles[i] = &user.Roles[i]
	}
	return roles, nil
}

// CreatePermission creates a new permission
func (r *userRepositoryImpl) CreatePermission(permission *domain.Permission) error {
	return r.db.Create(permission).Error
}

// UpdatePermission updates an existing permission
func (r *userRepositoryImpl) UpdatePermission(permission *domain.Permission) error {
	return r.db.Save(permission).Error
}

// DeletePermission soft deletes a permission
func (r *userRepositoryImpl) DeletePermission(id uuid.UUID) error {
	return r.db.Delete(&domain.Permission{}, id).Error
}

// GetPermissionByID retrieves a permission by ID
func (r *userRepositoryImpl) GetPermissionByID(id uuid.UUID) (*domain.Permission, error) {
	var permission domain.Permission
	err := r.db.First(&permission, id).Error
	if err != nil {
		return nil, err
	}
	return &permission, nil
}

// GetAllPermissions retrieves all permissions
func (r *userRepositoryImpl) GetAllPermissions() ([]*domain.Permission, error) {
	var permissions []*domain.Permission
	err := r.db.Find(&permissions).Error
	return permissions, err
}

// AssignPermissionToRole assigns a permission to a role
func (r *userRepositoryImpl) AssignPermissionToRole(roleID, permissionID uuid.UUID) error {
	role := &domain.Role{ID: roleID}
	permission := &domain.Permission{ID: permissionID}
	return r.db.Model(role).Association("Permissions").Append(permission)
}

// RemovePermissionFromRole removes a permission from a role
func (r *userRepositoryImpl) RemovePermissionFromRole(roleID, permissionID uuid.UUID) error {
	role := &domain.Role{ID: roleID}
	permission := &domain.Permission{ID: permissionID}
	return r.db.Model(role).Association("Permissions").Delete(permission)
}

// GetRolePermissions retrieves all permissions for a role
func (r *userRepositoryImpl) GetRolePermissions(roleID uuid.UUID) ([]*domain.Permission, error) {
	var role domain.Role
	err := r.db.Preload("Permissions").First(&role, roleID).Error
	if err != nil {
		return nil, err
	}
	// Convert []Permission to []*Permission
	permissions := make([]*domain.Permission, len(role.Permissions))
	for i := range role.Permissions {
		permissions[i] = &role.Permissions[i]
	}
	return permissions, nil
}

// CreateRefreshToken creates a new refresh token
func (r *userRepositoryImpl) CreateRefreshToken(token *domain.RefreshToken) error {
	return r.db.Create(token).Error
}

// GetRefreshToken retrieves a refresh token
func (r *userRepositoryImpl) GetRefreshToken(token string) (*domain.RefreshToken, error) {
	var refreshToken domain.RefreshToken
	err := r.db.Where("token = ? AND revoked = ? AND expires_at > ?", token, false, time.Now()).First(&refreshToken).Error
	if err != nil {
		return nil, err
	}
	return &refreshToken, nil
}

// RevokeRefreshToken revokes a refresh token
func (r *userRepositoryImpl) RevokeRefreshToken(token string) error {
	return r.db.Model(&domain.RefreshToken{}).Where("token = ?", token).Update("revoked", true).Error
}

// RevokeAllUserRefreshTokens revokes all refresh tokens for a user
func (r *userRepositoryImpl) RevokeAllUserRefreshTokens(userID uuid.UUID) error {
	return r.db.Model(&domain.RefreshToken{}).Where("user_id = ?", userID).Update("revoked", true).Error
}

// ListFiltered retrieves users matching the given filter with total count.
func (r *userRepositoryImpl) ListFiltered(filter UserListFilter) ([]*domain.User, int64, error) {
	query := r.db.Model(&domain.User{})

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
			"id IN (SELECT user_id FROM user_roles JOIN roles ON roles.id = user_roles.role_id WHERE roles.name IN ?)",
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
	err := query.Offset(filter.Offset).Limit(filter.Limit).Find(&users).Error
	return users, total, err
}

// GetActiveRefreshTokensByUser retrieves active (non-revoked, non-expired) refresh tokens for a user
func (r *userRepositoryImpl) GetActiveRefreshTokensByUser(userID uuid.UUID) ([]*domain.RefreshToken, error) {
	var tokens []*domain.RefreshToken
	err := r.db.Where("user_id = ? AND revoked = false AND expires_at > ?", userID, time.Now()).
		Order("created_at DESC").
		Find(&tokens).Error
	return tokens, err
}

// RevokeRefreshTokenByID revokes a single refresh token by its ID
func (r *userRepositoryImpl) RevokeRefreshTokenByID(id uuid.UUID) error {
	return r.db.Model(&domain.RefreshToken{}).Where("id = ?", id).Update("revoked", true).Error
}

// CleanExpiredRefreshTokens removes expired refresh tokens
func (r *userRepositoryImpl) CleanExpiredRefreshTokens() error {
	return r.db.Where("expires_at < ? OR revoked = ?", time.Now(), true).Delete(&domain.RefreshToken{}).Error
}

func (r *userRepositoryImpl) CountByStatus(status string) (int64, error) {
	var count int64
	err := r.db.Model(&domain.User{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

func (r *userRepositoryImpl) CountCreatedAfter(after time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&domain.User{}).Where("created_at >= ?", after).Count(&count).Error
	return count, err
}

func (r *userRepositoryImpl) GetAllActiveSessions(offset, limit int) ([]*domain.RefreshToken, error) {
	var tokens []*domain.RefreshToken
	err := r.db.Where("revoked = false AND expires_at > ?", time.Now()).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&tokens).Error
	return tokens, err
}

func (r *userRepositoryImpl) CountActiveSessions() (int64, error) {
	var count int64
	err := r.db.Model(&domain.RefreshToken{}).
		Where("revoked = false AND expires_at > ?", time.Now()).
		Count(&count).Error
	return count, err
}

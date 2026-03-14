package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/identity/domain"
	"gorm.io/gorm"
)

func newTestUserRepository(t *testing.T) (*gorm.DB, UserRepository) {
	t.Helper()
	db := setupTestDB(t)
	return db, NewUserRepository(db)
}

func TestUserRepositoryWithTx(t *testing.T) {
	db, repo := newTestUserRepository(t)

	impl := repo.(*userRepositoryImpl)

	if repo.WithTx(nil) != repo {
		t.Fatalf("expected WithTx(nil) to return same repository instance")
	}

	tx := db.Begin()
	defer tx.Rollback()

	txRepo := repo.WithTx(tx)
	if txRepo == repo {
		t.Fatalf("expected WithTx(tx) to return new repository instance")
	}

	if txImpl, ok := txRepo.(*userRepositoryImpl); !ok || txImpl.db != tx {
		t.Fatalf("expected WithTx(tx) to bind repository to transaction db")
	}

	if impl.db == tx {
		t.Fatalf("original repository must not be mutated by WithTx")
	}
}

func TestUserRepositoryCRUD(t *testing.T) {
	db, repo := newTestUserRepository(t)

	user := &domain.User{
		ID:       uuid.New(),
		Email:    "create@example.com",
		Username: "create-user",
		Password: "password",
		Status:   domain.UserStatusActive,
		Verified: true,
	}

	if err := repo.Create(user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fetched, err := repo.GetByID(user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if fetched.Email != user.Email {
		t.Errorf("GetByID returned wrong email, got %q want %q", fetched.Email, user.Email)
	}

	user.FirstName = "Updated"
	if err := repo.Update(user); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updated, err := repo.GetByID(user.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if updated.FirstName != "Updated" {
		t.Errorf("expected FirstName to be updated, got %q", updated.FirstName)
	}

	if err := repo.Delete(user.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	if err := db.Model(&domain.User{}).Where("id = ?", user.ID).Count(&count).Error; err != nil {
		t.Fatalf("count after delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected user to be soft deleted, found %d records", count)
	}
}

func TestUserRepositoryGettersAndExists(t *testing.T) {
	_, repo := newTestUserRepository(t)

	user := &domain.User{
		ID:       uuid.New(),
		Email:    "lookup@example.com",
		Username: "lookup-user",
		Password: "password",
		Status:   domain.UserStatusActive,
		Verified: true,
	}

	if err := repo.Create(user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	byEmail, err := repo.GetByEmail(user.Email)
	if err != nil {
		t.Fatalf("GetByEmail failed: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Errorf("GetByEmail returned wrong user ID")
	}

	byUsername, err := repo.GetByUsername(user.Username)
	if err != nil {
		t.Fatalf("GetByUsername failed: %v", err)
	}
	if byUsername.ID != user.ID {
		t.Errorf("GetByUsername returned wrong user ID")
	}

	existsEmail, err := repo.ExistsByEmail(user.Email)
	if err != nil {
		t.Fatalf("ExistsByEmail failed: %v", err)
	}
	if !existsEmail {
		t.Errorf("expected ExistsByEmail to be true")
	}

	existsUsername, err := repo.ExistsByUsername(user.Username)
	if err != nil {
		t.Fatalf("ExistsByUsername failed: %v", err)
	}
	if !existsUsername {
		t.Errorf("expected ExistsByUsername to be true")
	}

	notExists, err := repo.ExistsByEmail("missing@example.com")
	if err != nil {
		t.Fatalf("ExistsByEmail for missing user failed: %v", err)
	}
	if notExists {
		t.Errorf("expected ExistsByEmail for missing user to be false")
	}
}

func TestUserRepositoryGetAllAndCount(t *testing.T) {
	_, repo := newTestUserRepository(t)

	for i := 0; i < 3; i++ {
		user := &domain.User{
			ID:       uuid.New(),
			Email:    uuid.New().String() + "@example.com",
			Username: "user-" + uuid.New().String(),
			Password: "password",
			Status:   domain.UserStatusActive,
			Verified: true,
		}
		if err := repo.Create(user); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	users, err := repo.GetAll(0, 10)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("expected 3 users, got %d", len(users))
	}

	count, err := repo.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestUserRepositoryLoadRolesAndPermissions(t *testing.T) {
	db, repo := newTestUserRepository(t)

	user := seedUser(t, db, "roles@example.com", "role-user")
	role := seedRole(t, db, "admin")
	perm := seedPermission(t, db, "identity.manage")

	if err := db.Exec("INSERT INTO role_permissions (role_id, permission_id, created_at) VALUES (?, ?, datetime('now'))",
		role.ID.String(), perm.ID.String()).Error; err != nil {
		t.Fatalf("failed to associate permission to role: %v", err)
	}
	if err := db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
		user.ID.String(), role.ID.String()).Error; err != nil {
		t.Fatalf("failed to associate role to user: %v", err)
	}

	if err := repo.LoadRoles(user); err != nil {
		t.Fatalf("LoadRoles failed: %v", err)
	}

	if len(user.Roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(user.Roles))
	}
	if user.Roles[0].Name != role.Name {
		t.Errorf("expected role name %q, got %q", role.Name, user.Roles[0].Name)
	}
	if len(user.Roles[0].Permissions) != 1 || user.Roles[0].Permissions[0].Name != perm.Name {
		t.Errorf("expected permission %q to be preloaded", perm.Name)
	}
}

func TestUserRepositoryAssignAndRemoveRole(t *testing.T) {
	db, repo := newTestUserRepository(t)

	user := seedUser(t, db, "assign@example.com", "assign-user")
	role := seedRole(t, db, "member")

	if err := repo.AssignRole(user.ID, role.ID); err != nil {
		t.Fatalf("AssignRole failed: %v", err)
	}

	roles, err := repo.GetUserRoles(user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles failed: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != role.ID {
		t.Fatalf("expected user to have assigned role")
	}

	if err := repo.RemoveRole(user.ID, role.ID); err != nil {
		t.Fatalf("RemoveRole failed: %v", err)
	}

	roles, err = repo.GetUserRoles(user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles after removal failed: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("expected no roles after removal, got %d", len(roles))
	}
}

func TestUserRepositoryPermissionManagement(t *testing.T) {
	db, repo := newTestUserRepository(t)

	role := seedRole(t, db, "perm-role")
	perm := &domain.Permission{
		ID:   uuid.New(),
		Name: "perm.read",
	}

	if err := repo.CreatePermission(perm); err != nil {
		t.Fatalf("CreatePermission failed: %v", err)
	}

	gotByID, err := repo.GetPermissionByID(perm.ID)
	if err != nil {
		t.Fatalf("GetPermissionByID failed: %v", err)
	}
	if gotByID.Name != perm.Name {
		t.Errorf("GetPermissionByID returned wrong name")
	}

	perm.Description = "updated"
	if err := repo.UpdatePermission(perm); err != nil {
		t.Fatalf("UpdatePermission failed: %v", err)
	}

	perms, err := repo.GetAllPermissions()
	if err != nil {
		t.Fatalf("GetAllPermissions failed: %v", err)
	}
	if len(perms) != 1 {
		t.Errorf("expected 1 permission, got %d", len(perms))
	}

	if err := repo.AssignPermissionToRole(role.ID, perm.ID); err != nil {
		t.Fatalf("AssignPermissionToRole failed: %v", err)
	}

	rolePerms, err := repo.GetRolePermissions(role.ID)
	if err != nil {
		t.Fatalf("GetRolePermissions failed: %v", err)
	}
	if len(rolePerms) != 1 || rolePerms[0].ID != perm.ID {
		t.Fatalf("expected role to have assigned permission")
	}

	if err := repo.RemovePermissionFromRole(role.ID, perm.ID); err != nil {
		t.Fatalf("RemovePermissionFromRole failed: %v", err)
	}

	rolePerms, err = repo.GetRolePermissions(role.ID)
	if err != nil {
		t.Fatalf("GetRolePermissions after removal failed: %v", err)
	}
	if len(rolePerms) != 0 {
		t.Errorf("expected no permissions after removal, got %d", len(rolePerms))
	}

	if err := repo.DeletePermission(perm.ID); err != nil {
		t.Fatalf("DeletePermission failed: %v", err)
	}
}

func TestUserRepositoryRefreshTokenLifecycle(t *testing.T) {
	_, repo := newTestUserRepository(t)

	userID := uuid.New()
	token := &domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     "refresh-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if err := repo.CreateRefreshToken(token); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	fetched, err := repo.GetRefreshToken("refresh-token")
	if err != nil {
		t.Fatalf("GetRefreshToken failed: %v", err)
	}
	if fetched.ID != token.ID {
		t.Errorf("GetRefreshToken returned wrong token")
	}

	active, err := repo.GetActiveRefreshTokensByUser(userID)
	if err != nil {
		t.Fatalf("GetActiveRefreshTokensByUser failed: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active token, got %d", len(active))
	}

	if err := repo.RevokeRefreshToken("refresh-token"); err != nil {
		t.Fatalf("RevokeRefreshToken failed: %v", err)
	}

	active, err = repo.GetActiveRefreshTokensByUser(userID)
	if err != nil {
		t.Fatalf("GetActiveRefreshTokensByUser after revoke failed: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected no active tokens after revoke, got %d", len(active))
	}

	if err := repo.RevokeAllUserRefreshTokens(userID); err != nil {
		t.Fatalf("RevokeAllUserRefreshTokens failed: %v", err)
	}
}

func TestUserRepositoryRefreshTokenCleanupAndSessions(t *testing.T) {
	_, repo := newTestUserRepository(t)

	userID := uuid.New()

	activeToken := &domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     "active-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	expiredToken := &domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     "expired-token",
		ExpiresAt: time.Now().Add(-time.Hour),
	}

	if err := repo.CreateRefreshToken(activeToken); err != nil {
		t.Fatalf("CreateRefreshToken(active) failed: %v", err)
	}
	if err := repo.CreateRefreshToken(expiredToken); err != nil {
		t.Fatalf("CreateRefreshToken(expired) failed: %v", err)
	}

	if err := repo.CleanExpiredRefreshTokens(); err != nil {
		t.Fatalf("CleanExpiredRefreshTokens failed: %v", err)
	}

	active, err := repo.GetActiveRefreshTokensByUser(userID)
	if err != nil {
		t.Fatalf("GetActiveRefreshTokensByUser failed: %v", err)
	}
	if len(active) != 1 || active[0].ID != activeToken.ID {
		t.Fatalf("expected only active token to remain")
	}

	sessions, err := repo.GetAllActiveSessions(0, 10)
	if err != nil {
		t.Fatalf("GetAllActiveSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 active session, got %d", len(sessions))
	}

	countSessions, err := repo.CountActiveSessions()
	if err != nil {
		t.Fatalf("CountActiveSessions failed: %v", err)
	}
	if countSessions != 1 {
		t.Errorf("expected CountActiveSessions to be 1, got %d", countSessions)
	}

	if err := repo.RevokeRefreshTokenByID(activeToken.ID); err != nil {
		t.Fatalf("RevokeRefreshTokenByID failed: %v", err)
	}
}

func TestUserRepositoryAdminCounts(t *testing.T) {
	_, repo := newTestUserRepository(t)

	cutoff := time.Now().Add(-time.Minute)

	activeUser := &domain.User{
		ID:       uuid.New(),
		Email:    "active@example.com",
		Username: "active-user",
		Password: "password",
		Status:   domain.UserStatusActive,
		Verified: true,
	}
	if err := repo.Create(activeUser); err != nil {
		t.Fatalf("Create active user failed: %v", err)
	}

	inactiveUser := &domain.User{
		ID:       uuid.New(),
		Email:    "inactive@example.com",
		Username: "inactive-user",
		Password: "password",
		Status:   domain.UserStatusInactive,
		Verified: false,
	}
	if err := repo.Create(inactiveUser); err != nil {
		t.Fatalf("Create inactive user failed: %v", err)
	}

	countActive, err := repo.CountByStatus(string(domain.UserStatusActive))
	if err != nil {
		t.Fatalf("CountByStatus failed: %v", err)
	}
	if countActive != 1 {
		t.Errorf("expected 1 active user, got %d", countActive)
	}

	countRecent, err := repo.CountCreatedAfter(cutoff)
	if err != nil {
		t.Fatalf("CountCreatedAfter failed: %v", err)
	}
	if countRecent != 2 {
		t.Errorf("expected 2 users created after cutoff, got %d", countRecent)
	}
}

func TestUserRepositoryListFiltered(t *testing.T) {
	db, repo := newTestUserRepository(t)

	roleAdmin := seedRole(t, db, "admin")
	roleUser := seedRole(t, db, "user")

	activeVerified := seedUser(t, db, "alice@example.com", "alice")
	if err := db.Exec("UPDATE users SET status = ?, verified = 1 WHERE id = ?",
		string(domain.UserStatusActive), activeVerified.ID.String()).Error; err != nil {
		t.Fatalf("failed to save active verified user: %v", err)
	}

	inactive := seedUser(t, db, "bob@example.com", "bob")
	if err := db.Exec("UPDATE users SET status = ?, verified = 0 WHERE id = ?",
		string(domain.UserStatusInactive), inactive.ID.String()).Error; err != nil {
		t.Fatalf("failed to save inactive user: %v", err)
	}

	if err := db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
		activeVerified.ID.String(), roleAdmin.ID.String()).Error; err != nil {
		t.Fatalf("failed to associate admin role: %v", err)
	}
	if err := db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
		inactive.ID.String(), roleUser.ID.String()).Error; err != nil {
		t.Fatalf("failed to associate user role: %v", err)
	}

	tests := []struct {
		name       string
		filter     domain.UserListFilter
		wantTotal  int64
		wantResult int
		pgOnly     bool // skip on SQLite (uses ILIKE)
	}{
		{
			name: "search by email",
			filter: domain.UserListFilter{
				Search: "alice",
				Limit:  10,
			},
			wantTotal:  1,
			wantResult: 1,
			pgOnly:     true, // ListFiltered uses ILIKE which is PostgreSQL-only
		},
		{
			name: "only active users",
			filter: domain.UserListFilter{
				OnlyActive: true,
				Limit:      10,
			},
			wantTotal:  1,
			wantResult: 1,
		},
		{
			name: "filter by role",
			filter: domain.UserListFilter{
				Roles: []string{"admin"},
				Limit: 10,
			},
			wantTotal:  1,
			wantResult: 1,
		},
		{
			name: "invalid sort field falls back to default",
			filter: domain.UserListFilter{
				SortBy: "invalid_field",
				Order:  "asc",
				Limit:  10,
			},
			wantTotal:  2,
			wantResult: 2,
		},
		{
			name: "valid sort field and asc order",
			filter: domain.UserListFilter{
				SortBy: "email",
				Order:  "asc",
				Limit:  10,
			},
			wantTotal:  2,
			wantResult: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.pgOnly {
				t.Skip("requires PostgreSQL (ILIKE not supported by SQLite)")
			}
			users, total, err := repo.ListFiltered(tt.filter)
			if err != nil {
				t.Fatalf("ListFiltered failed: %v", err)
			}
			if total != tt.wantTotal {
				t.Errorf("expected total %d, got %d", tt.wantTotal, total)
			}
			if len(users) != tt.wantResult {
				t.Errorf("expected %d users, got %d", tt.wantResult, len(users))
			}
		})
	}
}

func TestUserRepositoryRoleManagement(t *testing.T) {
	db, repo := newTestUserRepository(t)

	role := &domain.Role{
		ID:   uuid.New(),
		Name: "test-role",
	}
	if err := repo.CreateRole(role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	gotByID, err := repo.GetRoleByID(role.ID)
	if err != nil {
		t.Fatalf("GetRoleByID failed: %v", err)
	}
	if gotByID.Name != "test-role" {
		t.Errorf("expected role name test-role, got %q", gotByID.Name)
	}

	gotByName, err := repo.GetRoleByName("test-role")
	if err != nil {
		t.Fatalf("GetRoleByName failed: %v", err)
	}
	if gotByName.ID != role.ID {
		t.Errorf("GetRoleByName returned wrong role")
	}

	role.Description = "updated"
	if err := repo.UpdateRole(role); err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}

	allRoles, err := repo.GetAllRoles()
	if err != nil {
		t.Fatalf("GetAllRoles failed: %v", err)
	}
	if len(allRoles) != 1 {
		t.Errorf("expected 1 role, got %d", len(allRoles))
	}

	if err := repo.DeleteRole(role.ID); err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}

	var count int64
	if err := db.Model(&domain.Role{}).Where("id = ?", role.ID).Count(&count).Error; err != nil {
		t.Fatalf("count after role delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected role to be soft deleted, found %d", count)
	}
}

func TestUserRepositoryGetByIDForUpdate(t *testing.T) {
	db, repo := newTestUserRepository(t)

	user := seedUser(t, db, "lock@example.com", "lock-user")

	// GetByIDForUpdate uses SELECT ... FOR UPDATE which SQLite
	// silently accepts; verify it returns the correct row.
	fetched, err := repo.GetByIDForUpdate(user.ID)
	if err != nil {
		t.Fatalf("GetByIDForUpdate failed: %v", err)
	}
	if fetched.Email != user.Email {
		t.Errorf("expected email %q, got %q", user.Email, fetched.Email)
	}
}

func TestUserRepositoryGetByIDNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetByID(uuid.New())
	if err == nil {
		t.Errorf("expected error for non-existent user ID")
	}
}

func TestUserRepositoryGetByEmailNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetByEmail("nobody@example.com")
	if err == nil {
		t.Errorf("expected error for non-existent email")
	}
}

func TestUserRepositoryGetByUsernameNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetByUsername("nobody")
	if err == nil {
		t.Errorf("expected error for non-existent username")
	}
}

func TestUserRepositoryGetRoleByIDNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetRoleByID(uuid.New())
	if err == nil {
		t.Errorf("expected error for non-existent role ID")
	}
}

func TestUserRepositoryGetRoleByNameNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetRoleByName("nonexistent")
	if err == nil {
		t.Errorf("expected error for non-existent role name")
	}
}

func TestUserRepositoryGetPermissionByIDNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetPermissionByID(uuid.New())
	if err == nil {
		t.Errorf("expected error for non-existent permission ID")
	}
}

func TestUserRepositoryGetRefreshTokenNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetRefreshToken("nonexistent-token")
	if err == nil {
		t.Errorf("expected error for non-existent refresh token")
	}
}

func TestUserRepositoryGetUserRolesNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetUserRoles(uuid.New())
	if err == nil {
		t.Errorf("expected error for non-existent user")
	}
}

func TestUserRepositoryGetRolePermissionsNotFound(t *testing.T) {
	_, repo := newTestUserRepository(t)

	_, err := repo.GetRolePermissions(uuid.New())
	if err == nil {
		t.Errorf("expected error for non-existent role")
	}
}

func TestUserRepositoryListFilteredDescOrder(t *testing.T) {
	_, repo := newTestUserRepository(t)

	for i := 0; i < 3; i++ {
		user := &domain.User{
			ID:       uuid.New(),
			Email:    uuid.New().String() + "@example.com",
			Username: "desc-" + uuid.New().String(),
			Password: "password",
			Status:   domain.UserStatusActive,
		}
		if err := repo.Create(user); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	users, total, err := repo.ListFiltered(domain.UserListFilter{
		SortBy: "username",
		Order:  "desc",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListFiltered with desc order failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
	if len(users) != 3 {
		t.Errorf("expected 3 users, got %d", len(users))
	}
}

func TestUserRepositoryGetAllPagination(t *testing.T) {
	_, repo := newTestUserRepository(t)

	for i := 0; i < 5; i++ {
		user := &domain.User{
			ID:       uuid.New(),
			Email:    uuid.New().String() + "@example.com",
			Username: "pg-" + uuid.New().String(),
			Password: "password",
			Status:   domain.UserStatusActive,
		}
		if err := repo.Create(user); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// Offset 2, limit 2 should return 2 users
	users, err := repo.GetAll(2, 2)
	if err != nil {
		t.Fatalf("GetAll with offset failed: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users with offset, got %d", len(users))
	}

	// Negative limit should be clamped
	users, err = repo.GetAll(0, -1)
	if err != nil {
		t.Fatalf("GetAll with negative limit failed: %v", err)
	}
	if len(users) != 5 {
		t.Errorf("expected 5 users with clamped limit, got %d", len(users))
	}
}

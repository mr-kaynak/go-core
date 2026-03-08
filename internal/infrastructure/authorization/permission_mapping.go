package authorization

// PermissionMapping maps a DB permission name to a Casbin resource+action pair.
type PermissionMapping struct {
	Resource Resource
	Action   Action
}

// permissionToCasbin maps every DB permission name to its Casbin equivalent.
// IMPORTANT: Each entry MUST produce a unique (Resource, Action) pair.
// Duplicate pairs cause policy collision — removing one permission would
// silently revoke the other.
var permissionToCasbin = map[string]PermissionMapping{
	// User permissions
	"users.view":   {ResourceUser, ActionRead},
	"users.create": {ResourceUser, ActionCreate},
	"users.update": {ResourceUser, ActionUpdate},
	"users.delete": {ResourceUser, ActionDelete},

	// Role permissions
	"roles.view":   {ResourceRole, ActionRead},
	"roles.create": {ResourceRole, ActionCreate},
	"roles.update": {ResourceRole, ActionUpdate},
	"roles.delete": {ResourceRole, ActionDelete},

	// Permission management
	"permissions.view":   {ResourcePermission, ActionRead},
	"permissions.manage": {ResourcePermission, ActionManage},

	// Template permissions
	"templates.view":   {ResourceTemplate, ActionRead},
	"templates.create": {ResourceTemplate, ActionCreate},
	"templates.update": {ResourceTemplate, ActionUpdate},
	"templates.delete": {ResourceTemplate, ActionDelete},

	// Notification permissions
	"notifications.view":   {ResourceNotification, ActionRead},
	"notifications.create": {ResourceNotification, ActionCreate},
	"notifications.manage": {ResourceNotification, ActionManage},

	// Admin permissions — each maps to a distinct resource or action
	"admin.access":    {ResourceAdmin, ActionRead},
	"admin.manage":    {ResourceAdmin, ActionManage},
	"admin.dashboard": {ResourceDashboard, ActionRead},

	// Audit permissions — dedicated resource + distinct actions
	"audit.view":   {ResourceAudit, ActionRead},
	"audit.export": {ResourceAudit, ActionExport},
}

// GetCasbinMapping returns the Casbin resource+action for a DB permission name.
func GetCasbinMapping(permissionName string) (PermissionMapping, bool) {
	m, ok := permissionToCasbin[permissionName]
	return m, ok
}

// GetAllMappings returns a shallow copy of the permission-to-Casbin mapping registry.
func GetAllMappings() map[string]PermissionMapping {
	cp := make(map[string]PermissionMapping, len(permissionToCasbin))
	for k, v := range permissionToCasbin {
		cp[k] = v
	}
	return cp
}

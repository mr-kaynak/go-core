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
	"templates.export": {ResourceTemplate, ActionExport},
	"templates.import": {ResourceTemplate, ActionImport},

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

	// Blog permissions
	"blog.posts.view":        {ResourceBlogPost, ActionRead},
	"blog.posts.create":      {ResourceBlogPost, ActionCreate},
	"blog.posts.update":      {ResourceBlogPost, ActionUpdate},
	"blog.posts.delete":      {ResourceBlogPost, ActionDelete},
	"blog.categories.view":   {ResourceBlogCategory, ActionRead},
	"blog.categories.create": {ResourceBlogCategory, ActionCreate},
	"blog.categories.update": {ResourceBlogCategory, ActionUpdate},
	"blog.categories.delete": {ResourceBlogCategory, ActionDelete},
	"blog.tags.view":         {ResourceBlogTag, ActionRead},
	"blog.tags.create":       {ResourceBlogTag, ActionCreate},
	"blog.tags.update":       {ResourceBlogTag, ActionUpdate},
	"blog.tags.delete":       {ResourceBlogTag, ActionDelete},
	"blog.comments.view":     {ResourceBlogComment, ActionRead},
	"blog.comments.create":   {ResourceBlogComment, ActionCreate},
	"blog.comments.update":   {ResourceBlogComment, ActionUpdate},
	"blog.comments.delete":   {ResourceBlogComment, ActionDelete},
	"blog.media.view":        {ResourceBlogMedia, ActionRead},
	"blog.media.create":      {ResourceBlogMedia, ActionCreate},
	"blog.media.delete":      {ResourceBlogMedia, ActionDelete},
}

// NOTE: permissionToCasbin is now PRIVATE seed data for NewPermissionRegistry.
// The former global accessors (GetCasbinMapping, GetAllMappings) were removed
// deliberately: every consumer must go through an instance-scoped
// PermissionRegistry so consumer-module permissions are never silently
// skipped and parallel applications stay isolated.

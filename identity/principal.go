// Package identity is the public identity facade: the read-only view a
// consumer module has of the authenticated caller.
//
// It is distinct from internal/modules/identity, which is core's identity
// module (users, roles, tokens, API keys). Nothing here touches the database;
// the values come from the request locals the auth middleware wrote.
package identity

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Principal is the authenticated caller of the current request.
type Principal struct {
	UserID      uuid.UUID
	Username    string
	Email       string
	Roles       []string
	Permissions []string
	// AuthMethod is "jwt" or "api_key".
	AuthMethod string
}

// FromContext reads the principal from the request locals populated by the
// auth middleware. It never panics: a missing or wrongly typed local yields
// the zero value for its field instead.
//
// ok is false only when the identifying local is absent or not a uuid.UUID —
// that request was not authenticated by the middleware, and callers must not
// treat the result as a caller identity. Everything else is tolerated:
// empty Roles and Permissions are VALID (an authenticated user with no grants
// is a legitimate state, so ok stays true), and missing optional strings such
// as Username or Email come back empty — the API-key path leaves them empty
// when no user record is available.
func FromContext(c fiber.Ctx) (*Principal, bool) {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		// Fail closed: no authenticated principal is ever the zero UUID.
		return nil, false
	}

	username, _ := c.Locals("username").(string)
	email, _ := c.Locals("email").(string)
	roles, _ := c.Locals("roles").([]string)
	permissions, _ := c.Locals("permissions").([]string)
	authMethod, _ := c.Locals("authMethod").(string)

	return &Principal{
		UserID:      userID,
		Username:    username,
		Email:       email,
		Roles:       roles,
		Permissions: permissions,
		AuthMethod:  authMethod,
	}, true
}

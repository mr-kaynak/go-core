// Package projects shows persistent, caller-owned data and transactional events.
package projects

import (
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/app"
	"github.com/mr-kaynak/go-core/identity"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Project struct {
	ID        uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	Name      string    `json:"name"`
	OwnerID   uuid.UUID `json:"owner_id" gorm:"type:uuid"`
	CreatedAt time.Time `json:"created_at"`
}

func (Project) TableName() string { return "startup_projects" }

type Module struct {
	db     *gorm.DB
	events app.EventPublisher
}

var _ app.Module = (*Module)(nil)
var _ app.MigrationProvider = (*Module)(nil)

func New() *Module             { return &Module{} }
func (m *Module) Name() string { return "projects" }

func (m *Module) MigrationSource() app.MigrationSource {
	// The directory is guaranteed by go:embed; SQL must be at the FS root.
	root, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic(err)
	}
	return app.MigrationSource{Name: "startup_projects", FS: root}
}

func (m *Module) Permissions() []app.Permission {
	objects := []string{"/api/v1/projects", "/api/v1/projects/*"}
	return []app.Permission{
		{Name: "projects.view", Objects: objects, Action: app.ActionRead},
		{Name: "projects.create", Objects: objects, Action: app.ActionCreate},
	}
}

func (m *Module) Register(ctx *app.ModuleContext) error {
	m.db, m.events = ctx.DB, ctx.Events
	routes := ctx.Router.Group("/projects", ctx.Auth, ctx.Authz)
	routes.Get("/", m.list)
	routes.Get("/:id", m.get)
	routes.Post("/", m.create)
	return nil
}

func (m *Module) list(c fiber.Ctx) error {
	caller, ok := identity.FromContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}
	// RBAC grants the operation; the owner predicate grants access to rows.
	// Even system_admin sees only their own projects in this example.
	rows := make([]Project, 0)
	query := m.db.WithContext(c.Context()).Where("owner_id = ?", caller.UserID)
	var total int64
	if err := query.Model(&Project{}).Count(&total).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	if err := query.Order("created_at DESC, id DESC").Limit(50).Find(&rows).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"projects": rows, "total": total})
}

func (m *Module) get(c fiber.Ctx) error {
	caller, ok := identity.FromContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid project id")
	}
	var row Project
	err = m.db.WithContext(c.Context()).Where("id = ? AND owner_id = ?", id, caller.UserID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(row)
}

func (m *Module) create(c fiber.Ctx) error {
	caller, ok := identity.FromContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := c.Bind().Body(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 120 {
		return fiber.NewError(fiber.StatusBadRequest, "name must contain 1 to 120 characters")
	}
	row := Project{ID: uuid.New(), Name: input.Name, OwnerID: caller.UserID, CreatedAt: time.Now().UTC()}
	err := m.db.WithContext(c.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		// Returning Dispatch's error rolls back the project as well as its event.
		return m.events.Dispatch(app.ContextWithTx(c.Context(), tx), "project.created", row.ID.String(), map[string]any{
			"project_id": row.ID.String(), "owner_id": row.OwnerID.String(), "name": row.Name,
		})
	})
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.Status(fiber.StatusCreated).JSON(row)
}

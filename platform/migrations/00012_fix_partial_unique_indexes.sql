-- +goose Up

-- Replace full unique indexes with partial unique indexes that exclude
-- soft-deleted rows. Without this, a deleted user's email/username blocks
-- new registrations with the same value.

DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_username;
CREATE UNIQUE INDEX idx_users_email ON users(email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_users_username ON users(username) WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_roles_name;
CREATE UNIQUE INDEX idx_roles_name ON roles(name) WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_permissions_name;
CREATE UNIQUE INDEX idx_permissions_name ON permissions(name) WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_verification_tokens_token;
CREATE UNIQUE INDEX idx_verification_tokens_token ON verification_tokens(token) WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_notification_templates_name;
CREATE UNIQUE INDEX idx_notification_templates_name ON notification_templates(name) WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_template_categories_name;
CREATE UNIQUE INDEX idx_template_categories_name ON template_categories(name) WHERE deleted_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_username;
CREATE UNIQUE INDEX idx_users_email ON users(email);
CREATE UNIQUE INDEX idx_users_username ON users(username);

DROP INDEX IF EXISTS idx_roles_name;
CREATE UNIQUE INDEX idx_roles_name ON roles(name);

DROP INDEX IF EXISTS idx_permissions_name;
CREATE UNIQUE INDEX idx_permissions_name ON permissions(name);

DROP INDEX IF EXISTS idx_verification_tokens_token;
CREATE UNIQUE INDEX idx_verification_tokens_token ON verification_tokens(token);

DROP INDEX IF EXISTS idx_notification_templates_name;
CREATE UNIQUE INDEX idx_notification_templates_name ON notification_templates(name);

DROP INDEX IF EXISTS idx_template_categories_name;
CREATE UNIQUE INDEX idx_template_categories_name ON template_categories(name);

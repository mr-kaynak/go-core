-- +goose Up
-- +goose StatementBegin
ALTER TABLE notification_templates ADD COLUMN created_by UUID;
CREATE INDEX idx_notification_templates_created_by ON notification_templates(created_by);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_notification_templates_created_by;
ALTER TABLE notification_templates DROP COLUMN IF EXISTS created_by;
-- +goose StatementEnd

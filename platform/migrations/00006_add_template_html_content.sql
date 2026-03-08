-- +goose Up
ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS html_content TEXT;
ALTER TABLE template_languages ADD COLUMN IF NOT EXISTS html_content TEXT;

-- +goose Down
ALTER TABLE template_languages DROP COLUMN IF EXISTS html_content;
ALTER TABLE notification_templates DROP COLUMN IF EXISTS html_content;

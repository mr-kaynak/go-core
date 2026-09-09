-- +goose Up
-- +goose StatementBegin
ALTER TABLE blog_posts ADD COLUMN meta_title VARCHAR(255) DEFAULT '' NOT NULL;
ALTER TABLE blog_posts ADD COLUMN meta_description VARCHAR(500) DEFAULT '' NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE blog_posts DROP COLUMN IF EXISTS meta_title;
ALTER TABLE blog_posts DROP COLUMN IF EXISTS meta_description;
-- +goose StatementEnd

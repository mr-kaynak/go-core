-- +goose Up
ALTER TABLE blog_comments ADD COLUMN depth INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE blog_comments DROP COLUMN depth;

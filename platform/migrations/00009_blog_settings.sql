-- +goose Up
-- +goose StatementBegin
CREATE TABLE blog_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    auto_approve_comments BOOLEAN NOT NULL DEFAULT false,
    posts_per_page INTEGER NOT NULL DEFAULT 20,
    view_cooldown_minutes INTEGER NOT NULL DEFAULT 30,
    feed_item_limit INTEGER NOT NULL DEFAULT 50,
    read_time_wpm INTEGER NOT NULL DEFAULT 200,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

-- Singleton constraint: only one settings row allowed
CREATE UNIQUE INDEX idx_blog_settings_singleton ON blog_settings ((true));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS blog_settings;
-- +goose StatementEnd

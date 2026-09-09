-- +goose Up
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS ip_address VARCHAR(45);
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS user_agent VARCHAR(512);

-- +goose Down
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS user_agent;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS ip_address;

-- +goose Up
-- +goose StatementBegin
ALTER TABLE users ALTER COLUMN two_factor_secret TYPE VARCHAR(512);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users ALTER COLUMN two_factor_secret TYPE VARCHAR(64);
-- +goose StatementEnd

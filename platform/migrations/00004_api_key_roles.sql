-- +goose Up
CREATE TABLE api_key_roles (
    api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    role_id    UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (api_key_id, role_id)
);
CREATE INDEX idx_api_key_roles_api_key_id ON api_key_roles(api_key_id);
CREATE INDEX idx_api_key_roles_role_id ON api_key_roles(role_id);

-- +goose Down
DROP TABLE IF EXISTS api_key_roles;

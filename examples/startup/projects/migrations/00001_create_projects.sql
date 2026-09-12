-- +goose Up
CREATE TABLE startup_projects (
    id UUID PRIMARY KEY,
    name VARCHAR(120) NOT NULL CHECK (char_length(trim(name)) > 0),
    owner_id UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX startup_projects_owner_created_idx ON startup_projects(owner_id, created_at DESC, id DESC);

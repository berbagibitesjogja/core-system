-- +goose Up
CREATE TABLE divisions (
    id SERIAL PRIMARY KEY,
    name VARCHAR(24) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE UNIQUE INDEX uk_divisions_active_name ON divisions (name) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE divisions;

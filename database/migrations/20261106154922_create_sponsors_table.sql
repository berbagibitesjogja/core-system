-- +goose Up
CREATE TYPE sponsor_variant AS ENUM ('company', 'individual');

CREATE TABLE sponsors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(150) NOT NULL,
    variant sponsor_variant NOT NULL DEFAULT 'individual',
    address VARCHAR(500),
    email VARCHAR(255),
    phone VARCHAR(20),
    logo UUID REFERENCES files(id) ON DELETE SET NULL,
    hidden BOOL NOT NULL DEFAULT TRUE,
    hide_donations BOOL NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE UNIQUE INDEX uk_sponsors_active_lower_name ON sponsors (LOWER(name)) WHERE deleted_at IS NULL;
CREATE INDEX idx_sponsors_name_trgm ON sponsors USING gin (name gin_trgm_ops);

-- +goose Down
DROP TABLE sponsors CASCADE;
DROP TYPE IF EXISTS sponsor_variant;

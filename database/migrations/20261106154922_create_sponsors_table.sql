-- +goose Up
CREATE TYPE sponsor_variant AS ENUM ('company', 'individual');

CREATE TABLE sponsors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) NOT NULL,
    variant sponsor_variant DEFAULT 'individual',
    address VARCHAR(500),
    email VARCHAR(255),
    phone VARCHAR(20),
    logo UUID REFERENCES files(id) ON DELETE SET NULL,
    hidden BOOL NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE UNIQUE INDEX uk_sponsors_active_name ON sponsors (name) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE sponsors;
DROP TYPE IF EXISTS sponsor_variant;

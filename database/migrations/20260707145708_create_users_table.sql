-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    password_hash TEXT,
    phone_number VARCHAR(20) DEFAULT NULL,
    profile_pic UUID REFERENCES files(id) ON DELETE SET NULL,
    pfp_external_url TEXT,
    bio VARCHAR(500),
    address TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    email_verified_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE UNIQUE INDEX idx_users_active_email ON users (LOWER(email)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_users_active_phone ON users (phone_number) WHERE deleted_at IS NULL;

CREATE INDEX idx_users_name_trgm ON users USING gin (name gin_trgm_ops);
CREATE INDEX idx_users_email_trgm ON users USING gin (email gin_trgm_ops);

-- +goose Down
DROP TABLE IF EXISTS users CASCADE;

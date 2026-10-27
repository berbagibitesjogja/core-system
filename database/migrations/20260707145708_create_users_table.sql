-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE TYPE user_role AS ENUM ('super', 'core', 'staff');

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    division_id INTEGER NOT NULL REFERENCES divisions(id),
    email VARCHAR(255) NOT NULL,
    password_hash TEXT,
    role user_role NOT NULL DEFAULT 'staff',
    name VARCHAR(255) NOT NULL,
    phone_number VARCHAR(20) NOT NULL,
    profile_pic UUID REFERENCES files(id),
    bio VARCHAR(500),
    address TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE INDEX idx_users_name_trgm ON users USING gin (name gin_trgm_ops);

-- +goose Down
DROP TABLE users;
DROP TYPE IF EXISTS user_role;

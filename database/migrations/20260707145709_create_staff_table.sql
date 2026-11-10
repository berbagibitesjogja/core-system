-- +goose Up
CREATE TYPE user_role AS ENUM ('super', 'core', 'staff');

CREATE TABLE staff (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    division_id INTEGER NOT NULL REFERENCES divisions(id),
    role user_role NOT NULL DEFAULT 'staff',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_staff_division_id ON staff (division_id);

-- +goose Down
DROP TABLE IF EXISTS staff CASCADE;
DROP TYPE IF EXISTS user_role;

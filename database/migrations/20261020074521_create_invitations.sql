-- +goose Up
CREATE TABLE user_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invited_by UUID NOT NULL REFERENCES staff(user_id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    role user_role NOT NULL DEFAULT 'staff',
    division_id INTEGER NOT NULL REFERENCES divisions(id),
    name VARCHAR(255),
    phone_number VARCHAR(20),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_invitations_email ON user_invitations (LOWER(email));
CREATE INDEX idx_invitations_invited_by ON user_invitations (invited_by);

-- +goose Down
DROP TABLE IF EXISTS user_invitations CASCADE;

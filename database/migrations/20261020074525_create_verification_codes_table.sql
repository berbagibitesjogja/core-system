-- +goose Up
CREATE TYPE verification_purpose AS ENUM ('email_verification', 'password_reset');

CREATE TABLE verification_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose verification_purpose NOT NULL,
    code_hash TEXT NOT NULL,
    attempts SMALLINT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_verification_codes_lookup ON verification_codes (user_id, purpose, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS verification_codes;
DROP TYPE IF EXISTS verification_purpose;

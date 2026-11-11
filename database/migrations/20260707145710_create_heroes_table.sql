-- +goose Up
CREATE TABLE heroes (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    remind_me_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_heroes_remind_me_at ON heroes (remind_me_at) WHERE remind_me_at IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS heroes CASCADE;

-- +goose Up
CREATE TYPE donation_status AS ENUM ('draft', 'open', 'closed', 'completed', 'cancelled');

CREATE TABLE donations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sponsor_id UUID NOT NULL REFERENCES sponsors(id),
    created_by UUID REFERENCES staff(user_id) ON DELETE SET NULL,
    event_at TIMESTAMPTZ NOT NULL,
    quota INTEGER NOT NULL,
    hero_quota INTEGER NOT NULL DEFAULT 0,
    remain INTEGER NOT NULL DEFAULT 0,
    address VARCHAR(500),
    maps TEXT,
    message TEXT,
    status donation_status NOT NULL DEFAULT 'draft',
    hide_from_public BOOLEAN NOT NULL DEFAULT TRUE,
    hide_sponsor BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL,
    CONSTRAINT chk_donations_quota CHECK (quota >= 0),
    CONSTRAINT chk_donations_hero_quota CHECK (hero_quota BETWEEN 0 AND quota),
    CONSTRAINT chk_donations_remain CHECK (remain BETWEEN 0 AND hero_quota)
);

CREATE INDEX idx_donations_public ON donations (event_at)
    WHERE deleted_at IS NULL AND hide_from_public = FALSE AND status = 'open';
CREATE INDEX idx_donations_status_event ON donations (status, event_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_donations_sponsor_id ON donations (sponsor_id);

-- +goose Down
DROP TABLE IF EXISTS donations CASCADE;
DROP TYPE IF EXISTS donation_status;

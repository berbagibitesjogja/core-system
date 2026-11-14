-- +goose Up
CREATE TYPE attendance_status AS ENUM ('registered', 'attended', 'absent', 'cancelled');

CREATE TABLE hero_attendances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    donation_id UUID NOT NULL REFERENCES donations(id) ON DELETE RESTRICT,
    hero_id UUID NOT NULL REFERENCES heroes(user_id) ON DELETE RESTRICT,
    status attendance_status NOT NULL DEFAULT 'registered',
    cancelled_late BOOLEAN NOT NULL DEFAULT FALSE,
    cancelled_at TIMESTAMPTZ,
    attended_at TIMESTAMPTZ,
    marked_by UUID REFERENCES staff(user_id) ON DELETE SET NULL,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_hero_attendance_status_timestamps CHECK (
        (status = 'cancelled' AND cancelled_at IS NOT NULL AND attended_at IS NULL) OR
        (status = 'attended' AND attended_at IS NOT NULL AND cancelled_at IS NULL) OR
        (status IN ('registered', 'absent') AND cancelled_at IS NULL AND attended_at IS NULL)
    )
);

CREATE UNIQUE INDEX uk_active_hero_claim ON hero_attendances (donation_id, hero_id) WHERE cancelled_at IS NULL;
CREATE INDEX idx_hero_attendances_hero ON hero_attendances (hero_id);
CREATE INDEX idx_hero_attendances_donation_status ON hero_attendances (donation_id, status);

-- +goose Down
DROP TABLE IF EXISTS hero_attendances CASCADE;
DROP TYPE IF EXISTS attendance_status;

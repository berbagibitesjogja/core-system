-- +goose Up
CREATE TYPE ban_scope AS ENUM ('hero_war', 'staff_war', 'all');
CREATE TYPE ban_reason AS ENUM ('late_cancellation', 'no_show', 'manual');

CREATE TABLE user_bans (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    scope ban_scope NOT NULL DEFAULT 'hero_war',
    donation_id UUID REFERENCES donations(id) ON DELETE RESTRICT,
    donation_count INT NOT NULL DEFAULT 2 CHECK (donation_count > 0),
    reason ban_reason NOT NULL DEFAULT 'late_cancellation',
    source_hero_attendance_id UUID UNIQUE REFERENCES hero_attendances(id) ON DELETE SET NULL,
    source_staff_attendance_id BIGINT UNIQUE REFERENCES staff_attendances(id) ON DELETE SET NULL,
    note TEXT,
    created_by UUID REFERENCES staff(user_id) ON DELETE SET NULL,
    lifted_at TIMESTAMPTZ,
    lifted_by UUID REFERENCES staff(user_id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_user_bans_donation_id CHECK (
        (reason = 'manual' AND donation_id IS NULL) OR
        (donation_id IS NOT NULL)
    ),
    CONSTRAINT chk_user_bans_source_scope CHECK (
        (scope = 'hero_war' AND source_hero_attendance_id IS NOT NULL AND source_staff_attendance_id IS NULL) OR
        (scope = 'staff_war' AND source_staff_attendance_id IS NOT NULL AND source_hero_attendance_id IS NULL) OR
        (reason = 'manual' AND source_hero_attendance_id IS NULL AND source_staff_attendance_id IS NULL)
    )
);

CREATE INDEX idx_user_bans_active ON user_bans (user_id, scope) WHERE lifted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS user_bans CASCADE;
DROP TYPE IF EXISTS ban_reason;
DROP TYPE IF EXISTS ban_scope;

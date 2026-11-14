-- +goose Up
CREATE TABLE staff_attendances (
    id BIGSERIAL PRIMARY KEY,
    war_slot_id BIGINT NOT NULL REFERENCES war_slots(id) ON DELETE RESTRICT,
    staff_id UUID NOT NULL REFERENCES staff(user_id) ON DELETE RESTRICT,
    status attendance_status NOT NULL DEFAULT 'registered',
    cancelled_late BOOLEAN NOT NULL DEFAULT FALSE,
    cancelled_at TIMESTAMPTZ,
    attended_at TIMESTAMPTZ,
    marked_by UUID REFERENCES staff(user_id) ON DELETE SET NULL,
    checkin_source VARCHAR(20) CHECK (checkin_source IN ('self', 'coordinator')),
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_staff_attendance_status_timestamps CHECK (
        (status = 'cancelled' AND cancelled_at IS NOT NULL AND attended_at IS NULL) OR
        (status = 'attended' AND attended_at IS NOT NULL AND cancelled_at IS NULL) OR
        (status IN ('registered', 'absent') AND cancelled_at IS NULL AND attended_at IS NULL)
    )
);

CREATE UNIQUE INDEX uk_active_staff_claim ON staff_attendances (war_slot_id, staff_id) WHERE cancelled_at IS NULL;
CREATE INDEX idx_staff_attendances_staff ON staff_attendances (staff_id);
CREATE INDEX idx_staff_attendances_slot ON staff_attendances (war_slot_id);

-- +goose Down
DROP TABLE IF EXISTS staff_attendances CASCADE;

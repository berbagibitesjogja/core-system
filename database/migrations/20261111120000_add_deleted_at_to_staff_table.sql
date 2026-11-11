-- +goose Up
ALTER TABLE staff ADD COLUMN deleted_at TIMESTAMPTZ DEFAULT NULL;

CREATE INDEX idx_staff_active ON staff (user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_staff_active_role ON staff (role) WHERE deleted_at IS NULL;
CREATE INDEX idx_staff_active_division ON staff (division_id) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_staff_active_division;
DROP INDEX IF EXISTS idx_staff_active_role;
DROP INDEX IF EXISTS idx_staff_active;

ALTER TABLE staff DROP COLUMN IF EXISTS deleted_at;

-- +goose Up
CREATE TABLE war_slots (
    id BIGSERIAL PRIMARY KEY,
    donation_id UUID NOT NULL REFERENCES donations(id) ON DELETE RESTRICT,
    division_id INT NOT NULL REFERENCES divisions(id) ON DELETE RESTRICT,
    quota INT NOT NULL CHECK (quota > 0),
    remain INT NOT NULL CHECK (remain >= 0 AND remain <= quota),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE UNIQUE INDEX uk_war_slots_donation_division ON war_slots (donation_id, division_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_war_slots_donation ON war_slots (donation_id);

-- +goose Down
DROP TABLE IF EXISTS war_slots CASCADE;

-- +goose Up
CREATE TABLE donation_target (
    donation_id UUID NOT NULL REFERENCES donations(id) ON DELETE CASCADE,
    beneficiary_category_id UUID NOT NULL REFERENCES beneficiary_categories(id),
    PRIMARY KEY (donation_id, beneficiary_category_id)
);

CREATE INDEX idx_donation_target_category_id ON donation_target (beneficiary_category_id);

-- +goose Down
DROP TABLE IF EXISTS donation_target;

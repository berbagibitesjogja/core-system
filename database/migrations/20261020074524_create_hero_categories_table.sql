-- +goose Up
CREATE TABLE hero_categories (
    hero_id UUID NOT NULL REFERENCES heroes(user_id) ON DELETE CASCADE,
    beneficiary_category_id UUID NOT NULL REFERENCES beneficiary_categories(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (hero_id, beneficiary_category_id)
);

CREATE INDEX idx_hero_categories_category_id ON hero_categories (beneficiary_category_id);

-- +goose Down
DROP TABLE IF EXISTS hero_categories;

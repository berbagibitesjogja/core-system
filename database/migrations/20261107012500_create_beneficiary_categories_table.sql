-- +goose Up
CREATE TABLE beneficiary_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(150) NOT NULL,
    variant VARCHAR(50) NOT NULL CHECK (variant IN (
        'student', 'orphanage', 'nursing_home', 'shelter',
        'religious_community', 'foundation', 'society', 'other'
    )),
    hidden BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uk_beneficiary_categories_active_lower_name
    ON beneficiary_categories (LOWER(name))
    WHERE deleted_at IS NULL;

CREATE INDEX idx_beneficiary_categories_name_trgm
    ON beneficiary_categories USING gin (name gin_trgm_ops);

CREATE INDEX idx_beneficiary_categories_variant
    ON beneficiary_categories (variant)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS beneficiary_categories;

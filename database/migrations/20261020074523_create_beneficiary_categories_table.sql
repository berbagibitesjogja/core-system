-- +goose Up
CREATE TABLE beneficiary_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(150) NOT NULL,
    address TEXT,
    maps TEXT,
    variant VARCHAR(50) NOT NULL CHECK (variant IN (
        'university', 'kipk', 'orphanage', 'nursing_home', 'shelter',
        'religious_community', 'foundation', 'society', 'other'
    )),
    email_domain VARCHAR(255),
    is_priority BOOLEAN NOT NULL DEFAULT FALSE,
    hidden BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT chk_email_domain_university_only
        CHECK (email_domain IS NULL OR variant = 'university'),
    CONSTRAINT chk_email_domain_lowercase
        CHECK (email_domain IS NULL OR email_domain = LOWER(email_domain))
);

CREATE UNIQUE INDEX uk_beneficiary_categories_active_lower_name
    ON beneficiary_categories (LOWER(name))
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX uk_beneficiary_categories_active_email_domain
    ON beneficiary_categories (email_domain)
    WHERE email_domain IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX idx_beneficiary_categories_name_trgm
    ON beneficiary_categories USING gin (name gin_trgm_ops);

CREATE INDEX idx_beneficiary_categories_variant
    ON beneficiary_categories (variant)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS beneficiary_categories CASCADE;

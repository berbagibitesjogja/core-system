-- +goose Up
CREATE TABLE food_items (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX uk_food_items_lower_name ON food_items (LOWER(name));
CREATE INDEX idx_food_items_name_trgm ON food_items USING gin (name gin_trgm_ops);

-- +goose Down
DROP TABLE food_items CASCADE;

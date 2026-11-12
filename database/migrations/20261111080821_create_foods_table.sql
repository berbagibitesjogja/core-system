-- +goose Up
CREATE TYPE food_unit AS ENUM ('kg', 'g', 'l', 'ml', 'portion', 'pack', 'piece');

CREATE TABLE foods (
    donation_id UUID NOT NULL REFERENCES donations(id) ON DELETE CASCADE,
    food_item_id INTEGER NOT NULL REFERENCES food_items(id),
    quantity NUMERIC(10,2) NOT NULL CHECK (quantity > 0),
    unit food_unit NOT NULL,
    PRIMARY KEY (donation_id, food_item_id)
);

CREATE INDEX idx_foods_food_item_id ON foods (food_item_id);

-- +goose Down
DROP TABLE IF EXISTS foods;
DROP TYPE IF EXISTS food_unit;

-- name: CreateFood :one
INSERT INTO foods (
    donation_id,
    food_item_id,
    quantity,
    unit
) VALUES (
    $1, $2, $3, $4
)
RETURNING donation_id, food_item_id, quantity, unit;

-- name: ListFoodsByDonationID :many
SELECT
    f.donation_id,
    f.food_item_id,
    f.quantity,
    f.unit,
    fi.name AS food_name
FROM foods f
JOIN food_items fi ON fi.id = f.food_item_id
WHERE f.donation_id = $1
ORDER BY fi.name ASC;

-- name: ListFoodsByDonationIDs :many
SELECT
    f.donation_id,
    f.food_item_id,
    f.quantity,
    f.unit,
    fi.name AS food_name
FROM foods f
JOIN food_items fi ON fi.id = f.food_item_id
WHERE f.donation_id = ANY($1::uuid[])
ORDER BY fi.name ASC;

-- name: DeleteFoodsByDonationID :exec
DELETE FROM foods
WHERE donation_id = $1;

-- name: CreateFoodItem :one
INSERT INTO food_items (
    name
) VALUES (
    $1
)
RETURNING id, name, created_at, updated_at;

-- name: GetFoodItemByID :one
SELECT id, name, created_at, updated_at
FROM food_items
WHERE id = $1
LIMIT 1;

-- name: UpdateFoodItem :one
UPDATE food_items
SET
    name = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING id, name, created_at, updated_at;

-- name: DeleteFoodItem :one
DELETE FROM food_items
WHERE id = $1
RETURNING id;

-- name: ListFoodItems :many
SELECT id, name
FROM food_items
WHERE (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%')
ORDER BY name ASC
LIMIT $1 OFFSET $2;

-- name: CountFoodItems :one
SELECT COUNT(*)
FROM food_items
WHERE (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%');

-- name: CountFoodItemsByIDs :one
SELECT COUNT(*)
FROM food_items
WHERE id = ANY($1::int[]);

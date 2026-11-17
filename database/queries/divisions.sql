-- name: GetDivisionByID :one
SELECT id, name, created_at, updated_at
FROM divisions
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: ListDivisions :many
SELECT id, name
FROM divisions
WHERE deleted_at IS NULL
ORDER BY id ASC;

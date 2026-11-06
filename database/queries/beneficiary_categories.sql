-- name: CreateBeneficiaryCategory :one
INSERT INTO beneficiary_categories (
    name,
    variant,
    hidden
) VALUES (
    $1, $2, $3
)
RETURNING id, name, variant, hidden, created_at, updated_at, deleted_at;

-- name: GetBeneficiaryCategoryByID :one
SELECT id, name, variant, hidden, created_at, updated_at, deleted_at
FROM beneficiary_categories
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: UpdateBeneficiaryCategory :one
UPDATE beneficiary_categories
SET
    name = $2,
    variant = $3,
    hidden = $4,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, name, variant, hidden, created_at, updated_at, deleted_at;

-- name: SoftDeleteBeneficiaryCategory :one
UPDATE beneficiary_categories
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, deleted_at;

-- name: ListBeneficiaryCategories :many
SELECT id, name, variant, hidden, created_at, updated_at, deleted_at
FROM beneficiary_categories
WHERE deleted_at IS NULL
  AND (sqlc.narg('hidden')::boolean IS NULL OR hidden = sqlc.narg('hidden'))
  AND (sqlc.narg('variant')::text IS NULL OR variant = sqlc.narg('variant'))
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%')
ORDER BY name ASC
LIMIT $1 OFFSET $2;

-- name: CountBeneficiaryCategories :one
SELECT COUNT(*)
FROM beneficiary_categories
WHERE deleted_at IS NULL
  AND (sqlc.narg('hidden')::boolean IS NULL OR hidden = sqlc.narg('hidden'))
  AND (sqlc.narg('variant')::text IS NULL OR variant = sqlc.narg('variant'))
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%');

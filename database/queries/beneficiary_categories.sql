-- name: CreateBeneficiaryCategory :one
INSERT INTO beneficiary_categories (
    name,
    address,
    maps,
    variant,
    email_domain,
    is_priority,
    hidden
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING id, name, address, maps, variant, email_domain, is_priority, hidden, created_at, updated_at, deleted_at;

-- name: GetBeneficiaryCategoryByID :one
SELECT id, name, address, maps, variant, email_domain, is_priority, hidden, created_at, updated_at, deleted_at
FROM beneficiary_categories
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: UpdateBeneficiaryCategory :one
UPDATE beneficiary_categories
SET
    name = $2,
    address = $3,
    maps = $4,
    variant = $5,
    email_domain = $6,
    is_priority = $7,
    hidden = $8,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, name, address, maps, variant, email_domain, is_priority, hidden, created_at, updated_at, deleted_at;

-- name: SoftDeleteBeneficiaryCategory :one
UPDATE beneficiary_categories
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, deleted_at;

-- name: ListBeneficiaryCategories :many
SELECT id, name, address, maps, variant, email_domain, is_priority, hidden, created_at, updated_at, deleted_at
FROM beneficiary_categories
WHERE deleted_at IS NULL
  AND (sqlc.narg('hidden')::boolean IS NULL OR hidden = sqlc.narg('hidden'))
  AND (sqlc.narg('variant')::text IS NULL OR variant = sqlc.narg('variant'))
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%')
ORDER BY is_priority DESC, name ASC
LIMIT $1 OFFSET $2;

-- name: CountBeneficiaryCategories :one
SELECT COUNT(*)
FROM beneficiary_categories
WHERE deleted_at IS NULL
  AND (sqlc.narg('hidden')::boolean IS NULL OR hidden = sqlc.narg('hidden'))
  AND (sqlc.narg('variant')::text IS NULL OR variant = sqlc.narg('variant'))
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%');

-- name: MatchUniversityCategoryByEmailDomain :one
SELECT id, name, address, maps, variant, email_domain, is_priority, hidden, created_at, updated_at
FROM beneficiary_categories
WHERE variant = 'university'
  AND email_domain IS NOT NULL
  AND deleted_at IS NULL
  AND (
    LOWER($1) = email_domain
    OR LOWER($1) LIKE '%.' || email_domain
  )
ORDER BY LENGTH(email_domain) DESC
LIMIT 1;

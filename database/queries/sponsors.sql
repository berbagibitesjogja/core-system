-- name: CreateSponsor :one
INSERT INTO sponsors (
    name,
    variant,
    address,
    email,
    phone,
    logo,
    hidden
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING id, name, variant, address, email, phone, logo, hidden, created_at, updated_at, deleted_at;

-- name: GetSponsorByID :one
SELECT id, name, variant, address, email, phone, logo, hidden, created_at, updated_at, deleted_at
FROM sponsors
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: UpdateSponsor :one
UPDATE sponsors
SET
    name = $2,
    variant = $3,
    address = $4,
    email = $5,
    phone = $6,
    logo = $7,
    hidden = $8,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, name, variant, address, email, phone, logo, hidden, created_at, updated_at, deleted_at;

-- name: SoftDeleteSponsor :one
UPDATE sponsors
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, deleted_at;

-- name: ListSponsors :many
SELECT id, name, variant, address, email, phone, logo, hidden, created_at, updated_at, deleted_at
FROM sponsors
WHERE deleted_at IS NULL
  AND (sqlc.narg('hidden')::boolean IS NULL OR hidden = sqlc.narg('hidden'))
  AND (sqlc.narg('variant')::sponsor_variant IS NULL OR variant = sqlc.narg('variant'))
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%')
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountSponsors :one
SELECT COUNT(*)
FROM sponsors
WHERE deleted_at IS NULL
  AND (sqlc.narg('hidden')::boolean IS NULL OR hidden = sqlc.narg('hidden'))
  AND (sqlc.narg('variant')::sponsor_variant IS NULL OR variant = sqlc.narg('variant'))
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%');

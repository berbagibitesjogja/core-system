-- name: AddHeroCategory :one
INSERT INTO hero_categories (
    hero_id,
    beneficiary_category_id
) VALUES (
    $1, $2
)
ON CONFLICT (hero_id, beneficiary_category_id) DO UPDATE
SET updated_at = NOW()
RETURNING hero_id, beneficiary_category_id, created_at, updated_at;

-- name: RemoveHeroCategory :exec
DELETE FROM hero_categories
WHERE hero_id = $1 AND beneficiary_category_id = $2;

-- name: ListCategoriesByHeroID :many
SELECT 
    bc.id,
    bc.name,
    bc.variant,
    bc.email_domain,
    bc.is_priority,
    hc.created_at AS enrolled_at
FROM hero_categories hc
JOIN beneficiary_categories bc ON bc.id = hc.beneficiary_category_id
WHERE hc.hero_id = $1 AND bc.deleted_at IS NULL
ORDER BY bc.is_priority DESC, bc.name ASC;

-- name: CountHeroesByCategoryID :one
SELECT COUNT(*)::INT
FROM hero_categories
WHERE beneficiary_category_id = $1;

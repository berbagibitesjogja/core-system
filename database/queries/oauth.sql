-- name: GetOAuthAccountByProvider :one
SELECT 
    oa.id,
    oa.user_id,
    oa.provider,
    oa.provider_user_id,
    oa.email,
    oa.created_at,
    u.name,
    u.phone_number,
    u.is_active,
    u.email_verified_at,
    s.role AS staff_role,
    s.division_id AS staff_division_id,
    d.name AS staff_division_name,
    (s.user_id IS NOT NULL)::BOOLEAN AS is_staff,
    (h.user_id IS NOT NULL)::BOOLEAN AS is_hero
FROM oauth_accounts oa
JOIN users u ON u.id = oa.user_id
LEFT JOIN staff s ON s.user_id = u.id AND s.deleted_at IS NULL
LEFT JOIN divisions d ON d.id = s.division_id AND d.deleted_at IS NULL
LEFT JOIN heroes h ON h.user_id = u.id
WHERE oa.provider = $1 AND oa.provider_user_id = $2 AND u.deleted_at IS NULL
LIMIT 1;

-- name: CreateOAuthAccount :one
INSERT INTO oauth_accounts (
    user_id,
    provider,
    provider_user_id,
    email
) VALUES (
    $1, $2, $3, $4
)
RETURNING id, user_id, provider, provider_user_id, email, created_at;

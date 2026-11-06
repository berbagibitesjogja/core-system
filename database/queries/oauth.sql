-- name: GetOAuthAccountByProvider :one
SELECT 
    oa.id,
    oa.user_id,
    oa.provider,
    oa.provider_user_id,
    oa.email,
    oa.created_at,
    u.division_id,
    u.role,
    u.name,
    u.phone_number,
    u.is_active,
    u.email_verified_at
FROM oauth_accounts oa
JOIN users u ON u.id = oa.user_id
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

-- name: GetOAuthAccountsByUserID :many
SELECT id, user_id, provider, provider_user_id, email, created_at
FROM oauth_accounts
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: DeleteOAuthAccount :exec
DELETE FROM oauth_accounts
WHERE user_id = $1 AND provider = $2;

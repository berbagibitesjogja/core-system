-- name: GetHeroByUserID :one
SELECT user_id, remind_me_at, created_at, updated_at
FROM heroes
WHERE user_id = $1
LIMIT 1;

-- name: CreateHero :one
INSERT INTO heroes (
    user_id,
    remind_me_at
) VALUES (
    $1, $2
)
RETURNING user_id, remind_me_at, created_at, updated_at;

-- name: GetHeroProfileByUserID :one
SELECT 
    h.user_id,
    h.remind_me_at,
    h.created_at,
    h.updated_at,
    u.email,
    u.name,
    u.phone_number,
    u.profile_pic,
    u.pfp_external_url,
    u.bio,
    u.address,
    u.is_active,
    u.email_verified_at
FROM heroes h
JOIN users u ON u.id = h.user_id
WHERE h.user_id = $1 AND u.deleted_at IS NULL
LIMIT 1;

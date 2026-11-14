-- name: GetUserByEmail :one
SELECT id, email, name, password_hash, phone_number, profile_pic, pfp_external_url, bio, address, is_active, email_verified_at, created_at, updated_at, deleted_at
FROM users
WHERE LOWER(email) = LOWER($1) AND deleted_at IS NULL
LIMIT 1;

-- name: GetUserWithRolesByEmail :one
SELECT 
    u.id,
    u.email,
    u.name,
    u.password_hash,
    u.phone_number,
    u.profile_pic,
    u.pfp_external_url,
    u.bio,
    u.address,
    u.is_active,
    u.email_verified_at,
    u.created_at,
    u.updated_at,
    s.role AS staff_role,
    s.division_id AS staff_division_id,
    d.name AS staff_division_name,
    (s.user_id IS NOT NULL)::BOOLEAN AS is_staff,
    (h.user_id IS NOT NULL)::BOOLEAN AS is_hero
FROM users u
LEFT JOIN staff s ON s.user_id = u.id AND s.deleted_at IS NULL
LEFT JOIN divisions d ON d.id = s.division_id AND d.deleted_at IS NULL
LEFT JOIN heroes h ON h.user_id = u.id
WHERE LOWER(u.email) = LOWER($1) AND u.deleted_at IS NULL
LIMIT 1;

-- name: GetUserWithRolesByID :one
SELECT 
    u.id,
    u.email,
    u.name,
    u.password_hash,
    u.phone_number,
    u.profile_pic,
    u.pfp_external_url,
    u.bio,
    u.address,
    u.is_active,
    u.email_verified_at,
    u.created_at,
    u.updated_at,
    s.role AS staff_role,
    s.division_id AS staff_division_id,
    d.name AS staff_division_name,
    (s.user_id IS NOT NULL)::BOOLEAN AS is_staff,
    (h.user_id IS NOT NULL)::BOOLEAN AS is_hero
FROM users u
LEFT JOIN staff s ON s.user_id = u.id AND s.deleted_at IS NULL
LEFT JOIN divisions d ON d.id = s.division_id AND d.deleted_at IS NULL
LEFT JOIN heroes h ON h.user_id = u.id
WHERE u.id = $1 AND u.deleted_at IS NULL
LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (
    email,
    name,
    password_hash,
    phone_number,
    pfp_external_url,
    is_active,
    email_verified_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING id, email, name, phone_number, is_active, email_verified_at, created_at, updated_at;

-- name: MarkEmailVerified :exec
UPDATE users
SET email_verified_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;

-- name: ClearUserPassword :exec
UPDATE users
SET password_hash = NULL,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;

-- name: UserExists :one
SELECT EXISTS(
    SELECT 1 FROM users
    WHERE id = $1 AND deleted_at IS NULL
)::bool;

-- name: GetUserBanTargetProfiles :one
SELECT
    EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL)::bool AS user_exists,
    EXISTS(SELECT 1 FROM heroes WHERE user_id = $1)::bool AS is_hero,
    EXISTS(SELECT 1 FROM staff WHERE user_id = $1 AND deleted_at IS NULL)::bool AS is_staff;

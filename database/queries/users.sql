-- name: GetUserByEmail :one
SELECT id, division_id, email, password_hash, role, name, phone_number, profile_pic, bio, address, is_active, email_verified_at, created_at, updated_at, deleted_at
FROM users
WHERE email = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: GetUserByID :one
SELECT id, division_id, email, password_hash, role, name, phone_number, profile_pic, bio, address, is_active, email_verified_at, created_at, updated_at, deleted_at
FROM users
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (
    division_id,
    email,
    password_hash,
    role,
    name,
    phone_number,
    is_active,
    email_verified_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING id, division_id, email, password_hash, role, name, phone_number, profile_pic, bio, address, is_active, email_verified_at, created_at, updated_at;

-- name: UpdateUserPassword :one
UPDATE users
SET password_hash = $2,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, updated_at;

-- name: CheckEmailExists :one
SELECT EXISTS (
    SELECT 1 FROM users WHERE email = $1 AND deleted_at IS NULL
) AS exists;

-- name: CountActiveSuperAdmins :one
SELECT COUNT(*)::BIGINT
FROM staff
WHERE role = 'super' AND deleted_at IS NULL;

-- name: GetStaffByUserID :one
SELECT user_id, division_id, role, created_at, updated_at, deleted_at
FROM staff
WHERE user_id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: GetStaffDetailsByUserID :one
SELECT 
    s.user_id,
    u.email,
    u.name,
    u.phone_number,
    u.profile_pic,
    u.pfp_external_url,
    u.is_active,
    s.division_id,
    d.name AS division_name,
    s.role,
    s.created_at,
    s.updated_at
FROM staff s
JOIN users u ON u.id = s.user_id
JOIN divisions d ON d.id = s.division_id
WHERE s.user_id = $1 AND s.deleted_at IS NULL AND u.deleted_at IS NULL
LIMIT 1;

-- name: CreateStaff :one
INSERT INTO staff (
    user_id,
    division_id,
    role
) VALUES (
    $1, $2, $3
)
ON CONFLICT (user_id) DO UPDATE
SET division_id = EXCLUDED.division_id,
    role = EXCLUDED.role,
    deleted_at = NULL,
    updated_at = NOW()
RETURNING user_id, division_id, role, created_at, updated_at;

-- name: UpdateStaffRole :one
UPDATE staff
SET role = $2,
    updated_at = NOW()
WHERE user_id = $1 AND deleted_at IS NULL
RETURNING user_id, division_id, role, updated_at;

-- name: UpdateStaffDivision :one
UPDATE staff
SET division_id = $2,
    updated_at = NOW()
WHERE user_id = $1 AND deleted_at IS NULL
RETURNING user_id, division_id, role, updated_at;

-- name: UpdateStaffRoleAndDivision :one
UPDATE staff
SET role = $2,
    division_id = $3,
    deleted_at = NULL,
    updated_at = NOW()
WHERE user_id = $1
RETURNING user_id, division_id, role, updated_at;

-- name: SoftDeleteStaff :one
UPDATE staff
SET deleted_at = NOW(),
    updated_at = NOW()
WHERE user_id = $1 AND deleted_at IS NULL
RETURNING user_id, division_id, role, deleted_at;

-- name: ListStaff :many
SELECT 
    s.user_id,
    u.email,
    u.name,
    u.phone_number,
    u.profile_pic,
    u.pfp_external_url,
    u.is_active,
    s.division_id,
    d.name AS division_name,
    s.role,
    s.created_at,
    s.updated_at
FROM staff s
JOIN users u ON u.id = s.user_id
JOIN divisions d ON d.id = s.division_id
WHERE s.deleted_at IS NULL 
  AND u.deleted_at IS NULL
  AND (sqlc.narg('division_id')::integer IS NULL OR s.division_id = sqlc.narg('division_id'))
  AND (sqlc.narg('role')::user_role IS NULL OR s.role = sqlc.narg('role'))
  AND (sqlc.narg('search')::text IS NULL OR u.name ILIKE '%' || sqlc.narg('search') || '%' OR u.email ILIKE '%' || sqlc.narg('search') || '%')
ORDER BY 
    CASE s.role 
        WHEN 'super' THEN 1 
        WHEN 'core' THEN 2 
        WHEN 'staff' THEN 3 
        ELSE 4 
    END, 
    u.name ASC
LIMIT $1 OFFSET $2;

-- name: CountStaff :one
SELECT COUNT(*)
FROM staff s
JOIN users u ON u.id = s.user_id
WHERE s.deleted_at IS NULL 
  AND u.deleted_at IS NULL
  AND (sqlc.narg('division_id')::integer IS NULL OR s.division_id = sqlc.narg('division_id'))
  AND (sqlc.narg('role')::user_role IS NULL OR s.role = sqlc.narg('role'))
  AND (sqlc.narg('search')::text IS NULL OR u.name ILIKE '%' || sqlc.narg('search') || '%' OR u.email ILIKE '%' || sqlc.narg('search') || '%');

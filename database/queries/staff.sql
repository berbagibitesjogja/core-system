-- name: GetStaffByUserID :one
SELECT user_id, division_id, role, created_at, updated_at
FROM staff
WHERE user_id = $1
LIMIT 1;

-- name: CreateStaff :one
INSERT INTO staff (
    user_id,
    division_id,
    role
) VALUES (
    $1, $2, $3
)
RETURNING user_id, division_id, role, created_at, updated_at;

-- name: UpdateStaffRoleAndDivision :one
UPDATE staff
SET role = $2,
    division_id = $3,
    updated_at = NOW()
WHERE user_id = $1
RETURNING user_id, division_id, role, updated_at;

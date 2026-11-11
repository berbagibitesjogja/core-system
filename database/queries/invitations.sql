-- name: CreateInvitation :one
INSERT INTO user_invitations (
    invited_by,
    email,
    role,
    division_id,
    name,
    phone_number,
    token_hash,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING id, invited_by, email, role, division_id, name, phone_number, token_hash, expires_at, accepted_at, created_at;

-- name: GetInvitationByTokenHash :one
SELECT 
    i.id, i.invited_by, i.email, i.role, i.division_id, i.name, i.phone_number, i.token_hash, i.expires_at, i.accepted_at, i.created_at,
    d.name AS division_name
FROM user_invitations i
JOIN divisions d ON d.id = i.division_id
WHERE i.token_hash = $1
LIMIT 1;

-- name: MarkInvitationAccepted :one
UPDATE user_invitations
SET accepted_at = NOW()
WHERE id = $1 AND accepted_at IS NULL
RETURNING id, accepted_at;

-- name: CheckPendingInvitationByEmail :one
SELECT EXISTS (
    SELECT 1 FROM user_invitations
    WHERE LOWER(email) = LOWER($1) AND accepted_at IS NULL AND expires_at > NOW()
) AS exists;

-- name: GetActiveInvitationByEmail :one
SELECT 
    i.id, i.invited_by, i.email, i.role, i.division_id, i.name, i.phone_number, i.token_hash, i.expires_at, i.accepted_at, i.created_at,
    d.name AS division_name
FROM user_invitations i
JOIN divisions d ON d.id = i.division_id
WHERE LOWER(i.email) = LOWER($1) AND i.accepted_at IS NULL AND i.expires_at > NOW()
ORDER BY i.created_at DESC
LIMIT 1;

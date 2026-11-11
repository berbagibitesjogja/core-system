-- name: CreateVerificationCode :one
INSERT INTO verification_codes (
    user_id,
    purpose,
    code_hash,
    expires_at
) VALUES (
    $1, $2, $3, $4
)
RETURNING id, user_id, purpose, code_hash, attempts, expires_at, used_at, created_at;

-- name: GetLatestActiveVerificationCode :one
SELECT id, user_id, purpose, code_hash, attempts, expires_at, used_at, created_at
FROM verification_codes
WHERE user_id = $1
  AND purpose = $2
  AND used_at IS NULL
  AND expires_at > NOW()
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementVerificationAttempts :one
UPDATE verification_codes
SET attempts = attempts + 1
WHERE id = $1
RETURNING id, attempts;

-- name: MarkVerificationCodeUsed :one
UPDATE verification_codes
SET used_at = NOW()
WHERE id = $1 AND used_at IS NULL
RETURNING id, used_at;

-- name: InvalidateOpenVerificationCodes :exec
UPDATE verification_codes
SET expires_at = NOW()
WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > NOW();

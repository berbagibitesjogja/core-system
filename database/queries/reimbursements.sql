-- name: CreateReimbursement :one
INSERT INTO reimbursements (
    user_id,
    amount,
    method,
    target,
    notes,
    status
) VALUES (
    $1, $2, $3, $4, $5, 'pending'
)
RETURNING id, user_id, amount, method, target, notes, status, payment_proof, paid_by, paid_at, rejection_reason, created_at, updated_at, deleted_at;

-- name: AddReimbursementFile :one
INSERT INTO reimbursement_files (
    reimbursement_id,
    file_id
) VALUES (
    $1, $2
)
RETURNING id, reimbursement_id, file_id, created_at;

-- name: GetReimbursementByID :one
SELECT id, user_id, amount, method, target, notes, status, payment_proof, paid_by, paid_at, rejection_reason, created_at, updated_at, deleted_at
FROM reimbursements
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: ListReimbursementFilesByReimbursementID :many
SELECT id, reimbursement_id, file_id, created_at
FROM reimbursement_files
WHERE reimbursement_id = $1
ORDER BY created_at ASC;

-- name: DeleteReimbursementFilesByReimbursementID :exec
DELETE FROM reimbursement_files
WHERE reimbursement_id = $1;

-- name: UpdateReimbursement :one
UPDATE reimbursements
SET
    amount = $2,
    method = $3,
    target = $4,
    notes = $5,
    status = $6,
    payment_proof = $7,
    paid_by = $8,
    paid_at = $9,
    rejection_reason = $10,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, user_id, amount, method, target, notes, status, payment_proof, paid_by, paid_at, rejection_reason, created_at, updated_at, deleted_at;

-- name: SoftDeleteReimbursement :one
UPDATE reimbursements
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, deleted_at;

-- name: ListReimbursements :many
SELECT id, user_id, amount, method, target, notes, status, payment_proof, paid_by, paid_at, rejection_reason, created_at, updated_at, deleted_at
FROM reimbursements
WHERE deleted_at IS NULL
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('search')::text IS NULL OR (notes ILIKE '%' || sqlc.narg('search') || '%' OR target ILIKE '%' || sqlc.narg('search') || '%'))
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountReimbursements :one
SELECT COUNT(*)
FROM reimbursements
WHERE deleted_at IS NULL
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('search')::text IS NULL OR (notes ILIKE '%' || sqlc.narg('search') || '%' OR target ILIKE '%' || sqlc.narg('search') || '%'));

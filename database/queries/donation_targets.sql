-- name: CreateDonationTarget :one
INSERT INTO donation_target (
    donation_id,
    beneficiary_category_id
) VALUES (
    $1, $2
)
RETURNING donation_id, beneficiary_category_id;

-- name: ListTargetsByDonationID :many
SELECT
    dt.donation_id,
    dt.beneficiary_category_id,
    bc.name AS category_name,
    bc.variant AS category_variant,
    bc.hidden AS category_hidden
FROM donation_target dt
JOIN beneficiary_categories bc ON bc.id = dt.beneficiary_category_id
WHERE dt.donation_id = $1
ORDER BY bc.name ASC;

-- name: ListTargetsByDonationIDs :many
SELECT
    dt.donation_id,
    dt.beneficiary_category_id,
    bc.name AS category_name,
    bc.variant AS category_variant,
    bc.hidden AS category_hidden
FROM donation_target dt
JOIN beneficiary_categories bc ON bc.id = dt.beneficiary_category_id
WHERE dt.donation_id = ANY($1::uuid[])
ORDER BY bc.name ASC;

-- name: DeleteTargetsByDonationID :exec
DELETE FROM donation_target
WHERE donation_id = $1;

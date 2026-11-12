-- name: CreateDonation :one
INSERT INTO donations (
    sponsor_id,
    created_by,
    event_at,
    quota,
    hero_quota,
    remain,
    address,
    maps,
    message,
    status,
    hide_from_public,
    hide_sponsor
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING id, sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor, created_at, updated_at, deleted_at;

-- name: GetDonationByID :one
SELECT id, sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor, created_at, updated_at, deleted_at
FROM donations
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: GetDonationByIDForUpdate :one
SELECT id, sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor, created_at, updated_at, deleted_at
FROM donations
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1
FOR UPDATE;

-- name: GetDonationDetailsByID :one
SELECT
    d.id, d.sponsor_id, d.created_by, d.event_at, d.quota, d.hero_quota, d.remain,
    d.address, d.maps, d.message, d.status, d.hide_from_public, d.hide_sponsor,
    d.created_at, d.updated_at, d.deleted_at,
    s.name AS sponsor_name,
    s.logo AS sponsor_logo,
    s.hidden AS sponsor_hidden,
    s.hide_donations AS sponsor_hide_donations,
    s.deleted_at AS sponsor_deleted_at,
    s.email AS sponsor_email,
    s.phone AS sponsor_phone,
    s.address AS sponsor_address,
    u.email AS created_by_email
FROM donations d
JOIN sponsors s ON s.id = d.sponsor_id
LEFT JOIN users u ON u.id = d.created_by
WHERE d.id = $1 AND d.deleted_at IS NULL
LIMIT 1;

-- name: UpdateDonation :one
UPDATE donations
SET
    sponsor_id = $2,
    event_at = $3,
    quota = $4,
    hero_quota = $5,
    remain = $6,
    address = $7,
    maps = $8,
    message = $9,
    status = $10,
    hide_from_public = $11,
    hide_sponsor = $12,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor, created_at, updated_at, deleted_at;

-- name: UpdateDonationStatus :one
UPDATE donations
SET
    status = $2,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor, created_at, updated_at, deleted_at;

-- name: SoftDeleteDonation :one
UPDATE donations
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, deleted_at;

-- name: ListDonationsStaff :many
SELECT
    d.id, d.sponsor_id, d.created_by, d.event_at, d.quota, d.hero_quota, d.remain,
    d.address, d.maps, d.message, d.status, d.hide_from_public, d.hide_sponsor,
    d.created_at, d.updated_at, d.deleted_at,
    s.name AS sponsor_name,
    s.logo AS sponsor_logo,
    s.hidden AS sponsor_hidden,
    s.hide_donations AS sponsor_hide_donations,
    s.deleted_at AS sponsor_deleted_at,
    s.email AS sponsor_email,
    s.phone AS sponsor_phone,
    s.address AS sponsor_address,
    u.email AS created_by_email
FROM donations d
JOIN sponsors s ON s.id = d.sponsor_id
LEFT JOIN users u ON u.id = d.created_by
WHERE d.deleted_at IS NULL
  AND (sqlc.narg('status')::donation_status IS NULL OR d.status = sqlc.narg('status'))
  AND (sqlc.narg('search')::text IS NULL OR d.address ILIKE '%' || sqlc.narg('search') || '%' ESCAPE '\' OR s.name ILIKE '%' || sqlc.narg('search') || '%' ESCAPE '\')
ORDER BY d.event_at DESC
LIMIT $1 OFFSET $2;

-- name: CountDonationsStaff :one
SELECT COUNT(*)
FROM donations d
JOIN sponsors s ON s.id = d.sponsor_id
WHERE d.deleted_at IS NULL
  AND (sqlc.narg('status')::donation_status IS NULL OR d.status = sqlc.narg('status'))
  AND (sqlc.narg('search')::text IS NULL OR d.address ILIKE '%' || sqlc.narg('search') || '%' ESCAPE '\' OR s.name ILIKE '%' || sqlc.narg('search') || '%' ESCAPE '\');

-- name: ListDonationsPublicOpen :many
SELECT
    d.id, d.event_at, d.quota, d.hero_quota, d.remain,
    d.address, d.maps, d.message, d.status, d.hide_sponsor,
    d.created_at, d.updated_at,
    s.id AS sponsor_id,
    s.name AS sponsor_name,
    s.logo AS sponsor_logo,
    (s.hidden OR s.hide_donations OR s.deleted_at IS NOT NULL)::boolean AS sponsor_hidden
FROM donations d
JOIN sponsors s ON s.id = d.sponsor_id
WHERE d.deleted_at IS NULL
  AND d.hide_from_public = FALSE
  AND (d.status = 'open' OR d.status = 'closed')
ORDER BY d.event_at ASC
LIMIT $1 OFFSET $2;

-- name: CountDonationsPublicOpen :one
SELECT COUNT(*)
FROM donations d
WHERE d.deleted_at IS NULL
  AND d.hide_from_public = FALSE
  AND (d.status = 'open' OR d.status = 'closed');

-- name: ListDonationsPublicPast :many
SELECT
    d.id, d.event_at, d.quota, d.hero_quota, d.remain,
    d.address, d.maps, d.message, d.status, d.hide_sponsor,
    d.created_at, d.updated_at,
    s.id AS sponsor_id,
    s.name AS sponsor_name,
    s.logo AS sponsor_logo,
    (s.hidden OR s.hide_donations OR s.deleted_at IS NOT NULL)::boolean AS sponsor_hidden
FROM donations d
JOIN sponsors s ON s.id = d.sponsor_id
WHERE d.deleted_at IS NULL
  AND d.hide_from_public = FALSE
  AND d.status = 'completed'
  AND d.event_at >= $1
ORDER BY d.event_at DESC
LIMIT $2 OFFSET $3;

-- name: CountDonationsPublicPast :one
SELECT COUNT(*)
FROM donations d
WHERE d.deleted_at IS NULL
  AND d.hide_from_public = FALSE
  AND d.status = 'completed'
  AND d.event_at >= $1;

-- name: TryAdvisoryXactLock :one
SELECT pg_try_advisory_xact_lock($1::bigint) AS locked;

-- name: AutoCompleteDonations :many
UPDATE donations
SET status = 'completed', updated_at = NOW()
WHERE deleted_at IS NULL
  AND (status = 'open' OR status = 'closed')
  AND event_at <= $1
RETURNING id;

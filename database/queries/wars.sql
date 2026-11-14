-- ============================================================================
-- HERO BAN ELIGIBILITY & BURN-OFF
-- ============================================================================

-- name: GetActiveHeroBans :many
SELECT ub.id, ub.user_id, ub.scope, ub.donation_id, ub.donation_count, ub.reason,
       ub.source_hero_attendance_id, ub.created_at,
       GREATEST(COALESCE(d.event_at, ub.created_at), ub.created_at)::timestamptz AS baseline_at
FROM user_bans ub
LEFT JOIN donations d ON d.id = ub.donation_id
WHERE ub.user_id = $1
  AND ub.scope IN ('hero_war', 'all')
  AND ub.lifted_at IS NULL;

-- name: CountCompletedEligibleDonationsForHero :one
SELECT COUNT(*)::int AS count
FROM donations d
WHERE d.status = 'completed'
  AND d.deleted_at IS NULL
  AND d.hero_quota > 0
  AND d.event_at > sqlc.arg('baseline_at')::timestamptz
  AND (
      NOT EXISTS (SELECT 1 FROM donation_target dt WHERE dt.donation_id = d.id)
      OR EXISTS (
          SELECT 1 FROM donation_target dt
          JOIN hero_categories hc ON dt.beneficiary_category_id = hc.beneficiary_category_id
          WHERE dt.donation_id = d.id AND hc.hero_id = $1
      )
  )
  AND NOT EXISTS (
      SELECT 1 FROM hero_attendances ha
      WHERE ha.donation_id = d.id
        AND ha.hero_id = $1
        AND ha.cancelled_at IS NULL
  );

-- ============================================================================
-- STAFF BAN ELIGIBILITY & BURN-OFF
-- ============================================================================

-- name: GetActiveStaffBans :many
SELECT ub.id, ub.user_id, ub.scope, ub.donation_id, ub.donation_count, ub.reason,
       ub.source_staff_attendance_id, ub.created_at,
       GREATEST(COALESCE(d.event_at, ub.created_at), ub.created_at)::timestamptz AS baseline_at
FROM user_bans ub
LEFT JOIN donations d ON d.id = ub.donation_id
WHERE ub.user_id = $1
  AND ub.scope IN ('staff_war', 'all')
  AND ub.lifted_at IS NULL;

-- name: CountCompletedEligibleDonationsForStaff :one
SELECT COUNT(*)::int AS count
FROM donations d
WHERE d.status = 'completed'
  AND d.deleted_at IS NULL
  AND d.event_at > sqlc.arg('baseline_at')::timestamptz
  AND EXISTS (
      SELECT 1 FROM war_slots ws
      WHERE ws.donation_id = d.id
        AND ws.division_id = $2
        AND ws.quota > 0
        AND ws.deleted_at IS NULL
  )
  AND NOT EXISTS (
      SELECT 1 FROM staff_attendances sa
      JOIN war_slots ws2 ON ws2.id = sa.war_slot_id
      WHERE ws2.donation_id = d.id
        AND sa.staff_id = $1
        AND sa.cancelled_at IS NULL
  );

-- ============================================================================
-- HEROES WAR CLAIMS, CANCEL & RESERVATIONS
-- ============================================================================

-- name: IsHeroEligibleForDonation :one
SELECT EXISTS(
    SELECT 1 FROM donation_target dt
    JOIN hero_categories hc ON dt.beneficiary_category_id = hc.beneficiary_category_id
    WHERE dt.donation_id = $1 AND hc.hero_id = $2
)::bool;

-- name: GetActiveHeroAttendance :one
SELECT id, donation_id, hero_id, status, cancelled_late, cancelled_at, attended_at, marked_by, notes, created_at, updated_at
FROM hero_attendances
WHERE donation_id = $1 AND hero_id = $2 AND cancelled_at IS NULL
LIMIT 1;

-- name: GetActiveHeroAttendanceForUpdate :one
SELECT id, status, created_at
FROM hero_attendances
WHERE donation_id = $1 AND hero_id = $2 AND cancelled_at IS NULL
LIMIT 1
FOR UPDATE;

-- name: HasPriorCancelledHeroAttendance :one
SELECT EXISTS(
    SELECT 1 FROM hero_attendances
    WHERE donation_id = $1 AND hero_id = $2 AND cancelled_at IS NOT NULL
)::bool;

-- name: CreateHeroAttendance :one
INSERT INTO hero_attendances (
    donation_id, hero_id, status
) VALUES (
    $1, $2, 'registered'
) RETURNING id, donation_id, hero_id, status, cancelled_late, cancelled_at, attended_at, marked_by, notes, created_at, updated_at;

-- name: CancelHeroAttendance :one
UPDATE hero_attendances
SET status = 'cancelled',
    cancelled_at = NOW(),
    cancelled_late = $2,
    updated_at = NOW()
WHERE id = $1 AND cancelled_at IS NULL
RETURNING id, donation_id, hero_id, status, cancelled_late, cancelled_at, attended_at, marked_by, notes, created_at, updated_at;

-- name: DecrementDonationRemain :one
UPDATE donations
SET remain = remain - 1,
    updated_at = NOW()
WHERE id = $1 AND remain > 0
RETURNING id, quota, hero_quota, remain, status;

-- name: IncrementDonationRemain :one
UPDATE donations
SET remain = remain + 1,
    updated_at = NOW()
WHERE id = $1 AND remain < hero_quota
RETURNING id, quota, hero_quota, remain, status;

-- name: ClearHeroRemindMeAt :exec
UPDATE heroes
SET remind_me_at = NULL,
    updated_at = NOW()
WHERE user_id = $1;

-- name: CountActiveHeroClaimsByDonation :one
SELECT COUNT(*)::int
FROM hero_attendances
WHERE donation_id = $1 AND cancelled_at IS NULL;

-- name: CountHeroReservations :one
SELECT COUNT(*)::int
FROM hero_attendances ha
JOIN donations d ON d.id = ha.donation_id
WHERE ha.hero_id = sqlc.arg('hero_id')
  AND d.deleted_at IS NULL
  AND (sqlc.narg('status')::attendance_status IS NULL OR ha.status = sqlc.narg('status')::attendance_status);

-- name: ListHeroReservations :many
SELECT ha.id, ha.donation_id, ha.hero_id, ha.status, ha.cancelled_late,
       ha.cancelled_at, ha.attended_at, ha.notes, ha.created_at, ha.updated_at,
       d.event_at, d.address, d.maps, d.message, d.status AS donation_status,
       s.name AS sponsor_name
FROM hero_attendances ha
JOIN donations d ON d.id = ha.donation_id
JOIN sponsors s ON s.id = d.sponsor_id
WHERE ha.hero_id = sqlc.arg('hero_id')
  AND d.deleted_at IS NULL
  AND (sqlc.narg('status')::attendance_status IS NULL OR ha.status = sqlc.narg('status')::attendance_status)
ORDER BY d.event_at DESC, ha.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- ============================================================================
-- SHARED LOCK HELPERS FOR WARS & ATTENDANCE
-- ============================================================================

-- name: GetDonationForShare :one
SELECT status, event_at
FROM donations
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1
FOR SHARE;

-- ============================================================================
-- WAR SLOTS (STAFF SHIFT QUOTAS)
-- ============================================================================

-- name: ListWarSlotsByDonation :many
SELECT ws.id, ws.donation_id, ws.division_id, ws.quota, ws.remain, ws.created_at, ws.updated_at, ws.deleted_at,
       d.name AS division_name,
       COUNT(sa.id) FILTER (WHERE sa.cancelled_at IS NULL)::int AS active_claims
FROM war_slots ws
JOIN divisions d ON d.id = ws.division_id
LEFT JOIN staff_attendances sa ON sa.war_slot_id = ws.id
WHERE ws.donation_id = $1 AND ws.deleted_at IS NULL
GROUP BY ws.id, d.name
ORDER BY ws.division_id ASC;

-- name: GetConfiguredWarSlotByID :one
SELECT ws.id, ws.donation_id, ws.division_id, ws.quota, ws.remain, ws.created_at, ws.updated_at,
       d.name AS division_name,
       COUNT(sa.id) FILTER (WHERE sa.cancelled_at IS NULL)::int AS active_claims
FROM war_slots ws
JOIN divisions d ON d.id = ws.division_id
LEFT JOIN staff_attendances sa ON sa.war_slot_id = ws.id
WHERE ws.id = $1 AND ws.deleted_at IS NULL
GROUP BY ws.id, d.name;

-- name: GetWarSlotByDonationAndDivisionForUpdate :one
SELECT id, quota, remain
FROM war_slots
WHERE donation_id = $1 AND division_id = $2 AND deleted_at IS NULL
LIMIT 1
FOR UPDATE;

-- name: GetWarSlotByIDForUpdate :one
SELECT id, donation_id, division_id, quota, remain
FROM war_slots
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1
FOR UPDATE;

-- name: ActiveWarSlotExistsForDonation :one
SELECT EXISTS(
    SELECT 1
    FROM war_slots ws
    JOIN donations d ON d.id = ws.donation_id
    WHERE ws.id = $1 AND ws.donation_id = $2 AND ws.deleted_at IS NULL AND d.deleted_at IS NULL
)::bool;

-- name: UpsertWarSlot :one
INSERT INTO war_slots (
    donation_id, division_id, quota, remain
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (donation_id, division_id) WHERE deleted_at IS NULL
DO UPDATE SET
    quota = EXCLUDED.quota,
    remain = EXCLUDED.remain,
    updated_at = NOW()
RETURNING id, donation_id, division_id, quota, remain, created_at, updated_at, deleted_at;

-- name: UpdateWarSlotQuota :one
UPDATE war_slots
SET quota = $2,
    remain = $3,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, donation_id, division_id, quota, remain, created_at, updated_at;

-- name: SoftDeleteWarSlot :execrows
UPDATE war_slots
SET deleted_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;

-- name: DecrementWarSlotRemain :one
UPDATE war_slots
SET remain = remain - 1,
    updated_at = NOW()
WHERE id = $1 AND remain > 0
RETURNING id, donation_id, division_id, quota, remain;

-- name: IncrementWarSlotRemain :one
UPDATE war_slots
SET remain = remain + 1,
    updated_at = NOW()
WHERE id = $1 AND remain < quota
RETURNING id, donation_id, division_id, quota, remain;

-- ============================================================================
-- STAFF ATTENDANCES (STAFF SHIFT CLAIMS & CHECK-IN)
-- ============================================================================

-- name: CountActiveStaffAttendancesBySlot :one
SELECT COUNT(*)
FROM staff_attendances
WHERE war_slot_id = $1 AND cancelled_at IS NULL;

-- name: ListActiveStaffVolunteersByDonation :many
SELECT sa.id, sa.war_slot_id, sa.staff_id, sa.status, sa.checkin_source, sa.attended_at, sa.created_at,
       u.name AS staff_name, u.email AS staff_email
FROM staff_attendances sa
JOIN war_slots ws ON ws.id = sa.war_slot_id
JOIN users u ON u.id = sa.staff_id
WHERE ws.donation_id = $1
  AND ws.deleted_at IS NULL
  AND sa.cancelled_at IS NULL
ORDER BY sa.created_at ASC;

-- name: GetActiveStaffAttendance :one
SELECT id, war_slot_id, staff_id, status, cancelled_late, cancelled_at, attended_at, marked_by, checkin_source, notes, created_at, updated_at
FROM staff_attendances
WHERE war_slot_id = $1 AND staff_id = $2 AND cancelled_at IS NULL
LIMIT 1;

-- name: GetActiveStaffAttendanceForUpdate :one
SELECT id, status, created_at
FROM staff_attendances
WHERE war_slot_id = $1 AND staff_id = $2 AND cancelled_at IS NULL
LIMIT 1
FOR UPDATE;

-- name: HasPriorCancelledStaffAttendance :one
SELECT EXISTS(
    SELECT 1 FROM staff_attendances sa
    JOIN war_slots ws ON ws.id = sa.war_slot_id
    WHERE ws.donation_id = $1 AND sa.staff_id = $2 AND sa.cancelled_at IS NOT NULL
)::bool;

-- name: GetLatestStaffAttendanceForCheckInForUpdate :one
SELECT id, status, cancelled_at
FROM staff_attendances
WHERE war_slot_id = $1 AND staff_id = $2
ORDER BY (cancelled_at IS NULL) DESC, created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: CreateStaffAttendance :one
INSERT INTO staff_attendances (
    war_slot_id, staff_id, status
) VALUES (
    $1, $2, 'registered'
) RETURNING id, war_slot_id, staff_id, status, cancelled_late, cancelled_at, attended_at, marked_by, checkin_source, notes, created_at, updated_at;

-- name: CancelStaffAttendance :one
UPDATE staff_attendances
SET status = 'cancelled',
    cancelled_at = NOW(),
    cancelled_late = $2,
    updated_at = NOW()
WHERE id = $1 AND cancelled_at IS NULL
RETURNING id, war_slot_id, staff_id, status, cancelled_late, cancelled_at, attended_at, marked_by, checkin_source, notes, created_at, updated_at;

-- name: StaffSelfCheckIn :one
UPDATE staff_attendances
SET status = 'attended',
    attended_at = NOW(),
    checkin_source = 'self',
    updated_at = NOW()
WHERE id = $1 AND status = 'registered' AND cancelled_at IS NULL
RETURNING id, war_slot_id, staff_id, status, cancelled_late, cancelled_at, attended_at, marked_by, checkin_source, notes, created_at, updated_at;

-- ============================================================================
-- ATTENDANCE LISTING & MARKING QUERIES
-- ============================================================================

-- name: CountHeroAttendancesByDonation :one
SELECT COUNT(*)::int
FROM hero_attendances ha
JOIN donations d ON d.id = ha.donation_id
WHERE ha.donation_id = sqlc.arg('donation_id')
  AND (
      (NOT sqlc.arg('unmarked_only')::boolean AND (sqlc.narg('status')::attendance_status IS NULL OR ha.status = sqlc.narg('status')::attendance_status))
      OR
      (sqlc.arg('unmarked_only')::boolean AND ha.status = 'registered' AND d.event_at < NOW() AND d.status != 'cancelled')
  );

-- name: ListHeroAttendancesByDonation :many
SELECT ha.id, ha.donation_id, ha.hero_id, ha.status, ha.cancelled_late,
       ha.cancelled_at, ha.attended_at, ha.marked_by, ha.notes, ha.created_at, ha.updated_at,
       u.name AS hero_name, u.email AS hero_email, u.phone_number AS hero_phone
FROM hero_attendances ha
JOIN donations d ON d.id = ha.donation_id
JOIN users u ON u.id = ha.hero_id
WHERE ha.donation_id = sqlc.arg('donation_id')
  AND (
      (NOT sqlc.arg('unmarked_only')::boolean AND (sqlc.narg('status')::attendance_status IS NULL OR ha.status = sqlc.narg('status')::attendance_status))
      OR
      (sqlc.arg('unmarked_only')::boolean AND ha.status = 'registered' AND d.event_at < NOW() AND d.status != 'cancelled')
  )
ORDER BY ha.created_at ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: GetHeroAttendanceForMarkForUpdate :one
SELECT hero_id, status
FROM hero_attendances
WHERE id = $1 AND donation_id = $2
LIMIT 1
FOR UPDATE;

-- name: CountStaffAttendancesBySlot :one
SELECT COUNT(*)::int
FROM staff_attendances sa
JOIN war_slots ws ON ws.id = sa.war_slot_id
JOIN donations d ON d.id = ws.donation_id
WHERE sa.war_slot_id = sqlc.arg('war_slot_id')
  AND (
      (NOT sqlc.arg('unmarked_only')::boolean AND (sqlc.narg('status')::attendance_status IS NULL OR sa.status = sqlc.narg('status')::attendance_status))
      OR
      (sqlc.arg('unmarked_only')::boolean AND sa.status = 'registered' AND d.event_at < NOW() AND d.status != 'cancelled')
  );

-- name: ListStaffAttendancesBySlot :many
SELECT sa.id, sa.war_slot_id, sa.staff_id, sa.status, sa.cancelled_late,
       sa.cancelled_at, sa.attended_at, sa.marked_by, sa.checkin_source, sa.notes, sa.created_at, sa.updated_at,
       u.name AS staff_name, u.email AS staff_email, u.phone_number AS staff_phone,
       s.division_id, div.name AS division_name
FROM staff_attendances sa
JOIN war_slots ws ON ws.id = sa.war_slot_id
JOIN donations d ON d.id = ws.donation_id
JOIN staff s ON s.user_id = sa.staff_id
JOIN divisions div ON div.id = s.division_id
JOIN users u ON u.id = sa.staff_id
WHERE sa.war_slot_id = sqlc.arg('war_slot_id')
  AND (
      (NOT sqlc.arg('unmarked_only')::boolean AND (sqlc.narg('status')::attendance_status IS NULL OR sa.status = sqlc.narg('status')::attendance_status))
      OR
      (sqlc.arg('unmarked_only')::boolean AND sa.status = 'registered' AND d.event_at < NOW() AND d.status != 'cancelled')
  )
ORDER BY sa.created_at ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: GetStaffAttendanceForMarkForUpdate :one
SELECT staff_id, status
FROM staff_attendances
WHERE id = $1 AND war_slot_id = $2
LIMIT 1
FOR UPDATE;

-- ============================================================================
-- ATTENDANCE MARKING & BANS UPSERT / AUTO-LIFT / LIST
-- ============================================================================

-- name: MarkHeroAttendance :one
UPDATE hero_attendances
SET status = $2::attendance_status,
    attended_at = CASE WHEN $2::attendance_status = 'attended'::attendance_status THEN NOW() ELSE NULL END,
    marked_by = $3,
    notes = $4,
    updated_at = NOW()
WHERE id = $1 AND status != 'cancelled'
RETURNING id, donation_id, hero_id, status, cancelled_late, cancelled_at, attended_at, marked_by, notes, created_at, updated_at;

-- name: MarkStaffAttendance :one
UPDATE staff_attendances
SET status = $2::attendance_status,
    attended_at = CASE WHEN $2::attendance_status = 'attended'::attendance_status THEN NOW() ELSE NULL END,
    marked_by = $3,
    checkin_source = 'coordinator',
    notes = $4,
    updated_at = NOW()
WHERE id = $1 AND status != 'cancelled'
RETURNING id, war_slot_id, staff_id, status, cancelled_late, cancelled_at, attended_at, marked_by, checkin_source, notes, created_at, updated_at;

-- name: UpsertHeroBanOnAbsent :one
INSERT INTO user_bans (
    user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id
) VALUES (
    $1, 'hero_war', $2, $3, 'no_show', $4
)
ON CONFLICT (source_hero_attendance_id)
DO UPDATE SET
    lifted_at = NULL,
    created_at = NOW(),
    donation_count = EXCLUDED.donation_count,
    note = 're-activated on absent mark'
WHERE user_bans.lifted_by IS NULL
  AND user_bans.lifted_at IS NOT NULL
RETURNING id, user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, source_staff_attendance_id, note, lifted_at, lifted_by, created_at;

-- name: UpsertHeroBanOnLateCancel :one
INSERT INTO user_bans (
    user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id
) VALUES (
    $1, 'hero_war', $2, $3, 'late_cancellation', $4
)
ON CONFLICT (source_hero_attendance_id)
DO UPDATE SET
    lifted_at = NULL,
    note = 're-activated on late cancellation'
WHERE user_bans.lifted_by IS NULL
RETURNING id, user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, source_staff_attendance_id, note, lifted_at, lifted_by, created_at;

-- name: UpsertStaffBanOnAbsent :one
INSERT INTO user_bans (
    user_id, scope, donation_id, donation_count, reason, source_staff_attendance_id
) VALUES (
    $1, 'staff_war', $2, $3, 'no_show', $4
)
ON CONFLICT (source_staff_attendance_id)
DO UPDATE SET
    lifted_at = NULL,
    created_at = NOW(),
    donation_count = EXCLUDED.donation_count,
    note = 're-activated on absent mark'
WHERE user_bans.lifted_by IS NULL
  AND user_bans.lifted_at IS NOT NULL
RETURNING id, user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, source_staff_attendance_id, note, lifted_at, lifted_by, created_at;

-- name: UpsertStaffBanOnLateCancel :one
INSERT INTO user_bans (
    user_id, scope, donation_id, donation_count, reason, source_staff_attendance_id
) VALUES (
    $1, 'staff_war', $2, $3, 'late_cancellation', $4
)
ON CONFLICT (source_staff_attendance_id)
DO UPDATE SET
    lifted_at = NULL,
    note = 're-activated on late cancellation'
WHERE user_bans.lifted_by IS NULL
RETURNING id, user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, source_staff_attendance_id, note, lifted_at, lifted_by, created_at;

-- name: AutoLiftHeroBan :exec
UPDATE user_bans
SET lifted_at = NOW(),
    note = 'auto-lifted on attendance correction'
WHERE source_hero_attendance_id = $1 AND lifted_by IS NULL;

-- name: AutoLiftStaffBan :exec
UPDATE user_bans
SET lifted_at = NOW(),
    note = 'auto-lifted on attendance correction'
WHERE source_staff_attendance_id = $1 AND lifted_by IS NULL;

-- name: AutoLiftSystemBansByDonation :exec
UPDATE user_bans
SET lifted_at = NOW(),
    note = 'auto-lifted: donation cancelled by staff'
WHERE donation_id = $1
  AND reason != 'manual'
  AND lifted_by IS NULL
  AND lifted_at IS NULL;

-- name: CreateManualBan :one
INSERT INTO user_bans (
    user_id, scope, donation_count, reason, note, created_by
) VALUES (
    $1, $2, $3, 'manual', $4, $5
) RETURNING id, user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, source_staff_attendance_id, note, lifted_at, lifted_by, created_at, created_by;

-- name: CheckActiveManualBan :one
SELECT EXISTS(
    SELECT 1 FROM user_bans
    WHERE user_id = $1
      AND reason = 'manual'
      AND lifted_at IS NULL
      AND (scope = $2 OR scope = 'all' OR $2 = 'all')
)::bool;

-- name: GetUserBanByID :one
SELECT id, user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, source_staff_attendance_id, note, lifted_at, lifted_by, created_at, created_by
FROM user_bans
WHERE id = $1
LIMIT 1;

-- name: CountUserBans :one
SELECT COUNT(*)::int
FROM user_bans
WHERE user_id = sqlc.arg('user_id')
  AND (sqlc.narg('scope')::ban_scope IS NULL OR scope = sqlc.narg('scope')::ban_scope)
  AND (NOT sqlc.arg('active_only')::boolean OR lifted_at IS NULL);

-- name: ListUserBans :many
SELECT id, user_id, scope, donation_id, donation_count, reason,
       source_hero_attendance_id, source_staff_attendance_id, note, created_by,
       lifted_at, lifted_by, created_at
FROM user_bans
WHERE user_id = sqlc.arg('user_id')
  AND (sqlc.narg('scope')::ban_scope IS NULL OR scope = sqlc.narg('scope')::ban_scope)
  AND (NOT sqlc.arg('active_only')::boolean OR lifted_at IS NULL)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: LiftUserBan :one
UPDATE user_bans
SET lifted_at = NOW(),
    lifted_by = $2,
    note = $3
WHERE id = $1
  AND lifted_at IS NULL
RETURNING id, user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, source_staff_attendance_id, note, lifted_at, lifted_by, created_at, created_by;

# API Contract: Donation Wars & Staff Shift Slots

> **Document Version:** `1.0.0`  
> **Topic Reference:** `015`, `016`, `017`, `018`  
> **Base Path:** `/api/v1`  
> **Security:** Bearer JWT (`Authorization: Bearer <token>`) & Token Bucket Rate Limiting (60 req/min per IP)  
> **Specification Format:** REST / JSON  

---

## 1. Global Standards & Conventions

### 1.1 Headers

| Header | Direction | Type | Description |
| :--- | :--- | :--- | :--- |
| `Content-Type` | Request / Response | String | `application/json; charset=utf-8` |
| `Authorization` | Request | String | `Bearer <access_token>` (Required for all endpoints in this contract) |
| `X-Request-Id` | Request / Response | String | Correlation ID format `ref_<hex>` (e.g. `ref_f5181fb0`). Propagated or auto-generated. |
| `Retry-After` | Response | Integer | Wait time in seconds when HTTP 429 is encountered. |

### 1.2 Response Envelopes

#### Standard Success (`200 OK`, `201 Created`)
```json
{
  "status": "success",
  "message": "Human-readable description of the completed action",
  "data": {}
}
```

#### Paginated Success (`200 OK`)
```json
{
  "status": "success",
  "message": "Reservations retrieved successfully",
  "data": [],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 42,
    "total_pages": 3
  }
}
```

#### Error Envelope (`4xx`, `5xx`)
```json
{
  "status": "error",
  "code": "ERROR_CODE_STRING",
  "message": "Safe human-readable summary of the error",
  "ref_id": "ref_a2a590df",
  "errors": {
    "field_name": "Specific actionable message for this field"
  }
}
```
> **Note:** Nullable fields on all War & Shift DTOs are serialized explicitly as `null` rather than omitted.

---

## 2. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `POST` | `/api/v1/donations/:id/claim` | Bearer JWT | Hero (`is_hero = true`) | Reserve a food package slot on an `open` donation (evaluates bans, target eligibility, and remaining quota). |
| `POST` | `/api/v1/donations/:id/cancel-claim` | Bearer JWT | Hero (`is_hero = true`) | Cancel the hero's active reservation before `event_at` (enforces grace period, anti-churn, and late penalty). |
| `GET` | `/api/v1/heroes/me/reservations` | Bearer JWT | Hero (`is_hero = true`) | Retrieve paginated reservation history for the calling hero with optional `?status=` filter. |
| `POST` | `/api/v1/donations/:id/war-slots` | Bearer JWT | Operational (`division_id = 3`) or `super` | Batch upsert division volunteer shift slots and quotas for a `draft` or `open` donation. |
| `GET` | `/api/v1/donations/:id/war-slots` | Bearer JWT | Staff (`is_staff = true`) | List all active war slots and enrolled volunteers for a donation. |
| `PATCH` | `/api/v1/donations/:id/war-slots/:slot_id` | Bearer JWT | Operational (`division_id = 3`) or `super` | Update the quota of a single active war slot while protecting active claims. |
| `DELETE` | `/api/v1/donations/:id/war-slots/:slot_id` | Bearer JWT | Operational (`division_id = 3`) or `super` | Soft-delete an active war slot (blocked if any active staff reservation exists). |
| `POST` | `/api/v1/donations/:id/war-slots/:slot_id/claim` | Bearer JWT | Staff (`is_staff = true`) | Claim a volunteer shift slot matching the caller's own division (evaluates staff bans and quota). |
| `POST` | `/api/v1/donations/:id/war-slots/:slot_id/cancel` | Bearer JWT | Staff (`is_staff = true`) | Cancel the caller's active staff shift claim before `event_at` (enforces grace period, anti-churn, and late penalty). |
| `POST` | `/api/v1/donations/:id/war-slots/:slot_id/check-in` | Bearer JWT | Staff (`is_staff = true`) | Self check-in for an active staff shift within the configurable event time window. |

---

## 3. Endpoint Specifications

### 3.1 Hero Claim Donation Slot

Atomically reserves a food package spot for the authenticated Hero.
1. Evaluates active Hero bans (`scope IN ('hero_war', 'all')`, `lifted_at IS NULL`) using dynamic completed-donation burn-off.
2. Evaluates target category eligibility if `donation_target` rows exist (open donations with zero targets are open to all Heroes).
3. Locks `donations` row `FOR UPDATE`, verifies `status = open` and `event_at > NOW()`, checks `remain > 0` and no existing active reservation (`cancelled_at IS NULL`), decrements `remain`, inserts a `registered` row into `hero_attendances`, and clears `heroes.remind_me_at`.

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/claim`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Authenticated Hero (`claims.IsHero == true`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Target donation ID. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Reservation created successfully",
  "data": {
    "id": "2672bb18-9c16-4fcd-8fe3-cc70a781d2e3",
    "donation_id": "00000000-0000-0000-0000-000000002002",
    "hero_id": "00000000-0000-0000-0000-000000000101",
    "status": "registered",
    "cancelled_late": false,
    "cancelled_at": null,
    "attended_at": null,
    "marked_by": null,
    "notes": null,
    "created_at": "2026-11-14T14:26:09.760851+07:00",
    "updated_at": "2026-11-14T14:26:09.760851+07:00"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_002`): Invalid donation ID format, or donation is not `open` / `event_at <= NOW()` (*`"Donation event is not open for reservations"`*).
* `403 Forbidden` (`FORBIDDEN`): Caller is not a Hero, or hero lacks matching enrolled beneficiary category for a targeted event (*`"You are not eligible for this targeted donation event"`*).
* `403 Forbidden` (`WAR_002`): Account is currently under an active `hero_war` or `all` ban (*`"Account is temporarily suspended from reserving food packages"`*).
* `404 Not Found` (`NOT_FOUND`): Donation not found or soft-deleted.
* `409 Conflict` (`WAR_001`): Zero remaining slots (`remain <= 0`).
* `409 Conflict` (`ALREADY_CLAIMED`): Hero already holds an active (`cancelled_at IS NULL`) reservation for this donation.

---

### 3.2 Hero Cancel Donation Claim

Cancels the calling Hero's active reservation for a donation and restores `donations.remain` by `1`.
- **Event Start Guard:** Rejects cancellation once `event_at <= NOW()` (`400 VAL_002`).
- **Status Guard:** Only `registered` reservations can be cancelled. Rejects `attended` (*`"Cannot cancel reservation after attendance has been recorded"`*) and any other non-`registered` state (`400 VAL_002`).
- **Late Penalty & Anti-Churn:**
  - If the donation itself is `cancelled`, late penalty is always waived (`cancelled_late: false`).
  - Otherwise, cancelling within `WAR_LATE_CANCELLATION_WINDOW` (default `12h` before `event_at`) is penalized unless it is the hero's first cancellation on that donation and occurs within `WAR_HERO_CANCEL_GRACE_PERIOD` (default `15m`) of `created_at`.
  - If penalized, records/reactivates a `hero_war` ban with `reason = late_cancellation` and `donation_count = WAR_HERO_BAN_EVENT_COUNT` (default `2`).

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/cancel-claim`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Authenticated Hero (`claims.IsHero == true`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Target donation ID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Reservation cancelled successfully",
  "data": {
    "cancelled_late": false
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_002`): Invalid donation ID, event already started, reservation already marked `attended`, or reservation status is not `registered`.
* `403 Forbidden`: Caller is not a Hero.
* `404 Not Found` (`NOT_FOUND`): Donation not found, or caller has no active (`cancelled_at IS NULL`) reservation for this donation.

---

### 3.3 List My Hero Reservations

Retrieves a paginated list of reservation records belonging to the calling Hero, ordered by `d.event_at DESC, ha.created_at DESC`. Excludes reservations whose parent donation has been soft-deleted (`d.deleted_at IS NULL`).

* **Method:** `GET`
* **Path:** `/api/v1/heroes/me/reservations`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Authenticated Hero (`claims.IsHero == true`).

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (values <= 0 or invalid fall back to `1`). |
| `limit` | integer | No | `20` | Items per page (`1..100`; out-of-range values fall back to `20`). |
| `status` | string | No | `""` | Filter by attendance status: `registered`, `attended`, `absent`, or `cancelled`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Reservations retrieved successfully",
  "data": [
    {
      "id": "2672bb18-9c16-4fcd-8fe3-cc70a781d2e3",
      "donation_id": "00000000-0000-0000-0000-000000002002",
      "hero_id": "00000000-0000-0000-0000-000000000101",
      "status": "registered",
      "cancelled_late": false,
      "cancelled_at": null,
      "attended_at": null,
      "notes": null,
      "created_at": "2026-11-14T14:26:09Z",
      "updated_at": "2026-11-14T14:26:09Z",
      "event_at": "2026-11-16T07:16:27Z",
      "address": "Jl. Gejayan No. 45, Depok, Sleman",
      "maps": "https://maps.google.com/?q=-7.775120,110.389410",
      "message": "Makan malam hangat terbuka untuk semua Hero",
      "donation_status": "open",
      "sponsor_name": "Yayasan Pangan Nusantara"
    }
  ],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 1,
    "total_pages": 1
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_001`): Invalid `?status=` filter (must be one of `registered`, `attended`, `absent`, `cancelled`).
* `403 Forbidden`: Caller is not a Hero.

---

### 3.4 Configure War Slots (Batch Upsert)

Creates or updates volunteer shift slot quotas by division for a `draft` or `open` donation.
- Accepts a non-empty JSON array of `{division_id, quota}` items.
- Rejects `division_id <= 0`, `quota <= 0`, and duplicate `division_id` entries (`400 VAL_001`).
- Sorts items by `division_id ASC` before locking to guarantee deadlock safety.
- For each existing active slot, enforces `new_quota >= active_claims` (`existing_quota - existing_remain`) and recalculates `remain = new_quota - active_claims`.

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/war-slots`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Operational staff (`division_id = 3`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Target donation ID. |

#### Request Body
```json
[
  {
    "division_id": 2,
    "quota": 2
  },
  {
    "division_id": 3,
    "quota": 4
  }
]
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `[].division_id` | int32 | **Yes** | Positive integer referencing an active (`deleted_at IS NULL`) row in `divisions(id)`. Cannot repeat in the same payload. |
| `[].quota` | int32 | **Yes** | Must be `> 0` and `>= active_claims` if the slot already exists. |

#### Responses

##### `200 OK`
Returns all active configured war slots for the donation, ordered by `division_id ASC`.
```json
{
  "status": "success",
  "message": "War slots configured successfully",
  "data": [
    {
      "id": 54,
      "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
      "division_id": 2,
      "division_name": "Managerial",
      "quota": 2,
      "remain": 2,
      "active_claims": 0,
      "created_at": "2026-11-14T14:26:09.580486+07:00",
      "updated_at": "2026-11-14T14:26:09.580486+07:00"
    },
    {
      "id": 55,
      "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
      "division_id": 3,
      "division_name": "Operational",
      "quota": 4,
      "remain": 3,
      "active_claims": 1,
      "created_at": "2026-11-14T14:26:09.580486+07:00",
      "updated_at": "2026-11-14T14:26:09.580486+07:00"
    }
  ]
}
```

##### Error Responses
* `400 Bad Request` (`VAL_001`): Invalid JSON, empty array, non-positive `division_id`/`quota`, or duplicate `division_id`.
* `400 Bad Request` (`VAL_002`): Invalid donation ID, non-existent/deleted division, or donation status is not `draft` or `open`.
* `403 Forbidden`: Caller is not Operational staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): Donation not found or soft-deleted.
* `409 Conflict` (`WAR_003`): Requested `quota` is lower than current `active_claims` for a division.

---

### 3.5 List War Slots

Retrieves all active (non-deleted) division war slots for a donation along with each slot's currently active volunteers (`cancelled_at IS NULL`), ordered by `ws.division_id ASC` and `sa.created_at ASC`.
> **Note:** Because there is at most one active slot per division per donation, this endpoint returns a direct JSON array rather than a paginated envelope.

* **Method:** `GET`
* **Path:** `/api/v1/donations/:id/war-slots`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Any authenticated Staff (`claims.IsStaff == true`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Target donation ID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "War slots retrieved successfully",
  "data": [
    {
      "id": 55,
      "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
      "division_id": 3,
      "division_name": "Operational",
      "quota": 2,
      "remain": 1,
      "active_claims": 1,
      "volunteers": [
        {
          "id": 54,
          "staff_id": "00000000-0000-0000-0000-000000000007",
          "staff_name": "Operational Staff",
          "staff_email": "op.sf@berbagibitesjogja.com",
          "status": "attended",
          "checkin_source": "self",
          "attended_at": "2026-11-14T14:26:09.715152+07:00",
          "created_at": "2026-11-14T14:26:09.690908+07:00"
        }
      ],
      "created_at": "2026-11-14T14:26:09.580486+07:00",
      "updated_at": "2026-11-14T14:26:09.690908+07:00"
    }
  ]
}
```

##### Error Responses
* `400 Bad Request` (`VAL_002`): Invalid donation ID format.
* `403 Forbidden`: Caller is not Staff.
* `404 Not Found` (`NOT_FOUND`): Donation not found or soft-deleted.

---

### 3.6 Update Single War Slot

Updates the `quota` (and recalculates `remain = quota - active_claims`) for a single active war slot belonging to a `draft` or `open` donation.

* **Method:** `PATCH`
* **Path:** `/api/v1/donations/:id/war-slots/:slot_id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Operational staff (`division_id = 3`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `slot_id` | int64 | **Yes** | Target war slot ID. |

#### Request Body
```json
{
  "quota": 5
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `quota` | int32 | **Yes** | Must be `> 0` and `>= active_claims`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "War slot updated successfully",
  "data": {
    "id": 55,
    "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
    "division_id": 3,
    "division_name": "Operational",
    "quota": 5,
    "remain": 4,
    "active_claims": 1,
    "created_at": "2026-11-14T14:26:09.580486+07:00",
    "updated_at": "2026-11-14T14:28:12.110244+07:00"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_001`): Malformed JSON or `quota <= 0`.
* `400 Bad Request` (`VAL_002`): Invalid donation/slot ID, or donation is not `draft`/`open`.
* `403 Forbidden`: Caller is not Operational staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): Donation or war slot not found (including cross-donation or soft-deleted slots).
* `409 Conflict` (`WAR_003`): `quota < active_claims`.

---

### 3.7 Soft-Delete War Slot

Soft-deletes (`deleted_at = NOW()`) a single active war slot for a `draft` or `open` donation.
- Rejects deletion with `409 WAR_004` if any active staff reservation (`cancelled_at IS NULL`) exists on the slot.
- Preserves historical `cancelled` rows and ban FK references (`user_bans.source_staff_attendance_id`).
- Because uniqueness uses `UNIQUE (donation_id, division_id) WHERE deleted_at IS NULL`, a soft-deleted division slot can later be re-created cleanly via `POST /api/v1/donations/:id/war-slots` with a new `slot_id`.

* **Method:** `DELETE`
* **Path:** `/api/v1/donations/:id/war-slots/:slot_id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Operational staff (`division_id = 3`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `slot_id` | int64 | **Yes** | Target war slot ID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "War slot deleted successfully",
  "data": {
    "id": 54
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_002`): Invalid donation/slot ID, or donation is not `draft`/`open`.
* `403 Forbidden`: Caller is not Operational staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): Donation or active war slot not found for this donation.
* `409 Conflict` (`WAR_004`): Slot has active staff reservations (`cancelled_at IS NULL`).

---

### 3.8 Claim Staff Shift Slot

Atomically claims a staff volunteer shift slot for the authenticated Staff member.
1. Reads caller's current `staff.division_id` from database.
2. Evaluates active Staff bans (`scope IN ('staff_war', 'all')`, `lifted_at IS NULL`) using dynamic completed-shift burn-off for `staffDivisionID`.
3. Locks `donations` (`FOR SHARE`) and `war_slots` (`FOR UPDATE`), verifies `status = open` and `event_at > NOW()`, verifies `slot.division_id == caller.division_id`, checks `remain > 0` and no existing active reservation (`cancelled_at IS NULL`), decrements `war_slots.remain`, and inserts a `registered` row into `staff_attendances`.

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/war-slots/:slot_id/claim`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Authenticated Staff (`claims.IsStaff == true`) belonging to the slot's division.

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `slot_id` | int64 | **Yes** | Target war slot ID. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Shift claimed successfully",
  "data": {
    "id": 54,
    "war_slot_id": 55,
    "staff_id": "00000000-0000-0000-0000-000000000007",
    "status": "registered",
    "cancelled_late": false,
    "cancelled_at": null,
    "attended_at": null,
    "marked_by": null,
    "checkin_source": null,
    "notes": null,
    "created_at": "2026-11-14T14:26:09.690908+07:00",
    "updated_at": "2026-11-14T14:26:09.690908+07:00"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_002`): Invalid IDs, or donation is not `open` / `event_at <= NOW()`.
* `403 Forbidden`: Caller is not Staff, lacks a staff profile, or slot belongs to a different division (*`"You can only claim shifts for your own division"`*).
* `403 Forbidden` (`WAR_002`): Caller has an active `staff_war` or `all` ban (*`"Account is temporarily suspended from staff shifts"`*).
* `404 Not Found` (`NOT_FOUND`): Donation or war slot not found (or soft-deleted / cross-donation).
* `409 Conflict` (`WAR_001`): No shift slots remaining (`remain <= 0`).
* `409 Conflict` (`ALREADY_CLAIMED`): Caller already has an active reservation for this slot.

---

### 3.9 Cancel Staff Shift Slot

Cancels the calling Staff member's active shift reservation before `event_at` and increments `war_slots.remain` by `1`.
- **Status Guard:** Only `registered` reservations can be cancelled. Rejects `attended` (*`"Cannot cancel shift reservation after attendance has been recorded"`*) and any non-`registered` state (`400 VAL_002`).
- **Late Penalty & Anti-Churn:**
  - Waived if the donation is `cancelled`.
  - Within `WAR_LATE_CANCELLATION_WINDOW` (default `12h`), penalized unless it is the staff member's first cancellation across the donation's war slots and within `WAR_STAFF_CANCEL_GRACE_PERIOD` (default `15m`) of `created_at`.
  - If penalized, records/reactivates a `staff_war` ban with `reason = late_cancellation` and `donation_count = WAR_STAFF_BAN_EVENT_COUNT` (default `1`).

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/war-slots/:slot_id/cancel`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Authenticated Staff (`claims.IsStaff == true`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `slot_id` | int64 | **Yes** | Target war slot ID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Shift reservation cancelled successfully",
  "data": {
    "cancelled_late": false
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_002`): Invalid IDs, event already started, shift already `attended` (including self check-in), or status is not `registered`.
* `403 Forbidden`: Caller is not Staff.
* `404 Not Found` (`NOT_FOUND`): Donation, war slot, or active (`cancelled_at IS NULL`) shift reservation not found.

---

### 3.10 Staff Self Check-In

Allows a claiming Staff member to record self attendance for their active shift within the configurable check-in window.
- **Donation Status Guard:** Allowed when donation status is `open`, `closed`, or `completed`; rejected for `draft` or `cancelled`.
- **Time Window Guard:** Allowed strictly between `event_at - WAR_STAFF_SELF_CHECKIN_BEFORE` (default `1h`) and `event_at + WAR_STAFF_SELF_CHECKIN_AFTER` (default `4h`).
- **Active Row Preference:** Locks the most recent active (`cancelled_at IS NULL`) reservation first, supporting claim -> cancel -> re-claim sequences cleanly.
- **Idempotency & Conflicts:**
  - If status is already `attended`, returns `200 OK` (`"Already checked in"`, `{"status": "attended"}`).
  - If reservation is `cancelled` or already marked `absent` by a coordinator, returns `400 VAL_002`.

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/war-slots/:slot_id/check-in`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Authenticated Staff (`claims.IsStaff == true`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `slot_id` | int64 | **Yes** | Target war slot ID. |

#### Responses

##### `200 OK` (Fresh Check-In)
```json
{
  "status": "success",
  "message": "Self check-in completed successfully",
  "data": {
    "id": 54,
    "war_slot_id": 55,
    "staff_id": "00000000-0000-0000-0000-000000000007",
    "status": "attended",
    "cancelled_late": false,
    "cancelled_at": null,
    "attended_at": "2026-11-14T14:26:09.715152+07:00",
    "marked_by": null,
    "checkin_source": "self",
    "notes": null,
    "created_at": "2026-11-14T14:26:09.690908+07:00",
    "updated_at": "2026-11-14T14:26:09.715152+07:00"
  }
}
```

##### `200 OK` (Idempotent Repeat Call)
```json
{
  "status": "success",
  "message": "Already checked in",
  "data": {
    "status": "attended"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_002`): Invalid IDs, `draft`/`cancelled` donation, outside self check-in time window, cancelled reservation, or already marked `absent`.
* `403 Forbidden`: Caller is not Staff.
* `404 Not Found` (`NOT_FOUND`): Donation, war slot, or shift reservation not found.

---

## 4. Domain Invariants & Configuration Reference

### 4.1 Concurrency & Lock Hierarchy
To eliminate deadlocks under concurrent war bursts, all mutating transactions acquire row locks in strict hierarchy order:
1. `donations` (`FOR UPDATE` for hero quota mutations; `FOR SHARE` for staff war-slot mutations).
2. `war_slots` (batch operations sorted ascending by `division_id`).
3. `hero_attendances` / `staff_attendances`.
4. `user_bans`.

### 4.2 Environment Configuration Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `WAR_LATE_CANCELLATION_WINDOW` | `12h` | Duration before `event_at` inside which cancellations incur a late penalty (unless exempted by grace period). |
| `WAR_HERO_CANCEL_GRACE_PERIOD` | `15m` | Initial post-claim grace period during which a hero's first cancellation on a donation avoids penalty. |
| `WAR_STAFF_CANCEL_GRACE_PERIOD` | `15m` | Initial post-claim grace period during which a staff member's first cancellation on a donation avoids penalty. |
| `WAR_HERO_BAN_EVENT_COUNT` | `2` | Eligible completed events required to burn off a hero late-cancellation ban. |
| `WAR_STAFF_BAN_EVENT_COUNT` | `1` | Eligible completed events required to burn off a staff late-cancellation ban. |
| `WAR_STAFF_SELF_CHECKIN_BEFORE` | `1h` | How early before `event_at` staff self check-in opens. |
| `WAR_STAFF_SELF_CHECKIN_AFTER` | `4h` | How long after `event_at` staff self check-in remains open. |

---

## 5. Error Code Reference

| Code | HTTP Status | Meaning |
| :--- | :--- | :--- |
| `VAL_001` | `400 Bad Request` | Request payload/query validation failed (malformed JSON, invalid `?status=` filter, non-positive quota/division, duplicate division). |
| `VAL_002` | `400 Bad Request` | Business guardrail or ID violation (invalid UUID/slot ID, closed/past donation, outside check-in window, cancelling after `attended`). |
| `UNAUTHORIZED` | `401 Unauthorized` | Missing, expired, or invalid JWT access token. |
| `FORBIDDEN` | `403 Forbidden` | Role/division mismatch or beneficiary target category ineligibility. |
| `NOT_FOUND` | `404 Not Found` | Donation, war slot, or active reservation does not exist (or was soft-deleted / cross-donation). |
| `WAR_001` | `409 Conflict` | Quota exhausted (`remain <= 0`). |
| `WAR_002` | `403 Forbidden` | Caller is blocked from claiming by an active suspension in `user_bans`. |
| `WAR_003` | `409 Conflict` | Requested slot `quota` is below currently `active_claims`. |
| `WAR_004` | `409 Conflict` | Cannot delete war slot because active staff claims (`cancelled_at IS NULL`) exist. |
| `ALREADY_CLAIMED` | `409 Conflict` | Caller already holds an active reservation for this donation or shift slot. |
| `INTERNAL_ERROR` | `500 Internal Server Error` | Unexpected server or database exception (logged server-side with `ref_id`). |

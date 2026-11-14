# API Contract: Attendance Tracking & Ban Governance

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
  "message": "Hero attendances retrieved successfully",
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
> **Note:** Nullable fields across all Attendance and Ban DTOs are emitted explicitly as `null` rather than omitted.

---

## 2. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/donations/:id/attendances` | Bearer JWT | Operational (`division_id = 3`) or `super` | List paginated Hero attendances for a donation with optional `?status=` filter (including `unmarked`). |
| `POST` | `/api/v1/donations/:id/attendances/:attendance_id` | Bearer JWT | Operational (`division_id = 3`) or `super` | Mark a Hero attendance as `attended` or `absent` (manages no-show penalties and auto-lifts). |
| `GET` | `/api/v1/donations/:id/war-slots/:slot_id/attendances` | Bearer JWT | Core (`role IN ('core', 'super')`) | List paginated Staff shift attendances for a specific war slot with optional `?status=` filter. |
| `POST` | `/api/v1/donations/:id/war-slots/:slot_id/attendances/:staff_attendance_id` | Bearer JWT | Core (`role IN ('core', 'super')`) | Mark a Staff shift attendance as `attended` or `absent` (`checkin_source = 'coordinator'`). |
| `GET` | `/api/v1/users/:id/bans` | Bearer JWT | Core (`role IN ('core', 'super')`) | List paginated ban history for a user with optional `?scope=` and `?active_only=` filters. |
| `POST` | `/api/v1/users/:id/bans` | Bearer JWT | Core (`role IN ('core', 'super')`) | Issue a manual suspension against a Hero and/or Staff account. |
| `POST` | `/api/v1/bans/:id/lift` | Bearer JWT | Core (`role IN ('core', 'super')`) | Manually pardon (`lift`) an active ban, recording coordinator attribution and optional audit note. |

---

## 3. Endpoint Specifications

### 3.1 List Hero Attendances

Retrieves a paginated list of Hero attendance records for a donation, ordered by `ha.created_at ASC`.

* **Method:** `GET`
* **Path:** `/api/v1/donations/:id/attendances`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Operational staff (`division_id = 3`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Target donation ID. |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (values <= 0 or invalid fall back to `1`). |
| `limit` | integer | No | `20` | Items per page (`1..100`; out-of-range values fall back to `20`). |
| `status` | string | No | `""` | Filter by `unmarked`, `registered`, `attended`, `absent`, or `cancelled`. `unmarked` matches `ha.status = 'registered' AND d.event_at < NOW() AND d.status != 'cancelled'`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Hero attendances retrieved successfully",
  "data": [
    {
      "id": "2672bb18-9c16-4fcd-8fe3-cc70a781d2e3",
      "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
      "hero_id": "00000000-0000-0000-0000-000000000101",
      "hero_name": "Hero Mahasiswa UGM",
      "hero_email": "hero.ugm@mail.ugm.ac.id",
      "hero_phone": "6200000000101",
      "status": "attended",
      "cancelled_late": false,
      "cancelled_at": null,
      "attended_at": "2026-11-14T14:26:09.799452+07:00",
      "marked_by": "00000000-0000-0000-0000-000000000006",
      "notes": null,
      "created_at": "2026-11-14T14:26:09.760851+07:00",
      "updated_at": "2026-11-14T14:26:09.799452+07:00"
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
* `400 Bad Request` (`VAL_001`): Invalid `?status=` filter value.
* `400 Bad Request` (`VAL_002`): Invalid donation ID format.
* `403 Forbidden`: Caller is not Operational staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): Donation not found or soft-deleted.

---

### 3.2 Mark Hero Attendance

Marks a Hero's reservation as `attended` or `absent` and reconciles automated `user_bans` entries inside a single transaction.
- **Donation Guard:** Allowed only when donation status is `open`, `closed`, or `completed` (`draft` and `cancelled` rejected with `400 VAL_002`).
- **Pre-Event Time Guards:**
  - Marking `absent` is blocked before `event_at` (`400 CANNOT_MARK_ABSENT_BEFORE_EVENT_START`).
  - Marking `attended` opens at `event_at - WAR_HERO_ATTENDANCE_WINDOW_BEFORE` (default `1h` before `event_at`; earlier calls fail with `400 ATTENDANCE_WINDOW_NOT_OPEN`).
- **Cancelled Reservation Guard:** Rejects reservations already in `cancelled` status (`400 VAL_002`).
- **Ban Side Effects:**
  - `absent`: Inserts or re-activates a `hero_war` ban (`reason = no_show`, `donation_count = WAR_HERO_BAN_EVENT_COUNT`, default `2`), strictly preserving any existing manual pardon (`WHERE lifted_by IS NULL`).
  - `attended`: Auto-lifts any system ban linked to `source_hero_attendance_id` where `lifted_by IS NULL` (setting `note = "auto-lifted on attendance correction"`).

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/attendances/:attendance_id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Operational staff (`division_id = 3`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `attendance_id` | string (UUID) | **Yes** | Target `hero_attendances(id)`. |

#### Request Body
```json
{
  "status": "attended",
  "notes": "Verified at gate A"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `status` | string | **Yes** | Must be `"attended"` or `"absent"` (case-insensitive trimmed). |
| `notes` | string | No | Optional coordinator remarks. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Attendance marked successfully",
  "data": {
    "id": "2672bb18-9c16-4fcd-8fe3-cc70a781d2e3",
    "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
    "hero_id": "00000000-0000-0000-0000-000000000101",
    "status": "attended",
    "cancelled_late": false,
    "cancelled_at": null,
    "attended_at": "2026-11-14T14:26:09.799452+07:00",
    "marked_by": "00000000-0000-0000-0000-000000000006",
    "notes": "Verified at gate A",
    "created_at": "2026-11-14T14:26:09.760851+07:00",
    "updated_at": "2026-11-14T14:26:09.799452+07:00"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_001`): Malformed JSON or `status` is not `'attended'` / `'absent'`.
* `400 Bad Request` (`VAL_002`): Invalid IDs, `draft`/`cancelled` donation, or `cancelled` reservation.
* `400 Bad Request` (`CANNOT_MARK_ABSENT_BEFORE_EVENT_START`): Attempted to mark `absent` before `event_at`.
* `400 Bad Request` (`ATTENDANCE_WINDOW_NOT_OPEN`): Attempted to mark `attended` prior to `event_at - WAR_HERO_ATTENDANCE_WINDOW_BEFORE`.
* `403 Forbidden`: Caller is not Operational staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): Donation or attendance record not found for this donation.

---

### 3.3 List Staff Attendances

Retrieves a paginated list of Staff shift attendance records for a specific active war slot, ordered by `sa.created_at ASC`. Verifies both the war slot and its parent donation are active (`ws.deleted_at IS NULL AND d.deleted_at IS NULL`).

* **Method:** `GET`
* **Path:** `/api/v1/donations/:id/war-slots/:slot_id/attendances`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Core staff (`role = core`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `slot_id` | int64 | **Yes** | Target war slot ID belonging to the donation. |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number. |
| `limit` | integer | No | `20` | Items per page (`1..100`). |
| `status` | string | No | `""` | Filter by `unmarked`, `registered`, `attended`, `absent`, or `cancelled`. `unmarked` matches `sa.status = 'registered' AND d.event_at < NOW() AND d.status != 'cancelled'`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Staff attendances retrieved successfully",
  "data": [
    {
      "id": 54,
      "war_slot_id": 55,
      "staff_id": "00000000-0000-0000-0000-000000000007",
      "staff_name": "Operational Staff",
      "staff_email": "op.sf@berbagibitesjogja.com",
      "staff_phone": "6200000000007",
      "division_id": 3,
      "division_name": "Operational",
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
* `400 Bad Request` (`VAL_001`): Invalid `?status=` filter value.
* `400 Bad Request` (`VAL_002`): Invalid donation ID or slot ID.
* `403 Forbidden`: Caller is not Core staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): War slot not found for this donation, war slot deleted, or parent donation soft-deleted.

---

### 3.4 Mark Staff Attendance

Marks a Staff volunteer shift reservation as `attended` or `absent` (`checkin_source = 'coordinator'`), managing `staff_war` absence bans atomically.
- **Donation & Pre-Event Guards:** Same lifecycle guard (`open`/`closed`/`completed`) as Hero attendance, using `WAR_STAFF_ATTENDANCE_WINDOW_BEFORE` (default `1h` before `event_at`) for `attended` and `now >= event_at` for `absent`.
- **Ban Side Effects:**
  - `absent`: Inserts or re-activates a `staff_war` ban (`reason = no_show`, `donation_count = WAR_STAFF_BAN_EVENT_COUNT`, default `1`), preserving any prior manual pardon (`WHERE lifted_by IS NULL`).
  - `attended`: Auto-lifts any system ban linked to `source_staff_attendance_id` where `lifted_by IS NULL`.

* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/war-slots/:slot_id/attendances/:staff_attendance_id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Core staff (`role = core`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Parent donation ID. |
| `slot_id` | int64 | **Yes** | Parent war slot ID. |
| `staff_attendance_id` | int64 | **Yes** | Target `staff_attendances(id)`. |

#### Request Body
```json
{
  "status": "attended",
  "notes": "Confirmed briefing attendance"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `status` | string | **Yes** | Must be `"attended"` or `"absent"`. |
| `notes` | string | No | Optional coordinator remarks. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Staff attendance marked successfully",
  "data": {
    "id": 54,
    "war_slot_id": 55,
    "staff_id": "00000000-0000-0000-0000-000000000007",
    "status": "attended",
    "cancelled_late": false,
    "cancelled_at": null,
    "attended_at": "2026-11-14T14:26:15.102344+07:00",
    "marked_by": "00000000-0000-0000-0000-000000000003",
    "checkin_source": "coordinator",
    "notes": "Confirmed briefing attendance",
    "created_at": "2026-11-14T14:26:09.690908+07:00",
    "updated_at": "2026-11-14T14:26:15.102344+07:00"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_001`): Malformed JSON or invalid `status`.
* `400 Bad Request` (`VAL_002`): Invalid IDs, `draft`/`cancelled` donation, or `cancelled` shift reservation.
* `400 Bad Request` (`CANNOT_MARK_ABSENT_BEFORE_EVENT_START`): Attempted to mark `absent` before `event_at`.
* `400 Bad Request` (`ATTENDANCE_WINDOW_NOT_OPEN`): Attempted to mark `attended` prior to `event_at - WAR_STAFF_ATTENDANCE_WINDOW_BEFORE`.
* `403 Forbidden`: Caller is not Core staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): Donation, war slot (or cross-donation slot), or staff attendance record not found.

---

### 3.5 List User Bans

Retrieves paginated suspension history (`user_bans`) for a specific user, ordered by `created_at DESC, id DESC`. Verifies target user exists and is not soft-deleted (`404 NOT_FOUND`).

* **Method:** `GET`
* **Path:** `/api/v1/users/:id/bans`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Core staff (`role = core`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Target user UUID. |

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number. |
| `limit` | integer | No | `20` | Items per page (`1..100`). |
| `scope` | string | No | `""` | Filter by exact ban scope: `hero_war`, `staff_war`, or `all`. |
| `active_only` | boolean | No | `false` | When `true`, returns only unlifted bans (`lifted_at IS NULL`). Invalid booleans return `400 VAL_001`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "User bans retrieved successfully",
  "data": [
    {
      "id": 12,
      "user_id": "00000000-0000-0000-0000-000000000101",
      "scope": "hero_war",
      "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
      "donation_count": 2,
      "reason": "no_show",
      "source_hero_attendance_id": "2672bb18-9c16-4fcd-8fe3-cc70a781d2e3",
      "source_staff_attendance_id": null,
      "note": null,
      "created_by": null,
      "lifted_at": null,
      "lifted_by": null,
      "created_at": "2026-11-14T14:30:00Z"
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
* `400 Bad Request` (`VAL_001`): Invalid `?scope=` or invalid `?active_only=` boolean.
* `400 Bad Request` (`VAL_002`): Invalid user UUID format.
* `403 Forbidden`: Caller is not Core staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): User not found or soft-deleted.

---

### 3.6 Create Manual Ban

Issues an administrative suspension (`reason = manual`) for a user across `hero_war`, `staff_war`, or `all` scopes.
- **Self-Ban Prevention:** Callers cannot ban themselves (`400 VAL_001`).
- **Profile Parity:** `hero_war` requires a `heroes` profile; `staff_war` requires a `staff` profile; `all` requires both (`400 VAL_002`).
- **Duplicate Guard:** Rejects creation with `409 MANUAL_BAN_ALREADY_ACTIVE` if an unlifted manual ban already covers the requested scope (including `'all'` overlapping specific scopes or vice versa).

* **Method:** `POST`
* **Path:** `/api/v1/users/:id/bans`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Core staff (`role = core`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Target user UUID. |

#### Request Body
```json
{
  "scope": "hero_war",
  "donation_count": 3,
  "note": "Repeated disruption during distribution briefing"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `scope` | string | **Yes** | One of `hero_war`, `staff_war`, or `all`. |
| `donation_count` | int32 | **Yes** | Must be between `1` and `10` inclusive. |
| `note` | string | **Yes** | Non-empty justification note after trimming whitespace. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Manual ban created successfully",
  "data": {
    "id": 15,
    "user_id": "00000000-0000-0000-0000-000000000101",
    "scope": "hero_war",
    "donation_id": null,
    "donation_count": 3,
    "reason": "manual",
    "source_hero_attendance_id": null,
    "source_staff_attendance_id": null,
    "note": "Repeated disruption during distribution briefing",
    "created_by": "00000000-0000-0000-0000-000000000003",
    "lifted_at": null,
    "lifted_by": null,
    "created_at": "2026-11-14T14:32:10Z"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_001`): Self-ban attempt, malformed JSON, `donation_count` outside `1..10`, empty `note`, or invalid `scope`.
* `400 Bad Request` (`VAL_002`): Invalid user UUID, or user lacks required `heroes`/`staff` profile for the requested `scope`.
* `403 Forbidden`: Caller is not Core staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): Target user not found or soft-deleted.
* `409 Conflict` (`MANUAL_BAN_ALREADY_ACTIVE`): Active manual ban already covers the requested scope.

---

### 3.7 Lift (Pardon) User Ban

Manually pardons an active ban, setting `lifted_at = NOW()`, `lifted_by = caller_id`, and `note`.
- Accepts an empty body or `{"note": "..."}`. If body is empty or `note` is blank/omitted, defaults `note` to `"manual pardon by coordinator"`. Malformed JSON returns `400 VAL_001`.
- Setting `lifted_by` permanently prevents subsequent automated upsert or attendance-correction queries from overwriting the pardon.
- Repeat lift calls are rejected atomically (`WHERE lifted_at IS NULL`) with `409 BAN_ALREADY_LIFTED`.

* **Method:** `POST`
* **Path:** `/api/v1/bans/:id/lift`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Core staff (`role = core`) or Super Admin (`role = super`).

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | int64 | **Yes** | Target `user_bans(id)`. |

#### Request Body *(Optional)*
```json
{
  "note": "Verified medical emergency documentation"
}
```

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "User ban pardoned successfully",
  "data": {
    "id": 12,
    "user_id": "00000000-0000-0000-0000-000000000101",
    "scope": "hero_war",
    "donation_id": "ce57018a-6a2d-42c0-8788-f962ca6dd966",
    "donation_count": 2,
    "reason": "no_show",
    "source_hero_attendance_id": "2672bb18-9c16-4fcd-8fe3-cc70a781d2e3",
    "source_staff_attendance_id": null,
    "note": "Verified medical emergency documentation",
    "created_by": null,
    "lifted_at": "2026-11-14T14:35:00Z",
    "lifted_by": "00000000-0000-0000-0000-000000000003",
    "created_at": "2026-11-14T14:30:00Z"
  }
}
```

##### Error Responses
* `400 Bad Request` (`VAL_001`): Malformed JSON payload.
* `400 Bad Request` (`VAL_002`): Invalid ban ID format.
* `403 Forbidden`: Caller is not Core staff or Super Admin.
* `404 Not Found` (`NOT_FOUND`): User ban record not found.
* `409 Conflict` (`BAN_ALREADY_LIFTED`): Ban has already been lifted.

---

## 4. Ban Engine Mechanics & Domain Invariants

### 4.1 Dynamic Event Burn-Off (`user_bans`)
Active unlifted bans (`lifted_at IS NULL`) are evaluated dynamically at claim time rather than via scheduled background counters:
* **Baseline Timestamp:** `baseline_at = GREATEST(COALESCE(d.event_at, ub.created_at), ub.created_at)`.
* **Hero Ban Burn-Off (`hero_war`, `all`):** Counts `completed` non-deleted donations with `d.event_at > baseline_at`, `d.hero_quota > 0`, where the hero was eligible (untargeted or matching `hero_categories`) and did **not** hold an active reservation (`cancelled_at IS NULL`). The ban expires once `count >= ban.donation_count`.
* **Staff Ban Burn-Off (`staff_war`, `all`):** Counts `completed` non-deleted donations with `d.event_at > baseline_at`, having an active `war_slots` entry (`quota > 0, deleted_at IS NULL`) for the staff member's division, where the staff member did **not** hold an active shift claim.

### 4.2 Manual Pardon Preservation
- Every automated ban mutation (`UpsertHeroBanOnAbsent`, `UpsertHeroBanOnLateCancel`, `UpsertStaffBanOnAbsent`, `UpsertStaffBanOnLateCancel`, `AutoLiftHeroBan`, `AutoLiftStaffBan`) guards updates with `WHERE lifted_by IS NULL`.
- Once a coordinator lifts a ban via `POST /api/v1/bans/:id/lift`, `lifted_by` is populated and subsequent status flip-flops (e.g., `absent` -> `pardon` -> `attended` -> `absent`) will not overwrite or re-activate the pardoned record.

### 4.3 Retroactive Lift on Donation Cancellation
When Operational staff cancels a donation via `POST /api/v1/donations/:id/cancel` (see [`Donations.md`](./Donations.md)), all unlifted system-generated bans (`reason != 'manual' AND lifted_by IS NULL AND lifted_at IS NULL`) tied to that `donation_id` are automatically lifted with `note = 'auto-lifted: donation cancelled by staff'`.

### 4.4 Environment Configuration Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `WAR_HERO_BAN_EVENT_COUNT` | `2` | Completed eligible events required to clear an automated hero ban. |
| `WAR_STAFF_BAN_EVENT_COUNT` | `1` | Completed eligible events required to clear an automated staff ban. |
| `WAR_HERO_ATTENDANCE_WINDOW_BEFORE` | `1h` | Lead time before `event_at` when coordinators may begin marking heroes `attended`. |
| `WAR_STAFF_ATTENDANCE_WINDOW_BEFORE` | `1h` | Lead time before `event_at` when coordinators may begin marking staff `attended`. |

---

## 5. Error Code Reference

| Code | HTTP Status | Meaning |
| :--- | :--- | :--- |
| `VAL_001` | `400 Bad Request` | Payload/query validation failed (malformed JSON, invalid `?status=`/`?scope=`/`?active_only=`, self-ban, invalid `donation_count`, empty `note`). |
| `VAL_002` | `400 Bad Request` | Business guardrail or ID violation (invalid UUID/integer ID, `draft`/`cancelled` donation, marking `cancelled` reservation, profile mismatch on manual ban). |
| `UNAUTHORIZED` | `401 Unauthorized` | Missing, expired, or invalid JWT access token. |
| `FORBIDDEN` | `403 Forbidden` | Insufficient permissions (must be Operational/`super` for hero attendance; `core`/`super` for staff attendance and bans). |
| `NOT_FOUND` | `404 Not Found` | Donation, war slot, attendance record, user, or ban record not found (or parent donation/slot soft-deleted). |
| `CANNOT_MARK_ABSENT_BEFORE_EVENT_START` | `400 Bad Request` | Attempted to mark `absent` before the distribution's `event_at`. |
| `ATTENDANCE_WINDOW_NOT_OPEN` | `400 Bad Request` | Attempted to mark `attended` before `event_at - WAR_*_ATTENDANCE_WINDOW_BEFORE`. |
| `MANUAL_BAN_ALREADY_ACTIVE` | `409 Conflict` | Target user already has an active manual ban covering the requested scope. |
| `BAN_ALREADY_LIFTED` | `409 Conflict` | Target ban record has already been lifted/pardoned. |
| `INTERNAL_ERROR` | `500 Internal Server Error` | Unexpected server or database exception (logged server-side with `ref_id`). |

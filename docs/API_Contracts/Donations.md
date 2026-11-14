# API Contract: Donations & Food Distribution Management

> **Document Version:** `1.1.0`  
> **Topic ID:** `013 & 014 (Hardened)`  
> **Base Path:** `/api/v1`  
> **Security:** Bearer JWT (`Authorization: Bearer <token>`) & Optional Bearer token for public read endpoints  
> **Rate Limiting:** Token Bucket (60 req/min per IP)  
> **Specification Format:** REST / JSON  

---

## 1. Global Standards & Conventions

### 1.1 Headers

| Header | Direction | Type | Description |
| :--- | :--- | :--- | :--- |
| `Content-Type` | Request / Response | String | `application/json; charset=utf-8` |
| `Authorization` | Request | String | `Bearer <access_token>` (Required for mutating endpoints and staff reads; optional for public reads) |
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
  "message": "Donations retrieved successfully",
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

---

## 2. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/donations` | Optional Bearer | Public / Hero / Staff | List donations. Public callers receive active (`open`, `closed`) and recent past (`completed` within 72h window) distributions; staff callers view all statuses with search and status filters. |
| `GET` | `/api/v1/donations/:id` | Optional Bearer | Public / Hero / Staff | Retrieve a single donation by UUID. Drafts, hidden distributions, and past distributions older than 72h are restricted to staff. |
| `POST` | `/api/v1/donations` | Bearer JWT | Operational (`division_id = 3`) or `super` | Create a new donation in `draft` status with associated foods and optional target categories. |
| `PATCH` | `/api/v1/donations/:id` | Bearer JWT | Operational (`division_id = 3`) or `super` | Safe partial update of a donation. Completed and cancelled donations are immutable (`400 VAL_001`). Open donations reject past `event_at` (`400 VAL_001`). |
| `DELETE` | `/api/v1/donations/:id` | Bearer JWT | Operational (`division_id = 3`) or `super` | Soft delete a donation (`deleted_at = NOW()`). Allowed only while in `draft` status (`400 VAL_001`). |
| `POST` | `/api/v1/donations/:id/publish` | Bearer JWT | Operational (`division_id = 3`) or `super` | Publish a `draft` donation. Transitions to `open` (or `closed` if `hero_quota > 0 && remain == 0`). Rejects if sponsor was soft-deleted (`400 VAL_002`). |
| `POST` | `/api/v1/donations/:id/close` | Bearer JWT | Operational (`division_id = 3`) or `super` | Manually close an `open` donation before its scheduled event time. |
| `POST` | `/api/v1/donations/:id/reopen` | Bearer JWT | Operational (`division_id = 3`) or `super` | Reopen a `closed` donation if `remain > 0` and event datetime is in the future. |
| `POST` | `/api/v1/donations/:id/complete` | Bearer JWT | Operational (`division_id = 3`) or `super` | Mark an `open` or `closed` distribution as `completed` (terminal state). |
| `POST` | `/api/v1/donations/:id/cancel` | Bearer JWT | Operational (`division_id = 3`) or `super` | Cancel a `draft`, `open`, or `closed` distribution (terminal state). |

---

## 3. Endpoint Specifications

### 3.1 List Donations

Retrieves a paginated list of food donations.
- **Public & Hero Callers:** Query active (`?status=open`, returning `open` and `closed`) or recent past (`?status=completed` within 72h window).
  - Sponsor contact information (`phone`, `email`, `address`, `hidden`, `hide_donations`) is strictly excluded.
  - If sponsor is hidden, `hide_donations=true`, `hide_sponsor=true`, or soft-deleted, sponsor renders as `{"id": null, "name": "Generous sponsor", "logo": null}`.
  - Targets with `hidden = true` are collapsed into a single `{"beneficiary_category_id": null, "name": "Hidden institution", "variant": null}` entry.
  - `is_full` is strictly `true` only when `hero_quota > 0 && remain == 0`.
- **Staff Callers:** Can query any status (`draft`, `open`, `closed`, `completed`, `cancelled`) or leave `status` empty for all records. Supports case-insensitive substring search matching donation address or sponsor name (`ESCAPE '\'`).

* **Method:** `GET`
* **Path:** `/api/v1/donations`
* **Authentication:** Optional (`Bearer <JWT>`)

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (must be > 0). |
| `limit` | integer | No | `20` | Records per page (maximum `100`, bounded dynamically by `PAGINATION_MAX_LIMIT`). |
| `status` | string | No | `open` (public) / `all` (staff) | Filter by status. Public callers accept `open` or `completed`. Staff accept any status. |
| `search` | string | No | `null` | Substring match against address and sponsor name (staff only). Wildcard characters `\`, `%`, and `_` are escaped automatically. |

#### Responses

##### `200 OK` (Public Caller View)
```json
{
  "status": "success",
  "message": "Donations retrieved successfully",
  "data": [
    {
      "id": "11111111-2222-3333-4444-555555555555",
      "event_at": "2026-11-20T10:00:00Z",
      "quota": 50,
      "hero_quota": 25,
      "remain": 10,
      "is_full": false,
      "address": "Jl. Kaliurang KM 5, Sleman",
      "maps": "https://maps.google.com/?q=-7.76,110.38",
      "message": "Pengambilan makanan di dekat gerbang barat.",
      "status": "open",
      "hide_sponsor": false,
      "created_at": "2026-11-12T10:00:00Z",
      "updated_at": "2026-11-12T10:00:00Z",
      "sponsor": {
        "id": "87acbb21-a5b2-4d99-8511-48cb43253df7",
        "name": "PT Berbagi Bites Jogja",
        "logo": "88888888-0000-0000-0000-000000000001"
      },
      "foods": [
        {
          "food_item_id": 1,
          "name": "Nasi Kotak Ayam Bakar",
          "quantity": 50.0,
          "unit": "portion"
        }
      ],
      "targets": [
        {
          "beneficiary_category_id": "99999999-0000-0000-0000-000000000001",
          "name": "Mahasiswa UGM",
          "variant": "university"
        },
        {
          "beneficiary_category_id": null,
          "name": "Hidden institution",
          "variant": null
        }
      ]
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

##### `200 OK` (Staff Caller View)
Staff callers receive additional fields: `hide_from_public`, `created_by`, and full sponsor contact information including soft-deletion state (`is_deleted`).
```json
{
  "status": "success",
  "message": "Donations retrieved successfully",
  "data": [
    {
      "id": "11111111-2222-3333-4444-555555555555",
      "event_at": "2026-11-20T10:00:00Z",
      "quota": 50,
      "hero_quota": 25,
      "remain": 10,
      "address": "Jl. Kaliurang KM 5, Sleman",
      "maps": "https://maps.google.com/?q=-7.76,110.38",
      "message": "Pengambilan makanan di dekat gerbang barat.",
      "status": "open",
      "hide_from_public": false,
      "hide_sponsor": false,
      "created_at": "2026-11-12T10:00:00Z",
      "updated_at": "2026-11-12T10:00:00Z",
      "sponsor": {
        "id": "87acbb21-a5b2-4d99-8511-48cb43253df7",
        "name": "PT Berbagi Bites Jogja",
        "logo": "88888888-0000-0000-0000-000000000001",
        "hidden": false,
        "hide_donations": false,
        "is_deleted": false,
        "email": "csr@bbj.com",
        "phone": "6281234567890",
        "address": "Sleman, Yogyakarta"
      },
      "created_by": {
        "id": "00000000-0000-0000-0000-000000000007",
        "email": "op.sf@berbagibitesjogja.com"
      },
      "foods": [
        {
          "food_item_id": 1,
          "name": "Nasi Kotak Ayam Bakar",
          "quantity": 50.0,
          "unit": "portion"
        }
      ],
      "targets": [
        {
          "beneficiary_category_id": "99999999-0000-0000-0000-000000000001",
          "name": "Mahasiswa UGM",
          "variant": "university"
        }
      ]
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

---

### 3.2 Get Donation by ID

Retrieves full details for a single donation.
- **Public & Hero Callers:** Receive `404 Not Found` if status is `draft`, `cancelled`, or `hide_from_public = true`, or if `status = completed` and the event is older than 72 hours.
- **Staff Callers:** Can inspect any active donation in any status.

* **Method:** `GET`
* **Path:** `/api/v1/donations/:id`
* **Authentication:** Optional (`Bearer <JWT>`)

#### Responses

* `200 OK`: Returns public or staff response representation according to caller role.
* `400 Bad Request` (`VAL_001`): Invalid UUID format.
* `404 Not Found` (`NOT_FOUND`): Donation not found, deleted, or restricted by visibility/status rules.

---

### 3.3 Create Donation

Creates a new food distribution event in `draft` status.

* **Method:** `POST`
* **Path:** `/api/v1/donations`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Operational staff (`division_id = 3`) or Super Admin (`role = super`).

#### Request Body
```json
{
  "sponsor_id": "87acbb21-a5b2-4d99-8511-48cb43253df7",
  "event_at": "2026-11-20T10:00:00Z",
  "quota": 50,
  "hero_quota": 25,
  "address": "Di UGM, Sleman",
  "maps": "https://maps.google.com/?q=-7.76,110.38",
  "message": "Masuk gerbang utama belok kanan",
  "hide_from_public": false,
  "hide_sponsor": false,
  "foods": [
    {
      "food_item_id": 1,
      "quantity": 50.0,
      "unit": "portion"
    }
  ],
  "beneficiary_category_ids": [
    "99999999-0000-0000-0000-000000000001"
  ]
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `sponsor_id` | string (UUID) | **Yes** | Must reference an active, non-deleted sponsor in `sponsors(id)`. |
| `event_at` | string (RFC3339) | **Yes** | Scheduled distribution datetime in valid RFC3339 format. |
| `quota` | integer | **Yes** | Total meal portions available. Must be `> 0`. |
| `hero_quota` | integer | No | Quota allocated for individual Hero claims. Must be `>= 0` and `<= quota`. Defaults to `0`. If all target categories are `organization`, must strictly be `0`. |
| `address` | string | No | Physical venue address. Max 500 characters. |
| `maps` | string | No | Google Maps / GPS link. |
| `message` | string | No | Freeform staff notes or instructions for beneficiaries. |
| `hide_from_public` | boolean | No | When `true`, hides the donation from public/hero views. Defaults to `true`. |
| `hide_sponsor` | boolean | No | When `true`, masks sponsor to `"Generous sponsor"` for this specific event. Defaults to `false`. |
| `foods` | array of objects | **Yes** | At least 1 food item required. Cannot contain duplicate `food_item_id`s. |
| `foods[].food_item_id` | integer | **Yes** | Must reference an existing record in `food_items(id)`. |
| `foods[].quantity` | float | **Yes** | Amount of food. Must be `> 0`. |
| `foods[].unit` | string | **Yes** | One of: `kg`, `g`, `l`, `ml`, `portion`, `pack`, `piece`. |
| `beneficiary_category_ids` | array of UUIDs | **No** | Target categories. An empty list `[]` or omitted field represents a general food drive open to all registered Heroes. |

#### Responses

* `201 Created`: Donation created successfully in `draft` status.
* `400 Bad Request` (`VAL_001`): Field validation failed (e.g. `quota <= 0`, `hero_quota > quota`).
* `400 Bad Request` (`VAL_002`): Referenced entity does not exist or sponsor is soft-deleted.
* `403 Forbidden` (`AUTH_002`): Caller is not an operational staff member or super admin.

---

### 3.4 Update Donation

Performs a safe partial update (`PATCH`) on a donation record. Fields omitted from the payload retain their existing values.
- **State Guardrail:** Terminal states (`completed`, `cancelled`) are strictly immutable (`400 VAL_001`).
- **Past Date Guardrail:** When an `open` donation is updated, setting `event_at` to the past is rejected (`400 VAL_001`).
- **Quota & Remain Integrity:**
  - Reducing `hero_quota` below currently active hero reservations (`active_claims`) is rejected with `409 Conflict` (`QUOTA_BELOW_ACTIVE_CLAIMS`).
  - When `hero_quota` is updated, remaining spots are recalculated as `remain = hero_quota - active_claims`.
  - `hero_quota` cannot exceed total `quota` (`400 VAL_001`).
- **No Status Flapping:** Modifying quotas never changes donation status (`open` remains `open` even when `remain = 0`, and `closed` remains `closed` until explicitly reopened via `/reopen`).
- **Target Category Clearing:** Passing `beneficiary_category_ids: []` clears all targets.

* **Method:** `PATCH`
* **Path:** `/api/v1/donations/:id`
* **Authentication:** Required (`Bearer <JWT>`)

---

### 3.5 Delete Donation

Performs a soft delete (`deleted_at = NOW()`) on a donation. Allowed only when `status = draft` (`400 VAL_001`).

* **Method:** `DELETE`
* **Path:** `/api/v1/donations/:id`
* **Authentication:** Required (`Bearer <JWT>`)

---

### 3.6 Donation Lifecycle Action Endpoints

#### 3.6.1 Publish Donation
Transitions a `draft` donation into `open`.
* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/publish`
* **Prerequisites:**
  1. Current status must be `draft`.
  2. Scheduled `event_at` must be strictly in the future (`> NOW()`).
  3. At least one food item must be attached.
  4. If all target categories are `organization`, `hero_quota` must be `0`.
  5. Referenced sponsor must not be soft-deleted (`400 VAL_002` *"Sponsor has been deleted"*).

#### 3.6.2 Close Donation
Manually closes an `open` donation before its scheduled event time.
* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/close`
* **Prerequisites:** Current status must be `open`.

#### 3.6.3 Reopen Donation
Reopens a previously `closed` donation.
* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/reopen`
* **Prerequisites:** Status `closed`, `remain > 0`, and `event_at > NOW()`.

#### 3.6.4 Complete Donation
Marks a distribution as successfully completed.
* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/complete`
* **Prerequisites:** Status must be `open` or `closed`.

#### 3.6.5 Cancel Donation
Aborts a scheduled food distribution.
* **Method:** `POST`
* **Path:** `/api/v1/donations/:id/cancel`
* **Prerequisites:** Current status must be `draft`, `open`, or `closed`.

---

## 4. Domain Invariants & Background Jobs

### 4.1 Quota Invariants
1. **Total Quota:** `quota > 0` at all times.
2. **Hero Allocation:** `0 <= hero_quota <= quota`.
3. **Remaining Calculation:** On creation, initial `remain = hero_quota`. On updates, `final_remain = current.remain + (new_hero_quota - current.hero_quota)`.
4. **Non-Negative Remain:** Updates causing `remain < 0` are rejected immediately.
5. **Quota Penuh Badge (`is_full`):** Displayed as `true` in public views strictly when `hero_quota > 0 && remain == 0`. Organization-only events (`hero_quota = 0, remain = 0`) and manually closed events with spots left report `is_full: false`.

### 4.2 Quadruple Privacy Masking
A sponsor is masked to `"Generous sponsor"` (with `id = null` and `logo = null`) on public and hero endpoints if **any** of the following conditions are met:
1. `donations.hide_sponsor = true` (per-event masking selected by staff).
2. `sponsors.hidden = true` (sponsor profile is unlisted/hidden).
3. `sponsors.hide_donations = true` (sponsor profile is public, but distributions default to anonymous).
4. `sponsors.deleted_at IS NOT NULL` (sponsor was soft-deleted).

Under no circumstances are a sponsor's `phone`, `email`, or `address` returned to public or hero callers.

### 4.3 Background Auto-Complete Job
- **Trigger:** Active donations (`open` or `closed`) with `event_at <= NOW() - DONATION_AUTO_COMPLETE_AFTER` (default grace window: `2h`) automatically transition to `completed`.
- **Worker Cadence:** Runs periodically via background ticker every `DONATION_AUTO_COMPLETE_INTERVAL` (default: `1h`). Also runs once at server startup.
- **Horizontal Replica Safety:** Transactional advisory lock `pg_try_advisory_xact_lock(7483920192)` ensures exactly one instance executes the sweep per tick.

---

## 5. Error Code Reference

| Code | HTTP Status | Meaning |
| :--- | :--- | :--- |
| `VAL_001` | `400 Bad Request` | Validation failure (malformed payload, invalid format, out-of-range value, or state transition violation). |
| `VAL_002` | `400 Bad Request` | Foreign key or entity reference conflict (non-existent or soft-deleted sponsor, food item, or category). |
| `AUTH_001` | `401 Unauthorized` | Missing, expired, or malformed JWT access token. |
| `AUTH_002` | `403 Forbidden` | Insufficient permissions (e.g., non-operational staff mutating donations, or public accessing draft). |
| `NOT_FOUND` | `404 Not Found` | Donation ID does not exist, has been soft deleted, or is hidden from public callers. |
| `ERR_INTERNAL` | `500 Internal Server Error` | Unexpected server or database exception. Details safe-logged with `ref_id`. |

# API Contract: Sponsors Management

> **Document Version:** `1.1.0`  
> **Topic ID:** `004 & 014 (Hardened)`  
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

#### Standard Success (`2xx`)
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
  "message": "Human-readable description of the completed action",
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
| `GET` | `/api/v1/sponsors` | Optional Bearer | Public / Hero / Staff | List sponsors (paginated, filtered). Public callers receive whitelisted `{ id, name, variant, logo }` for non-hidden sponsors. Staff callers receive full contact profiles. |
| `GET` | `/api/v1/sponsors/:id` | Optional Bearer | Public / Hero / Staff | Retrieve a single active sponsor by UUID. Public callers receive whitelisted `{ id, name, variant, logo }`. Hidden sponsors require authenticated staff. |
| `POST` | `/api/v1/sponsors` | Bearer JWT | Operational (`super`, `core`, `staff`) | Create a new sponsor. Triggers phone normalization, strict email validation, logo upload verification, and active name uniqueness. |
| `PATCH` | `/api/v1/sponsors/:id` | Bearer JWT | Operational (`super`, `core`, `staff`) | Concurrency-safe partial update with `FOR UPDATE` row lock. |
| `DELETE` | `/api/v1/sponsors/:id` | Bearer JWT | Operational (`super`, `core`, `staff`) | Soft-delete sponsor (`deleted_at = NOW()`). Releases active unique name constraint. |

---

## 3. Endpoint Specifications

### 3.1 List Sponsors

Retrieves a paginated list of sponsors with optional query filtering.
- **Public & Hero Callers:** Non-hidden sponsors only (`hidden = FALSE`). Returned fields are strictly whitelisted to `id`, `name`, `variant`, and `logo`. PII (`email`, `phone`, `address`) and internal flags (`hidden`, `hide_donations`, timestamps) are strictly stripped.
- **Staff Callers:** Authenticated staff members can optionally filter `?hidden=true` or query all records, receiving full profile details.

* **Method:** `GET`
* **Path:** `/api/v1/sponsors`
* **Authentication:** Optional (`Bearer <JWT>`)

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (must be > 0). |
| `limit` | integer | No | `20` | Records per page (maximum `100`, bounded dynamically by `PAGINATION_MAX_LIMIT`). |
| `variant` | string | No | `null` | Filter by variant: `company` or `individual`. |
| `search` | string | No | `null` | Case-insensitive substring search matching sponsor `name`. Wildcards `\`, `%`, and `_` are escaped with `ESCAPE ''`. |
| `hidden` | boolean | No | `false` (public) | Filter by visibility. Ignored for public callers (always forced to `false`). |

#### Responses

##### `200 OK` (Public & Hero Caller View)
```json
{
  "status": "success",
  "message": "Sponsors retrieved successfully",
  "data": [
    {
      "id": "767e9a5d-e29c-4056-a9bc-acf8033592a8",
      "name": "PT Berbagi Bites Jogja",
      "variant": "company",
      "logo": "b9e87ca6-adb2-e0d2-c24b-93752b7555b5"
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
```json
{
  "status": "success",
  "message": "Sponsors retrieved successfully",
  "data": [
    {
      "id": "767e9a5d-e29c-4056-a9bc-acf8033592a8",
      "name": "PT Berbagi Bites Jogja",
      "variant": "company",
      "address": "Di Sleman",
      "email": "it@berbagibitesjogja.com",
      "phone": "6200000000001",
      "logo": "b9e87ca6-adb2-e0d2-c24b-93752b7555b5",
      "hidden": false,
      "hide_donations": false,
      "created_at": "2026-11-06T23:21:40+07:00",
      "updated_at": "2026-11-06T23:21:40+07:00"
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

### 3.2 Get Sponsor by ID

Retrieves an active (non-deleted) sponsor by UUID.
- **Public & Hero Callers:** Receive whitelisted `{ id, name, variant, logo }`. If the sponsor is hidden, returns `404 Not Found`.
- **Staff Callers:** Receive the full sponsor entity regardless of `hidden` status.

* **Method:** `GET`
* **Path:** `/api/v1/sponsors/:id`
* **Authentication:** Optional (`Bearer <JWT>`)

#### Responses

##### `200 OK` (Public View)
```json
{
  "status": "success",
  "message": "Sponsor retrieved successfully",
  "data": {
    "id": "767e9a5d-e29c-4056-a9bc-acf8033592a8",
    "name": "PT Berbagi Bites Jogja",
    "variant": "company",
    "logo": null
  }
}
```

##### `200 OK` (Staff View)
```json
{
  "status": "success",
  "message": "Sponsor retrieved successfully",
  "data": {
    "id": "767e9a5d-e29c-4056-a9bc-acf8033592a8",
    "name": "PT Berbagi Bites Jogja",
    "variant": "company",
    "address": "Di Sleman",
    "email": "it@berbagibitesjogja.com",
    "phone": "6200000000001",
    "logo": null,
    "hidden": false,
    "hide_donations": false,
    "created_at": "2026-11-06T23:21:40+07:00",
    "updated_at": "2026-11-06T23:21:40+07:00"
  }
}
```

---

### 3.3 Create Sponsor

Creates a new sponsor entity in the database.
- Phone numbers are sanitized to `62...` by database trigger `trg_normalize_sponsor_phone`.
- Active sponsor names must be unique (`uk_sponsors_active_name`).
- Strict email validation: Parsed via standard library `net/mail.ParseAddress`. Display names (e.g. `"Jane Doe <jane@example.com>"`) are rejected (`400 VAL_001`).
- Logo verification: If `logo` is provided, verifies that the file exists in `files` table and has `status = 'uploaded'`. Non-existent file ID returns `400 VAL_002` (*"Referenced logo file does not exist"*); file in unuploaded status returns `400 VAL_001` (*"File not found or not uploaded"*).

* **Method:** `POST`
* **Path:** `/api/v1/sponsors`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Request Body
```json
{
  "name": "PT Berbagi Bites Jogja",
  "variant": "company",
  "address": "Di Sleman",
  "email": "it@berbagibitesjogja.com",
  "phone": "+62 000-000 000 001",
  "logo": null,
  "hidden": false,
  "hide_donations": false
}
```

---

### 3.4 Update Sponsor (Safe PATCH)

Applies partial updates to an existing sponsor.
- **Concurrency Guardrail:** Executes within a transaction using `GetSponsorByIDForUpdate` (`SELECT ... FOR UPDATE`) before modifying attributes.
- **Strict Email Validation:** If `email` is updated, enforces `net/mail.ParseAddress` without display name formatting.
- **Logo Upload Verification:** If `logo` is updated to a non-empty UUID, verifies existence and `status = 'uploaded'`. Passing `""` clears the logo.

* **Method:** `PATCH`
* **Path:** `/api/v1/sponsors/:id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

---

### 3.5 Delete Sponsor (Soft Delete)

Soft-deletes a sponsor by marking `deleted_at = NOW()`. Releases active unique name constraint.

* **Method:** `DELETE`
* **Path:** `/api/v1/sponsors/:id`
* **Authentication:** Required (`Bearer <JWT>`)

---

## 4. Domain Models & Constraints

### 4.1 Schema Definition

```sql
CREATE TYPE sponsor_variant AS ENUM ('company', 'individual');

CREATE TABLE sponsors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(150) NOT NULL,
    variant sponsor_variant DEFAULT 'individual',
    address VARCHAR(500),
    email VARCHAR(255),
    phone VARCHAR(20),
    logo UUID REFERENCES files(id) ON DELETE SET NULL,
    hidden BOOL NOT NULL DEFAULT FALSE,
    hide_donations BOOL NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE UNIQUE INDEX uk_sponsors_active_name ON sponsors (name) WHERE deleted_at IS NULL;
```

### 4.2 Invariants & Business Logic

1. **Active Name Uniqueness:**
   Enforced via partial unique index `uk_sponsors_active_name` on `LOWER(name)` (`name VARCHAR(150)`).
2. **Phone Number Standardization:**
   Database trigger `trg_normalize_sponsor_phone` normalizes phone numbers to `62...` format.
3. **Public Privacy Protection:**
   Public directory queries strictly restrict output keys to `id`, `name`, `variant`, `logo`. Under no circumstances are PII or internal flags leaked to non-staff callers.
4. **Logo Media Attachment:**
   `logo` points to `files(id)`. Validated to exist with `status = 'uploaded'` upon creation and update.

# API Contract: Sponsors Management

> **Document Version:** `1.0.0`  
> **Topic ID:** `004`  
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
| `Authorization` | Request | String | `Bearer <access_token>` (Required for mutating endpoints; optional for read endpoints) |
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
    "limit": 10,
    "total": 42,
    "total_pages": 5
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
*Note: `errors` map is only included when field-level validation or constraint conflicts occur.*

---

## 2. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/sponsors` | Optional Bearer | Public / Staff | List sponsors (paginated, filtered). Public callers receive non-hidden sponsors only. |
| `GET` | `/api/v1/sponsors/:id` | Optional Bearer | Public / Staff | Retrieve a single active sponsor by UUID. Hidden sponsors require authenticated staff. |
| `POST` | `/api/v1/sponsors` | Bearer JWT | `super`, `core`, `staff` | Create a new sponsor. Triggers phone normalization and enforces active name uniqueness. |
| `PATCH` | `/api/v1/sponsors/:id` | Bearer JWT | `super`, `core`, `staff` | Partial safe update of sponsor mutable fields without resetting unmentioned attributes. |
| `DELETE` | `/api/v1/sponsors/:id` | Bearer JWT | `super`, `core`, `staff` | Soft-delete sponsor (`deleted_at = NOW()`). Releases active unique name constraint. |

---

## 3. Endpoint Specifications

### 3.1 List Sponsors

Retrieves a paginated list of sponsors with optional query filtering. Unauthenticated public callers are restricted to non-hidden sponsors (`hidden = FALSE`). Authenticated staff members can optionally filter `?hidden=true` or query all records.

* **Method:** `GET`
* **Path:** `/api/v1/sponsors`
* **Authentication:** Optional (`Bearer <JWT>`)
* **Role Permissions:** Public access permitted. Authenticated staff/admins have access to hidden records.

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (must be > 0). |
| `limit` | integer | No | `10` | Records per page (maximum `100`). |
| `variant` | string | No | `null` | Filter by variant: `company` or `individual`. |
| `search` | string | No | `null` | Case-insensitive substring search matching sponsor `name` (ILIKE). |
| `hidden` | boolean | No | `false` (public) | Filter by visibility. Ignored for public callers (always forced to `false`). |

#### Responses

##### `200 OK` (Public / Paginated)
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
      "created_at": "2026-11-06T23:21:40+07:00",
      "updated_at": "2026-11-06T23:21:40+07:00"
    }
  ],
  "meta": {
    "page": 1,
    "limit": 10,
    "total": 1,
    "total_pages": 1
  }
}
```

##### `400 Bad Request` (Invalid Variant Parameter)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid variant filter. Must be 'company' or 'individual'",
  "ref_id": "ref_9a8b7c6d"
}
```

---

### 3.2 Get Sponsor by ID

Retrieves an active (non-deleted) sponsor by UUID. If the sponsor is hidden, public unauthenticated requests will receive a `404 Not Found` response.

* **Method:** `GET`
* **Path:** `/api/v1/sponsors/:id`
* **Authentication:** Optional (`Bearer <JWT>`)
* **Role Permissions:** Public access for non-hidden sponsors. Staff access for all active sponsors.

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | string (UUID) | **Yes** | Standard RFC 4122 UUID of the sponsor. |

#### Responses

##### `200 OK`
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
    "created_at": "2026-11-06T23:21:40+07:00",
    "updated_at": "2026-11-06T23:21:40+07:00"
  }
}
```

##### `404 Not Found` (Non-existent, Soft-Deleted, or Public Access to Hidden Record)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Sponsor not found",
  "ref_id": "ref_3915ad58"
}
```

##### `400 Bad Request` (Malformed UUID)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid sponsor ID format",
  "ref_id": "ref_81726a54"
}
```

---

### 3.3 Create Sponsor

Creates a new sponsor entity in the database. The database trigger `trg_normalize_sponsor_phone` automatically sanitizes phone numbers into international subscriber format (`62...`). Active sponsor names must be unique.

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
  "hidden": false
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `name` | string | **Yes** | Max 50 characters. Must be unique among active sponsors (`deleted_at IS NULL`). |
| `variant` | string | No | One of: `company`, `individual`. Defaults to `individual`. |
| `address` | string | No | Max 500 characters. |
| `email` | string | No | Max 255 characters. Must contain valid `@` format if provided. |
| `phone` | string | No | Max 20 characters. Automatically sanitized to `62...` format by database trigger. |
| `logo` | string (UUID) | No | Must be a valid UUID referencing an existing record in `files(id)`. Set to `null` if omitted. |
| `hidden` | boolean | No | Defaults to `false`. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Sponsor created successfully",
  "data": {
    "id": "dbeed153-3ac5-46d2-90c3-83e9d5ac8318",
    "name": "PT Berbagi Bites Jogja",
    "variant": "company",
    "address": "Di Sleman",
    "email": "it@berbagibitesjogja.com",
    "phone": "6200000000001",
    "logo": null,
    "hidden": false,
    "created_at": "2026-11-06T23:27:53+07:00",
    "updated_at": "2026-11-06T23:27:53+07:00"
  }
}
```

##### `400 Bad Request` (Duplicate Active Name - SQLSTATE 23505)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Validation failed",
  "ref_id": "ref_f5181fb0",
  "errors": {
    "name": "Sponsor name is already registered"
  }
}
```

##### `400 Bad Request` (Invalid Logo File Reference - SQLSTATE 23503)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Validation failed",
  "ref_id": "ref_e4171ea1",
  "errors": {
    "logo": "Referenced logo file does not exist"
  }
}
```

##### `400 Bad Request` (Validation Failure)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Validation failed",
  "ref_id": "ref_c3161da2",
  "errors": {
    "name": "Sponsor name is required",
    "variant": "Variant must be 'company' or 'individual'",
    "email": "Invalid email format"
  }
}
```

---

### 3.4 Update Sponsor (Safe PATCH)

Applies partial updates to an existing sponsor. Follows the **Safe PATCH pattern** (`AGENTS.md §4.5`) &mdash; existing values are pre-fetched before updating to ensure unmentioned fields (such as enum types) are never corrupted by zero-values.

* **Method:** `PATCH`
* **Path:** `/api/v1/sponsors/:id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Request Body
*Provide only the fields you wish to update:*
```json
{
  "address": "Di Sini Senang",
  "hidden": true
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `name` | string | No | Max 50 characters. Trapped for duplicate active name collision. |
| `variant` | string | No | One of: `company`, `individual`. |
| `address` | string | No | Max 500 characters. Pass empty string `""` to clear. |
| `email` | string | No | Max 255 characters. Pass empty string `""` to clear. |
| `phone` | string | No | Max 20 characters. Automatically normalized if updated. Pass empty string `""` to clear. |
| `logo` | string (UUID) | No | Must be valid UUID referencing `files(id)`. Pass empty string `""` to clear. |
| `hidden` | boolean | No | Toggle visibility. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Sponsor updated successfully",
  "data": {
    "id": "dbeed153-3ac5-46d2-90c3-83e9d5ac8318",
    "name": "PT Berbagi Bites Jogja",
    "variant": "company",
    "address": "Di Sini Senang",
    "email": "it@berbagibitesjogja.com",
    "phone": "6200000000001",
    "logo": null,
    "hidden": true,
    "created_at": "2026-11-06T23:27:53+07:00",
    "updated_at": "2026-11-06T23:31:12+07:00"
  }
}
```

##### `404 Not Found` (Sponsor Does Not Exist or Is Soft-Deleted)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Sponsor not found",
  "ref_id": "ref_728190ab"
}
```

---

### 3.5 Delete Sponsor (Soft Delete)

Soft-deletes a sponsor by marking `deleted_at = NOW()`. Soft-deleted records are immediately excluded from subsequent queries and release their unique active name constraint, allowing the name to be re-used if needed.

* **Method:** `DELETE`
* **Path:** `/api/v1/sponsors/:id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Sponsor deleted successfully",
  "data": {
    "id": "dbeed153-3ac5-46d2-90c3-83e9d5ac8318"
  }
}
```

##### `404 Not Found` (Already Deleted or Does Not Exist)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Sponsor not found",
  "ref_id": "ref_518290bc"
}
```

---

## 4. Domain Models & Constraints

### 4.1 Schema Definition

```sql
CREATE TYPE sponsor_variant AS ENUM ('company', 'individual');

CREATE TABLE sponsors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) NOT NULL,
    variant sponsor_variant DEFAULT 'individual',
    address VARCHAR(500),
    email VARCHAR(255),
    phone VARCHAR(20),
    logo UUID REFERENCES files(id) ON DELETE SET NULL,
    hidden BOOL NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ DEFAULT NULL
);

CREATE UNIQUE INDEX uk_sponsors_active_name ON sponsors (name) WHERE deleted_at IS NULL;
```

### 4.2 Invariants & Business Logic

1. **Active Name Uniqueness:**
   Enforced via partial unique index `uk_sponsors_active_name`. Soft-deleted records (`deleted_at IS NOT NULL`) do not prevent re-registration of the same sponsor name.
2. **Phone Number Standardization:**
   Database trigger `trg_normalize_sponsor_phone` intercepts every INSERT and UPDATE of `phone`, invoking `normalize_phone_number()`. Input formats such as `0812-3456-7890` or `+62 812 3456 7890` are stored as `6281234567890`.
3. **Logo Media Attachment:**
   `logo` points to `files(id)` in PostgreSQL. The foreign key constraint includes `ON DELETE SET NULL`, ensuring that if a logo file is pruned from object storage (RustFS), the sponsor entity remains valid with `logo = NULL`.

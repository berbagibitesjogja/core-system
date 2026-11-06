# API Contract: Beneficiary Categories Management

> **Document Version:** `1.0.0`  
> **Topic ID:** `006`  
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
  "message": "Beneficiary categories retrieved successfully",
  "data": [],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 23,
    "total_pages": 2
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
| `GET` | `/api/v1/beneficiary-categories` | Optional Bearer | Public / Staff | List beneficiary categories (paginated, filtered). Public callers receive non-hidden records only. |
| `GET` | `/api/v1/beneficiary-categories/:id` | Optional Bearer | Public / Staff | Retrieve single category by UUID. Hidden records require authenticated staff. |
| `POST` | `/api/v1/beneficiary-categories` | Bearer JWT | `super`, `core`, `staff` | Create a new beneficiary category. Enforces case-insensitive active uniqueness. |
| `PATCH` | `/api/v1/beneficiary-categories/:id` | Bearer JWT | `super`, `core`, `staff` | Partial safe update of mutable category fields (`name`, `variant`, `hidden`). |
| `DELETE` | `/api/v1/beneficiary-categories/:id` | Bearer JWT | `super`, `core`, `staff` | Soft-delete category (`deleted_at = NOW()`). Releases active unique name constraint. |

---

## 3. Taxonomy Variants & Indexing Design

* **Allowed Taxonomy Variants:**
  `student`, `orphanage`, `nursing_home`, `shelter`, `religious_community`, `foundation`, `society`, `other`.
* **Primary Key:** UUID (`id UUID DEFAULT gen_random_uuid()`).
* **GIN Trigram Index:** `idx_beneficiary_categories_name_trgm` on `name gin_trgm_ops` provides high-performance substring search (`ILIKE '%<query>%'`) without sequential table scans.
* **Partial Active Uniqueness:** `uk_beneficiary_categories_active_lower_name` on `LOWER(name) WHERE deleted_at IS NULL` prevents duplicates among active records while allowing re-use after deletion.
* **Variant B-Tree Index:** `idx_beneficiary_categories_variant` on `variant WHERE deleted_at IS NULL` optimizes category breakdown filtering.

---

## 4. Endpoint Specifications

### 4.1 List Beneficiary Categories

Retrieves a paginated list of beneficiary categories sorted alphabetically by name. Unauthenticated callers are restricted to non-hidden categories (`hidden = false`). Authenticated staff can query all categories or filter specifically by `?hidden=true`.

* **Method:** `GET`
* **Path:** `/api/v1/beneficiary-categories`
* **Authentication:** Optional (`Bearer <JWT>`)
* **Role Permissions:** Public access for public categories. Authenticated staff for full access.

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (must be > 0). |
| `limit` | integer | No | `20` | Records per page (maximum `100`). |
| `variant` | string | No | `null` | Filter by taxonomy variant. |
| `search` | string | No | `null` | Case-insensitive substring search matching category `name`. |
| `hidden` | boolean | No | `false` (public) | Filter by visibility. Ignored for public callers (always forced to `false`). |

#### Responses

##### `200 OK` (Public / Paginated)
```json
{
  "status": "success",
  "message": "Beneficiary categories retrieved successfully",
  "data": [
    {
      "id": "e2e5057e-e17f-4cfa-a7b2-bd72e6be31d9",
      "name": "Mahasiswa",
      "variant": "student",
      "hidden": false,
      "created_at": "2026-11-07T01:25:00+07:00",
      "updated_at": "2026-11-07T01:25:00+07:00"
    },
    {
      "id": "673f324d-6202-4fc8-9824-7f28bc9ef2d2",
      "name": "Panti Asuhan Anak",
      "variant": "orphanage",
      "hidden": false,
      "created_at": "2026-11-07T01:25:00+07:00",
      "updated_at": "2026-11-07T01:25:00+07:00"
    }
  ],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 23,
    "total_pages": 2
  }
}
```

##### `400 Bad Request` (Invalid Variant)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid variant filter. Allowed: student, orphanage, nursing_home, shelter, religious_community, foundation, society, other",
  "ref_id": "ref_83fa7102"
}
```

---

### 4.2 Get Beneficiary Category by ID

Retrieves an active beneficiary category by UUID. If the category is hidden, public unauthenticated requests will receive a `404 Not Found` response.

* **Method:** `GET`
* **Path:** `/api/v1/beneficiary-categories/:id`
* **Authentication:** Optional (`Bearer <JWT>`)
* **Role Permissions:** Public access for non-hidden categories. Staff access for all active categories.

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | Yes | Beneficiary Category unique UUID identifier. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Beneficiary category retrieved successfully",
  "data": {
    "id": "e2e5057e-e17f-4cfa-a7b2-bd72e6be31d9",
    "name": "Mahasiswa",
    "variant": "student",
    "hidden": false,
    "created_at": "2026-11-07T01:25:00+07:00",
    "updated_at": "2026-11-07T01:25:00+07:00"
  }
}
```

##### `400 Bad Request` (Malformed UUID)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid category ID format",
  "ref_id": "ref_48d91c7a"
}
```

##### `404 Not Found` (Missing, Deleted, or Hidden from Public)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Beneficiary category not found",
  "ref_id": "ref_74b5c102"
}
```

---

### 4.3 Create Beneficiary Category

Creates a new beneficiary category in the catalog.

* **Method:** `POST`
* **Path:** `/api/v1/beneficiary-categories`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** `super`, `core`, `staff`

#### Request Body

```json
{
  "name": "Panti Jompo Lansia Terlantar",
  "variant": "nursing_home",
  "hidden": false
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `name` | string | Yes | Min 1, Max 150 chars | Name of the beneficiary category. |
| `variant` | string | Yes | One of 8 allowed variants | Taxonomy classification. |
| `hidden` | boolean | No | Default `false` | When true, visible only to authenticated staff. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Beneficiary category created successfully",
  "data": {
    "id": "8905da67-9366-4c94-8178-569382218412",
    "name": "Panti Jompo Lansia Terlantar",
    "variant": "nursing_home",
    "hidden": false,
    "created_at": "2026-11-07T01:35:10+07:00",
    "updated_at": "2026-11-07T01:35:10+07:00"
  }
}
```

##### `400 Bad Request` (Validation Failure)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Validation failed",
  "ref_id": "ref_179b0c24",
  "errors": {
    "name": "Name is required",
    "variant": "Invalid variant. Allowed: student, orphanage, nursing_home, shelter, religious_community, foundation, society, other"
  }
}
```

##### `400 Bad Request` (Duplicate Active Name Conflict)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Database constraint violation",
  "ref_id": "ref_599b418a",
  "errors": {
    "name": "Beneficiary category already exists"
  }
}
```

---

### 4.4 Update Beneficiary Category

Performs a safe partial update on an active beneficiary category. Unmentioned fields retain their existing values.

* **Method:** `PATCH`
* **Path:** `/api/v1/beneficiary-categories/:id`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** `super`, `core`, `staff`

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | Yes | Category unique UUID identifier. |

#### Request Body

```json
{
  "name": "Panti Jompo & Lansia Sejahtera",
  "hidden": true
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `name` | string | No | Min 1, Max 150 chars | Updated category name. |
| `variant` | string | No | One of 8 allowed variants | Updated taxonomy variant. |
| `hidden` | boolean | No | Boolean | Updated visibility flag. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Beneficiary category updated successfully",
  "data": {
    "id": "8905da67-9366-4c94-8178-569382218412",
    "name": "Panti Jompo & Lansia Sejahtera",
    "variant": "nursing_home",
    "hidden": true,
    "created_at": "2026-11-07T01:35:10+07:00",
    "updated_at": "2026-11-07T01:36:25+07:00"
  }
}
```

##### `400 Bad Request` (Duplicate Name Conflict)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Database constraint violation",
  "ref_id": "ref_aa2817cc",
  "errors": {
    "name": "Beneficiary category already exists"
  }
}
```

##### `404 Not Found`
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Beneficiary category not found",
  "ref_id": "ref_93cb4a11"
}
```

---

### 4.5 Delete Beneficiary Category

Performs a soft delete on a beneficiary category (`deleted_at = NOW()`). Once deleted, the category is hidden from all list and get queries, and its name is freed for re-creation.

* **Method:** `DELETE`
* **Path:** `/api/v1/beneficiary-categories/:id`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** `super`, `core`, `staff`

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | Yes | Category unique UUID identifier. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Beneficiary category deleted successfully",
  "data": {
    "id": "8905da67-9366-4c94-8178-569382218412"
  }
}
```

##### `404 Not Found` (Already Deleted or Missing)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Beneficiary category not found",
  "ref_id": "ref_d83e2091"
}
```

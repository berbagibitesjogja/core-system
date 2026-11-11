# API Contract: Beneficiary Categories Management

> **Document Version:** `1.1.0`  
> **Topic Reference:** `006`, `010`, `011`  
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
| `PATCH` | `/api/v1/beneficiary-categories/:id` | Bearer JWT | `super`, `core`, `staff` | Partial safe update of mutable category fields (`name`, `address`, `maps`, `variant`, `email_domain`, `is_priority`, `hidden`). |
| `DELETE` | `/api/v1/beneficiary-categories/:id` | Bearer JWT | `super`, `core`, `staff` | Soft-delete category (`deleted_at = NOW()`). Releases active unique name constraint. |

---

## 3. Taxonomy Variants & Indexing Design

* **Allowed Taxonomy Variants (9 variants):**
  * `university` &mdash; Higher education institution (eligible for automatic Hero domain-matching and hero assignments).
  * `kipk` &mdash; Kartu Indonesia Pintar Kuliah priority category (eligible for hero assignments).
  * `orphanage` &mdash; Panti asuhan / orphanages.
  * `nursing_home` &mdash; Panti jompo / eldercare facilities.
  * `shelter` &mdash; Emergency or temporary shelters.
  * `religious_community` &mdash; Community religious centers / places of worship.
  * `foundation` &mdash; Registered charitable foundations (yayasan).
  * `society` &mdash; Community groups and neighborhood associations.
  * `other` &mdash; General or uncategorized beneficiary entities.
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
      "id": "f83366d2-9a98-4003-94ae-eb09d79b065c",
      "name": "Universitas Gadjah Mada",
      "address": "Bulaksumur, Caturtunggal, Sleman, DI Yogyakarta",
      "maps": "https://maps.app.goo.gl/ugm",
      "variant": "university",
      "email_domain": "mail.ugm.ac.id",
      "is_priority": true,
      "hidden": false,
      "created_at": "2026-11-07T01:25:00+07:00",
      "updated_at": "2026-11-07T01:25:00+07:00"
    },
    {
      "id": "673f324d-6202-4fc8-9824-7f28bc9ef2d2",
      "name": "Panti Asuhan Anak Yatim",
      "address": "Jl. Kaliurang KM 7, Sleman",
      "maps": null,
      "variant": "orphanage",
      "email_domain": null,
      "is_priority": false,
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
  "message": "Invalid variant filter. Allowed: university, kipk, orphanage, nursing_home, shelter, religious_community, foundation, society, other",
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
    "id": "f83366d2-9a98-4003-94ae-eb09d79b065c",
    "name": "Universitas Gadjah Mada",
    "address": "Bulaksumur, Caturtunggal, Sleman, DI Yogyakarta",
    "maps": "https://maps.app.goo.gl/ugm",
    "variant": "university",
    "email_domain": "mail.ugm.ac.id",
    "is_priority": true,
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
  "name": "Universitas Islam Indonesia",
  "address": "Jl. Kaliurang KM 14.5, Sleman",
  "maps": "https://maps.google.com/?q=uii",
  "variant": "university",
  "email_domain": "uii.ac.id",
  "is_priority": true,
  "hidden": false
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `name` | string | **Yes** | Min 1, Max 150 chars | Name of the beneficiary category. |
| `address` | string | No | Max 255 chars | Physical address of the entity. |
| `maps` | string | No | Max 255 chars | Google Maps URL or share link. |
| `variant` | string | **Yes** | One of 9 allowed variants | Taxonomy classification. |
| `email_domain` | string | No | Max 255 chars | Required only for domain-matched auto-enrollment; only valid when `variant = university`. Must contain a dot and be strictly lowercase. |
| `is_priority` | boolean | No | Default `false` | Marks category as a priority target for distributions. |
| `hidden` | boolean | No | Default `false` | When true, visible only to authenticated staff. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Beneficiary category created successfully",
  "data": {
    "id": "8905da67-9366-4c94-8178-569382218412",
    "name": "Universitas Islam Indonesia",
    "address": "Jl. Kaliurang KM 14.5, Sleman",
    "maps": "https://maps.google.com/?q=uii",
    "variant": "university",
    "email_domain": "uii.ac.id",
    "is_priority": true,
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
    "variant": "Invalid variant. Allowed: university, kipk, orphanage, nursing_home, shelter, religious_community, foundation, society, other",
    "email_domain": "Email domain can only be set for university variant"
  }
}
```

##### `400 Bad Request` (Duplicate Active Name or Domain Conflict)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Database constraint violation",
  "ref_id": "ref_599b418a",
  "errors": {
    "name": "Beneficiary category already exists",
    "email_domain": "Email domain is already registered"
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
  "name": "Universitas Islam Indonesia Terpadu",
  "is_priority": false,
  "hidden": true
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `name` | string | No | Min 1, Max 150 chars | Updated category name. |
| `address` | string | No | Max 255 chars | Updated physical address. |
| `maps` | string | No | Max 255 chars | Updated Google Maps link. |
| `variant` | string | No | One of 9 allowed variants | Updated taxonomy variant. Protected by hero enrollment guard. |
| `email_domain` | string | No | Max 255 chars | Updated email domain. Only allowed when variant is `university`. |
| `is_priority` | boolean | No | Boolean | Updated priority flag. |
| `hidden` | boolean | No | Boolean | Updated visibility flag. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Beneficiary category updated successfully",
  "data": {
    "id": "8905da67-9366-4c94-8178-569382218412",
    "name": "Universitas Islam Indonesia Terpadu",
    "address": "Jl. Kaliurang KM 14.5, Sleman",
    "maps": "https://maps.google.com/?q=uii",
    "variant": "university",
    "email_domain": "uii.ac.id",
    "is_priority": false,
    "hidden": true,
    "created_at": "2026-11-07T01:35:10+07:00",
    "updated_at": "2026-11-07T01:36:25+07:00"
  }
}
```

##### `400 Bad Request` (Hero Enrollment Variant Guard)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Validation failed",
  "ref_id": "ref_14319dc7",
  "errors": {
    "variant": "Cannot change category variant away from hero-eligible while heroes are actively enrolled in it"
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

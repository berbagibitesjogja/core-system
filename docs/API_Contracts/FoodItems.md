# API Contract: Food Items Management

> **Document Version:** `1.0.0`  
> **Topic ID:** `005`  
> **Base Path:** `/api/v1`  
> **Security:** Bearer JWT (`Authorization: Bearer <token>`) for mutating endpoints; Public access for read endpoints  
> **Rate Limiting:** Token Bucket (60 req/min per IP)  
> **Specification Format:** REST / JSON  

---

## 1. Global Standards & Conventions

### 1.1 Headers

| Header | Direction | Type | Description |
| :--- | :--- | :--- | :--- |
| `Content-Type` | Request / Response | String | `application/json; charset=utf-8` |
| `Authorization` | Request | String | `Bearer <access_token>` (Required for `POST`, `PATCH`, `DELETE`) |
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
  "message": "Food items retrieved successfully",
  "data": [],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 383,
    "total_pages": 20
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
| `GET` | `/api/v1/food-items` | Public | None | List food items (paginated, trigram search). Strips timestamps for minimal payload and blazing-fast autocomplete. |
| `GET` | `/api/v1/food-items/:id` | Public | None | Retrieve single food item by integer ID. Includes full entity metadata (`created_at`, `updated_at`). |
| `POST` | `/api/v1/food-items` | Bearer JWT | `super`, `core`, `staff` | Create a new food item. Enforces case-insensitive uniqueness (`uk_food_items_lower_name`). |
| `PATCH` | `/api/v1/food-items/:id` | Bearer JWT | `super`, `core`, `staff` | Update food item name by ID. Enforces case-insensitive uniqueness. |
| `DELETE` | `/api/v1/food-items/:id` | Bearer JWT | `super`, `core`, `staff` | Hard-delete food item by ID. Returns deleted ID. |

---

## 3. Performance & Indexing Design

* **Primary Key:** `SERIAL` integer ID (`int32`) for high-throughput indexed lookups.
* **GIN Trigram Index:** `idx_food_items_name_trgm` on `name gin_trgm_ops` provides fast substring matching (`ILIKE '%<query>%'`) across large dictionaries without sequential table scans.
* **Case-Insensitive Uniqueness:** Unique index `uk_food_items_lower_name` on `LOWER(name)` prevents duplicates regardless of casing (e.g. `ayam goreng` vs `Ayam Goreng`).
* **Lean List Payload:** `GET /api/v1/food-items` returns only `id` and `name` to minimize network overhead and database fetch time during autocomplete/typeahead queries.

---

## 4. Endpoint Specifications

### 4.1 List Food Items

Retrieves a paginated list of food items sorted alphabetically by name. Designed for autocomplete and search dropdowns.

* **Method:** `GET`
* **Path:** `/api/v1/food-items`
* **Authentication:** Public
* **Role Permissions:** None

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (must be > 0). |
| `limit` | integer | No | `20` | Records per page (maximum `100`). |
| `search` | string | No | `null` | Case-insensitive trigram substring search matching `name`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Food items retrieved successfully",
  "data": [
    {
      "id": 5,
      "name": "Ayam"
    },
    {
      "id": 6,
      "name": "Ayam Asam Manis"
    },
    {
      "id": 7,
      "name": "Ayam Bacem"
    }
  ],
  "meta": {
    "page": 1,
    "limit": 3,
    "total": 41,
    "total_pages": 14
  }
}
```

---

### 4.2 Get Food Item by ID

Retrieves complete information for a specific food item by its integer ID.

* **Method:** `GET`
* **Path:** `/api/v1/food-items/:id`
* **Authentication:** Public
* **Role Permissions:** None

#### Path Parameters

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `id` | integer | Positive integer food item ID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Food item retrieved successfully",
  "data": {
    "id": 5,
    "name": "Ayam",
    "created_at": "2026-11-07T01:04:48+07:00",
    "updated_at": "2026-11-07T01:04:48+07:00"
  }
}
```

##### `400 Bad Request`
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid food item ID format",
  "ref_id": "ref_9bf99bf3"
}
```

##### `404 Not Found`
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Food item not found",
  "ref_id": "ref_59942a6c"
}
```

---

### 4.3 Create Food Item

Creates a new food item in the dictionary.

* **Method:** `POST`
* **Path:** `/api/v1/food-items`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Request Body
```json
{
  "name": "Ayam Woku"
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `name` | string | Yes | 1 - 100 characters, unique (case-insensitive) | Food item name. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Food item created successfully",
  "data": {
    "id": 384,
    "name": "Ayam Woku",
    "created_at": "2026-11-07T01:09:40+07:00",
    "updated_at": "2026-11-07T01:09:40+07:00"
  }
}
```

##### `400 Bad Request (Validation Failure)`
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Validation failed",
  "ref_id": "ref_1876800e",
  "errors": {
    "name": "Food item name is required"
  }
}
```

##### `400 Bad Request (Unique Collision)`
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Food item already exists",
  "ref_id": "ref_82be20fa",
  "errors": {
    "name": "Food item already exists"
  }
}
```

##### `401 Unauthorized`
```json
{
  "status": "error",
  "code": "UNAUTHORIZED",
  "message": "Missing or invalid authentication token",
  "ref_id": "ref_42da9f1a"
}
```

##### `403 Forbidden`
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Insufficient permissions to perform this action",
  "ref_id": "ref_ce4020a6"
}
```

---

### 4.4 Update Food Item

Updates the name of an existing food item.

* **Method:** `PATCH`
* **Path:** `/api/v1/food-items/:id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Path Parameters

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `id` | integer | Positive integer food item ID. |

#### Request Body
```json
{
  "name": "Ayam Woku Manado"
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `name` | string | Yes | 1 - 100 characters, unique (case-insensitive) | New food item name. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Food item updated successfully",
  "data": {
    "id": 384,
    "name": "Ayam Woku Manado",
    "created_at": "2026-11-07T01:09:40+07:00",
    "updated_at": "2026-11-07T01:10:00+07:00"
  }
}
```

##### `400 Bad Request (Unique Collision)`
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Food item already exists",
  "ref_id": "ref_e69006fe",
  "errors": {
    "name": "Food item already exists"
  }
}
```

##### `404 Not Found`
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Food item not found",
  "ref_id": "ref_1a0c0e5b"
}
```

---

### 4.5 Delete Food Item

Permanently removes a food item from the database.

* **Method:** `DELETE`
* **Path:** `/api/v1/food-items/:id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Path Parameters

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `id` | integer | Positive integer food item ID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Food item deleted successfully",
  "data": {
    "id": 384
  }
}
```

##### `404 Not Found`
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Food item not found",
  "ref_id": "ref_812c30ab"
}
```

---

## 5. Error Code Reference

| Code | HTTP Status | Description |
| :--- | :--- | :--- |
| `VAL_001` | 400 Bad Request | Payload validation failure or invalid ID parameter format. |
| `VAL_002` | 400 Bad Request | Case-insensitive unique constraint collision on food item name (`uk_food_items_lower_name`). |
| `UNAUTHORIZED` | 401 Unauthorized | Missing, malformed, or expired Bearer JWT token. |
| `FORBIDDEN` | 403 Forbidden | Authenticated user lacks required staff role (`super`, `core`, `staff`). |
| `NOT_FOUND` | 404 Not Found | Requested food item ID does not exist. |
| `INTERNAL_ERROR` | 500 Internal Server Error | Unhandled server or database exception. |

# API Contract: Divisions Reference

> **Document Version:** `1.0.0`  
> **Topic ID:** `020`  
> **Base Path:** `/api/v1`  
> **Security:** Bearer JWT (`Authorization: Bearer <token>`, `staffOnly`: `super`, `core`, `staff`)  
> **Rate Limiting:** Token Bucket (60 req/min per IP)  
> **Specification Format:** REST / JSON  

---

## 1. Global Standards & Conventions

### 1.1 Headers

| Header | Direction | Type | Description |
| :--- | :--- | :--- | :--- |
| `Content-Type` | Request / Response | String | `application/json; charset=utf-8` |
| `Authorization` | Request | String | `Bearer <access_token>` (Required; active staff role) |
| `X-Request-Id` | Request / Response | String | Correlation ID format `ref_<hex>` (e.g. `ref_f5181fb0`). Propagated or auto-generated. |
| `Retry-After` | Response | Integer | Wait time in seconds when HTTP 429 is encountered. |

### 1.2 Response Envelopes

#### Standard Success (`200 OK`)
```json
{
  "status": "success",
  "message": "Divisions retrieved successfully",
  "data": []
}
```

#### Error Envelope (`4xx`, `5xx`)
```json
{
  "status": "error",
  "code": "ERROR_CODE_STRING",
  "message": "Safe human-readable summary of the error",
  "ref_id": "ref_a2a590df"
}
```

---

## 2. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/divisions` | Bearer JWT | Staff (`super`, `core`, `staff`) | List all active (non-deleted) organizational divisions ordered by `id ASC`. |

---

## 3. Endpoint Specifications

### 3.1 List Divisions

Retrieves all active (non-soft-deleted) divisions ordered ascending by `id`. Designed as a lightweight lookup endpoint for staff filters, invitations, division transfers, and war-slot configuration.

* **Method:** `GET`
* **Path:** `/api/v1/divisions`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** Any active staff (`super`, `core`, `staff`)

#### Query Parameters

None. (No pagination or filtering.)

#### Data Model (`DivisionItem`)

| Field | Type | Description |
| :--- | :--- | :--- |
| `id` | `integer` (`int32`) | Division ID (`1 = IT`, `2 = Managerial`, `3 = Operational`, `4 = Medinfo`). |
| `name` | `string` | Division name (`max 24 chars`). |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Divisions retrieved successfully",
  "data": [
    {
      "id": 1,
      "name": "IT"
    },
    {
      "id": 2,
      "name": "Managerial"
    },
    {
      "id": 3,
      "name": "Operational"
    },
    {
      "id": 4,
      "name": "Medinfo"
    }
  ]
}
```
*Note: Returns `"data": []` (never `null`) when no active divisions exist.*

##### `401 Unauthorized`
```json
{
  "status": "error",
  "code": "UNAUTHORIZED",
  "message": "Missing or invalid authorization token",
  "ref_id": "ref_a2a590df"
}
```

##### `403 Forbidden`
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Staff access required",
  "ref_id": "ref_a2a590df"
}
```

##### `429 Too Many Requests`
```json
{
  "status": "error",
  "code": "RATE_LIMIT_EXCEEDED",
  "message": "Too many requests. Please try again later.",
  "ref_id": "ref_a2a590df"
}
```

##### `500 Internal Server Error`
```json
{
  "status": "error",
  "code": "INTERNAL_ERROR",
  "message": "Failed to retrieve divisions",
  "ref_id": "ref_a2a590df"
}
```

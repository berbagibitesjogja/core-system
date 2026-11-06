# API Contract: Reimbursements Management

> **Document Version:** `1.0.0`  
> **Topic ID:** `007`  
> **Base Path:** `/api/v1`  
> **Security:** Bearer JWT (`Authorization: Bearer <token>`) required for all endpoints  
> **Rate Limiting:** Token Bucket (60 req/min per IP)  
> **Specification Format:** REST / JSON  

---

## 1. Global Standards & Conventions

### 1.1 Headers

| Header | Direction | Type | Description |
| :--- | :--- | :--- | :--- |
| `Content-Type` | Request / Response | String | `application/json; charset=utf-8` |
| `Authorization` | Request | String | `Bearer <access_token>` (Required for all endpoints) |
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
  "message": "Reimbursements retrieved successfully",
  "data": [],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 45,
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
*Note: `errors` map is only included when field-level validation or constraint conflicts occur.*

---

## 2. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `POST` | `/api/v1/reimbursements` | Bearer JWT | Authenticated | Create a reimbursement claim. User ID is inferred from token; supports multi-receipt file linking. |
| `GET` | `/api/v1/reimbursements` | Bearer JWT | Authenticated | List reimbursement claims (paginated, filtered by user, status, search). |
| `GET` | `/api/v1/reimbursements/:id` | Bearer JWT | Authenticated | Retrieve a single active reimbursement claim by UUID including attached receipt file IDs. |
| `PATCH` | `/api/v1/reimbursements/:id` | Bearer JWT | Authenticated | Safe partial update of reimbursement details, status, audit trail, or attached receipt files. |
| `DELETE` | `/api/v1/reimbursements/:id` | Bearer JWT | Authenticated | Soft-delete a reimbursement (`deleted_at = NOW()`). |

*Note: Role-based authorization policies (e.g., restricting approval/payment to `super` and own-claim editing to applicants) are defined in Topic 008 RBAC Middleware.*

---

## 3. Database & Relational Design

* **Primary Key:** UUID (`id UUID DEFAULT gen_random_uuid()`).
* **Applicant Expense Receipts (1-to-Many):**
  Tracked in junction table `reimbursement_files`:
  * `reimbursement_id UUID REFERENCES reimbursements(id) ON DELETE CASCADE`
  * `file_id UUID REFERENCES files(id) ON DELETE RESTRICT`
  * Unique pair constraint: `uq_reimbursement_files_pair (reimbursement_id, file_id)`
* **Treasurer Proof of Payment (1-to-1):**
  Tracked directly on main table:
  * `payment_proof UUID REFERENCES files(id) ON DELETE SET NULL`
* **Audit Trail:**
  * `paid_by UUID REFERENCES users(id) ON DELETE SET NULL`
  * `paid_at TIMESTAMPTZ`
  * `rejection_reason TEXT`
* **Status State Machine:**
  * `status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'rejected'))`
* **Soft Delete:**
  * `deleted_at TIMESTAMPTZ` ensures financial audit records are never physically destroyed.

---

## 4. Endpoint Specifications

### 4.1 Create Reimbursement

Submits a new reimbursement expense claim. The applicant's `user_id` is automatically extracted from the authenticated JWT claims. Multiple receipt file IDs can be attached atomically.

* **Method:** `POST`
* **Path:** `/api/v1/reimbursements`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** Authenticated Users

#### Request Body

```json
{
  "amount": 45000,
  "method": "GoPay",
  "target": "087870635415",
  "notes": "Beli plastik wrap rescue operasional",
  "file_ids": [
    "b9e87ca6-adb2-e0d2-c24b-93752b7555b5"
  ]
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `amount` | integer | Yes | Positive integer (> 0) | Reimbursement claim amount in IDR. |
| `method` | string | Yes | Min 1, Max 50 chars | Disbursement method (e.g. `BCA`, `GoPay`, `Cash`). |
| `target` | string | Yes | Min 1, Max 100 chars | Account number, phone number, or transfer destination. |
| `notes` | string | No | Text or null | Expense description or justification. |
| `file_ids` | array[UUID] | No | Array of valid UUIDs | Attached receipt file IDs (must exist in `files` table). |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Reimbursement created successfully",
  "data": {
    "id": "3972cb1a-ac8d-4496-bd1e-16a5d7b8cb31",
    "user_id": "00000000-0000-0000-0000-000000000001",
    "amount": 45000,
    "method": "GoPay",
    "target": "087870635415",
    "notes": "Beli plastik wrap rescue operasional",
    "status": "pending",
    "payment_proof": null,
    "paid_by": null,
    "paid_at": null,
    "rejection_reason": null,
    "file_ids": [
      "b9e87ca6-adb2-e0d2-c24b-93752b7555b5"
    ],
    "created_at": "2026-11-07T02:16:20+07:00",
    "updated_at": "2026-11-07T02:16:20+07:00"
  }
}
```

##### `400 Bad Request` (Validation Failure)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Validation failed",
  "ref_id": "ref_59ba12dd",
  "errors": {
    "amount": "Amount must be greater than zero",
    "method": "Payment method cannot be empty",
    "target": "Target cannot be empty"
  }
}
```

##### `400 Bad Request` (Referenced Receipt File Not Found)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Database constraint violation",
  "ref_id": "ref_aa1298c1",
  "errors": {
    "file_id": "Referenced receipt file does not exist"
  }
}
```

---

### 4.2 List Reimbursements

Retrieves a paginated list of reimbursement claims with optional filters by applicant user ID, status, and substring search on target/notes.

* **Method:** `GET`
* **Path:** `/api/v1/reimbursements`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** Authenticated Users

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | 1-indexed page number (must be > 0). |
| `limit` | integer | No | `20` | Records per page (maximum `100`). |
| `user_id` | UUID | No | `null` | Filter claims submitted by a specific user. |
| `status` | string | No | `null` | Filter by claim status: `pending`, `completed`, `rejected`. |
| `search` | string | No | `null` | Case-insensitive substring search matching `target` or `notes`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Reimbursements retrieved successfully",
  "data": [
    {
      "id": "3972cb1a-ac8d-4496-bd1e-16a5d7b8cb31",
      "user_id": "00000000-0000-0000-0000-000000000001",
      "amount": 45000,
      "method": "GoPay",
      "target": "087870635415",
      "notes": "Beli plastik wrap rescue operasional",
      "status": "pending",
      "payment_proof": null,
      "paid_by": null,
      "paid_at": null,
      "rejection_reason": null,
      "file_ids": [
        "b9e87ca6-adb2-e0d2-c24b-93752b7555b5"
      ],
      "created_at": "2026-11-07T02:16:20+07:00",
      "updated_at": "2026-11-07T02:16:20+07:00"
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

##### `400 Bad Request` (Invalid Status Filter)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid status filter. Allowed: pending, completed, rejected",
  "ref_id": "ref_37be81c4"
}
```

---

### 4.3 Get Reimbursement by ID

Retrieves an active reimbursement claim and its attached receipt file IDs by UUID.

* **Method:** `GET`
* **Path:** `/api/v1/reimbursements/:id`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** Authenticated Users

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | Yes | Reimbursement unique UUID identifier. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Reimbursement retrieved successfully",
  "data": {
    "id": "3972cb1a-ac8d-4496-bd1e-16a5d7b8cb31",
    "user_id": "00000000-0000-0000-0000-000000000001",
    "amount": 45000,
    "method": "GoPay",
    "target": "087870635415",
    "notes": "Beli plastik wrap rescue operasional",
    "status": "pending",
    "payment_proof": null,
    "paid_by": null,
    "paid_at": null,
    "rejection_reason": null,
    "file_ids": [
      "b9e87ca6-adb2-e0d2-c24b-93752b7555b5"
    ],
    "created_at": "2026-11-07T02:16:20+07:00",
    "updated_at": "2026-11-07T02:16:20+07:00"
  }
}
```

##### `404 Not Found` (Missing or Soft-Deleted)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Reimbursement not found",
  "ref_id": "ref_90d21e88"
}
```

---

### 4.4 Update Reimbursement

Performs a safe partial update on an active reimbursement claim. If `file_ids` is provided in the body, existing receipt linkages are atomically cleared and replaced in a transaction.

* **Method:** `PATCH`
* **Path:** `/api/v1/reimbursements/:id`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** Authenticated Users

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | Yes | Reimbursement unique UUID identifier. |

#### Request Body

```json
{
  "status": "completed",
  "payment_proof": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
  "paid_by": "00000000-0000-0000-0000-000000000001",
  "paid_at": "2026-11-07T02:30:00+07:00"
}
```

| Field | Type | Required | Constraints | Description |
| :--- | :--- | :--- | :--- | :--- |
| `amount` | integer | No | Positive integer (> 0) | Updated claim amount. |
| `method` | string | No | Min 1, Max 50 chars | Updated payment method. |
| `target` | string | No | Min 1, Max 100 chars | Updated payment target. |
| `notes` | string | No | Text or null | Updated notes. |
| `status` | string | No | `pending`, `completed`, `rejected` | Updated claim state. |
| `payment_proof` | UUID | No | Valid UUID or null | Treasurer payment proof file ID (`files(id)`). |
| `paid_by` | UUID | No | Valid UUID or null | Treasurer user ID (`users(id)`). |
| `paid_at` | string | No | RFC3339 timestamp or null | Timestamp of payment execution. |
| `rejection_reason` | string | No | Text or null | Reason if claim is rejected. |
| `file_ids` | array[UUID] | No | Array of valid UUIDs | Replacement receipt file IDs. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Reimbursement updated successfully",
  "data": {
    "id": "3972cb1a-ac8d-4496-bd1e-16a5d7b8cb31",
    "user_id": "00000000-0000-0000-0000-000000000001",
    "amount": 45000,
    "method": "GoPay",
    "target": "087870635415",
    "notes": "Beli plastik wrap rescue operasional",
    "status": "completed",
    "payment_proof": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
    "paid_by": "00000000-0000-0000-0000-000000000001",
    "paid_at": "2026-11-07T02:30:00+07:00",
    "rejection_reason": null,
    "file_ids": [
      "b9e87ca6-adb2-e0d2-c24b-93752b7555b5"
    ],
    "created_at": "2026-11-07T02:16:20+07:00",
    "updated_at": "2026-11-07T02:30:15+07:00"
  }
}
```

##### `400 Bad Request` (Invalid Payment Proof UUID)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "Database constraint violation",
  "ref_id": "ref_28ca9155",
  "errors": {
    "payment_proof": "Referenced payment proof file does not exist"
  }
}
```

##### `404 Not Found`
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Reimbursement not found",
  "ref_id": "ref_90d21e88"
}
```

---

### 4.5 Delete Reimbursement

Performs a soft delete on a reimbursement claim (`deleted_at = NOW()`). Soft-deleted claims are excluded from list and get queries while preserving financial history in the database.

* **Method:** `DELETE`
* **Path:** `/api/v1/reimbursements/:id`
* **Authentication:** Bearer JWT Required
* **Role Permissions:** Authenticated Users

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | Yes | Reimbursement unique UUID identifier. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Reimbursement deleted successfully",
  "data": {
    "id": "3972cb1a-ac8d-4496-bd1e-16a5d7b8cb31"
  }
}
```

##### `404 Not Found`
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Reimbursement not found",
  "ref_id": "ref_42af901b"
}
```

# API Contract: Staff Management & Role Governance

> **Document Version:** `1.0.0`  
> **Topic Reference:** `012`  
> **Base Path:** `/api/v1`  
> **Security:** Bearer JWT (`Authorization: Bearer <token>`) & Token Bucket Rate Limiting (60 req/min per IP)  
> **Specification Format:** REST / JSON  

---

## 1. Global Standards & Conventions

### 1.1 Headers

| Header | Direction | Type | Description |
| :--- | :--- | :--- | :--- |
| `Content-Type` | Request / Response | String | `application/json; charset=utf-8` |
| `Authorization` | Request | String | `Bearer <access_token>` (Required for all staff endpoints) |
| `X-Request-Id` | Request / Response | String | Correlation ID format `ref_<hex>` (e.g. `ref_72fed63a`). Propagated or auto-generated. |
| `Retry-After` | Response | Integer | Wait time in seconds when HTTP 429 rate limit is encountered. |

### 1.2 Response Envelopes

#### Standard Success Envelope (`200 OK`)
```json
{
  "status": "success",
  "message": "Human-readable description of the completed action",
  "data": {}
}
```

#### Paginated Success Envelope (`200 OK`)
```json
{
  "status": "success",
  "message": "Staff members retrieved successfully",
  "data": [],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 10,
    "total_pages": 1
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

## 2. Role Governance & State Transition Matrix

Staff governance implements a strict three-tier hierarchy (`super` > `core` > `staff`):

| Caller Role | Allowed Role Mutations | Allowed Division Transfers | Allowed Kick (`DELETE /staff/:id`) | Allowed Self-Demote (`PATCH /staff/me`) | Allowed Self-Resign (`DELETE /staff/me`) |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **`super`** | Promote / demote anyone to `super`, `core`, or `staff` *(enforces Last Super invariant)* | Yes, to any existing division | Yes, kicks target to Hero *(enforces Last Super invariant)* | Yes, demotes self to `core` or `staff` *(enforces Last Super invariant)* | Yes, steps down to pure Hero *(enforces Last Super invariant)* |
| **`core`** | Can only promote `staff` &rarr; `core` **within caller's own division**. Cannot promote to `super`. Cannot demote other members. | No (`403 Forbidden`) | No (`403 Forbidden`) | Yes, demotes self from `core` &rarr; `staff` | Yes, steps down to pure Hero |
| **`staff`** | None (`403 Forbidden`) | No (`403 Forbidden`) | No (`403 Forbidden`) | No, already at base staff tier (`400 VAL_001`) | Yes, steps down to pure Hero |

### 2.1 Critical Invariants

1. **Last Super Lockout Prevention (`ROLE_LAST_SUPER`)**:
   - The system strictly forbids demoting, kicking, or self-resigning the last remaining active Super Administrator in the entire organization.
   - If `CountActiveSuperAdmins <= 1`, requests attempting to remove the super role fail immediately with `400 Bad Request` and error code `ROLE_LAST_SUPER`.
2. **Session Revocation & Continuation**:
   - Whenever a staff member's role or division is altered by an administrator, all existing refresh token sessions for the target user are revoked immediately in the database (`revoked_at = NOW()`), forcing re-authentication with fresh permissions.
   - When a caller demotes themselves (`PATCH /api/v1/staff/me`) or resigns (`DELETE /api/v1/staff/me`), the server issues an updated JWT token pair directly in the response payload (`AuthTokenResponse`), allowing client applications to seamlessly downgrade authorization state without logging the user out.
3. **Kick to Hero & Self-Resignation**:
   - When a staff member is kicked (`DELETE /api/v1/staff/:id`) or resigns (`DELETE /api/v1/staff/me`), their record in the `staff` table is soft-deleted (`deleted_at = NOW()`).
   - A corresponding record in the `heroes` table is guaranteed to exist (`INSERT INTO heroes ... ON CONFLICT DO NOTHING`).
   - Kicked or resigned users immediately lose staff status (`is_staff = false`, `role = ""`, `division_id = 0`) across all authorization middleware and queries.

---

## 3. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/staff` | Bearer JWT | `super`, `core`, `staff` | List active staff members with pagination, role/division filtering, and search |
| `GET` | `/api/v1/staff/:id` | Bearer JWT | `super`, `core`, `staff` | Retrieve profile and division details of a single active staff member |
| `PATCH` | `/api/v1/staff/:id` | Bearer JWT | `super`, `core` | Promote/demote staff member or reassign division (subject to governance matrix) |
| `PATCH` | `/api/v1/staff/me` | Bearer JWT | `super`, `core` | Self-demote caller role; revokes old sessions and returns updated JWT tokens |
| `DELETE` | `/api/v1/staff/:id` | Bearer JWT | `super` | Remove staff privileges and transition user to Hero; revokes target sessions |
| `DELETE` | `/api/v1/staff/me` | Bearer JWT | `super`, `core`, `staff` | Voluntary resignation from staff; transitions caller to pure Hero with updated JWTs |

---

## 4. Endpoint Specifications

### 4.1 List Staff Members

Retrieves a paginated list of active staff members ordered by role hierarchy (`super` &rarr; `core` &rarr; `staff`) and then user name. Excludes soft-deleted staff members.

* **Method:** `GET`
* **Path:** `/api/v1/staff`
* **Authentication:** Bearer JWT (`is_staff = true`)

#### Query Parameters

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `page` | integer | No | `1` | Page number (1-indexed). |
| `limit` | integer | No | `20` | Items per page (max `100`). |
| `division_id` | integer | No | *empty* | Filter staff by division ID. |
| `role` | string | No | *empty* | Filter staff by role (`super`, `core`, `staff`). |
| `search` | string | No | *empty* | Case-insensitive substring search matching user `name` or `email`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Staff members retrieved successfully",
  "data": [
    {
      "user_id": "00000000-0000-0000-0000-000000000001",
      "email": "it@berbagibitesjogja.com",
      "name": "Tim IT",
      "phone_number": "620000000001",
      "profile_pic": null,
      "pfp_external_url": null,
      "is_active": true,
      "division_id": 1,
      "division_name": "IT",
      "role": "super",
      "created_at": "2026-11-11T11:04:14+07:00",
      "updated_at": "2026-11-11T11:04:14+07:00"
    },
    {
      "user_id": "00000000-0000-0000-0000-000000000003",
      "email": "mg.c@berbagibitesjogja.com",
      "name": "Managerial Core",
      "phone_number": "620000000003",
      "profile_pic": null,
      "pfp_external_url": null,
      "is_active": true,
      "division_id": 2,
      "division_name": "Managerial",
      "role": "core",
      "created_at": "2026-11-11T11:04:14+07:00",
      "updated_at": "2026-11-11T11:04:14+07:00"
    }
  ],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 10,
    "total_pages": 1
  }
}
```

##### `400 Bad Request`
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid division_id filter",
  "ref_id": "ref_8129a0f1"
}
```

##### `401 Unauthorized`
```json
{
  "status": "error",
  "code": "UNAUTHORIZED",
  "message": "Missing or invalid authentication token",
  "ref_id": "ref_c8901234"
}
```

##### `403 Forbidden`
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Staff privileges required",
  "ref_id": "ref_90123456"
}
```

---

### 4.2 Get Staff Member by ID

Retrieves profile, contact, and division details for a specific active staff member.

* **Method:** `GET`
* **Path:** `/api/v1/staff/:id`
* **Authentication:** Bearer JWT (`is_staff = true`)

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID or string | **Yes** | Target staff user UUID, or the literal string `"me"` for the caller. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Staff member retrieved successfully",
  "data": {
    "user_id": "00000000-0000-0000-0000-000000000004",
    "email": "mg.sf@berbagibitesjogja.com",
    "name": "Managerial Staff",
    "phone_number": "620000000004",
    "profile_pic": null,
    "pfp_external_url": null,
    "is_active": true,
    "division_id": 2,
    "division_name": "Managerial",
    "role": "staff",
    "created_at": "2026-11-11T11:04:14+07:00",
    "updated_at": "2026-11-11T11:04:14+07:00"
  }
}
```

##### `400 Bad Request` (Invalid UUID format)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Invalid staff user ID format",
  "ref_id": "ref_109283ab"
}
```

##### `404 Not Found` (Missing or Soft-Deleted Staff)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Staff member not found",
  "ref_id": "ref_abcdef12"
}
```

---

### 4.3 Update Staff Role or Division

Mutates a staff member's role and/or division assignment. Enforces the complete transition governance matrix, division boundaries for Core members, and the Last Super lockout check. Target user sessions are revoked upon successful update.

* **Method:** `PATCH`
* **Path:** `/api/v1/staff/:id`
* **Authentication:** Bearer JWT (`is_staff = true`, `role IN ('super', 'core')`)

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID or string | **Yes** | Target staff user UUID, or `"me"`. |

#### Request Body
```json
{
  "role": "core",
  "division_id": 2
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `role` | string | Optional* | Must be `'super'`, `'core'`, or `'staff'`. Must differ from target's current role. |
| `division_id` | integer | Optional* | Valid division ID (1-4). Modifying this field strictly requires `caller.Role == "super"`. |

*\*At least one of `role` or `division_id` must be provided.*

#### Responses

##### `200 OK` (Updating another staff member)
```json
{
  "status": "success",
  "message": "Staff member updated successfully",
  "data": {
    "user_id": "00000000-0000-0000-0000-000000000004",
    "email": "mg.sf@berbagibitesjogja.com",
    "name": "Managerial Staff",
    "phone_number": "620000000004",
    "profile_pic": null,
    "pfp_external_url": null,
    "is_active": true,
    "division_id": 2,
    "division_name": "Managerial",
    "role": "core",
    "created_at": "2026-11-11T11:04:14+07:00",
    "updated_at": "2026-11-11T12:22:18+07:00"
  }
}
```

##### `200 OK` (Self-demotion via `:id = "me"` or caller UUID)
```json
{
  "status": "success",
  "message": "Role updated successfully",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "5e60dbf538f1bf4a60d2ccf39c86f2cd10ffb830860e1a5e0aef11c717461d3b",
    "expires_in": 900,
    "user": {
      "id": "00000000-0000-0000-0000-000000000003",
      "email": "mg.c@berbagibitesjogja.com",
      "name": "Managerial Core",
      "phone_number": "620000000003",
      "is_active": true,
      "is_staff": true,
      "role": "staff",
      "division_id": 2,
      "division_name": "Managerial",
      "is_hero": false,
      "email_verified_at": "2026-11-11T11:04:14+07:00"
    }
  }
}
```

##### `400 Bad Request` (Last Super Lockout Prevention)
```json
{
  "status": "error",
  "code": "ROLE_LAST_SUPER",
  "message": "Cannot demote the only remaining Super Administrator",
  "ref_id": "ref_91a0b3f5"
}
```

##### `400 Bad Request` (Identical Role Redundancy)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Staff member already has role 'core'",
  "ref_id": "ref_8192a01f"
}
```

##### `403 Forbidden` (Division Boundary Violation)
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Core members can only promote staff within their own division",
  "ref_id": "ref_59bf3153"
}
```

##### `403 Forbidden` (Division Transfer by Non-Super)
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Only Super Administrators can reassign staff divisions",
  "ref_id": "ref_48102f9c"
}
```

##### `403 Forbidden` (Core Demoting Others or Promoting to Super)
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Core members can only promote staff to Core",
  "ref_id": "ref_72b38410"
}
```

---

### 4.4 Self-Demote Staff

Convenience endpoint for staff administrators to step down to a lower staff role. On success, existing refresh tokens are revoked, and a fresh JWT pair is issued reflecting the downgraded role.

* **Method:** `PATCH`
* **Path:** `/api/v1/staff/me`
* **Authentication:** Bearer JWT (`is_staff = true`, `role IN ('super', 'core')`)

#### Request Body
```json
{
  "role": "staff"
}
```

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `role` | string | **Yes** | Lower role requested. For Core, must be `'staff'`. For Super, can be `'core'` or `'staff'`. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Role updated successfully",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "5e60dbf538f1bf4a60d2ccf39c86f2cd10ffb830860e1a5e0aef11c717461d3b",
    "expires_in": 900,
    "user": {
      "id": "00000000-0000-0000-0000-000000000003",
      "email": "mg.c@berbagibitesjogja.com",
      "name": "Managerial Core",
      "phone_number": "620000000003",
      "is_active": true,
      "is_staff": true,
      "role": "staff",
      "division_id": 2,
      "division_name": "Managerial",
      "is_hero": false,
      "email_verified_at": "2026-11-11T11:04:14+07:00"
    }
  }
}
```

##### `400 Bad Request` (Attempting Self-Demotion at Base Staff Tier)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Staff member already has role 'staff'",
  "ref_id": "ref_8192a01f"
}
```

##### `400 Bad Request` (Last Super Lockout Prevention)
```json
{
  "status": "error",
  "code": "ROLE_LAST_SUPER",
  "message": "Cannot demote the only remaining Super Administrator",
  "ref_id": "ref_91a0b3f5"
}
```

---

### 4.5 Kick Staff to Hero

Super-only operation that strips all staff privileges from a user. Soft-deletes the target's `staff` record (`deleted_at = NOW()`), guarantees a 1:1 `heroes` profile exists, and revokes all active refresh tokens.

* **Method:** `DELETE`
* **Path:** `/api/v1/staff/:id`
* **Authentication:** Bearer JWT (`is_staff = true`, `role = "super"`)

#### Path Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | **Yes** | User ID of the staff member to kick. Cannot be caller's own ID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Staff member removed successfully"
}
```

##### `400 Bad Request` (Attempting to Kick Self)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Cannot kick yourself. Use the resign endpoint to step down.",
  "ref_id": "ref_7109283a"
}
```

##### `400 Bad Request` (Last Super Lockout Prevention)
```json
{
  "status": "error",
  "code": "ROLE_LAST_SUPER",
  "message": "Cannot remove the only remaining Super Administrator",
  "ref_id": "ref_a019283f"
}
```

##### `403 Forbidden` (Caller Not Super Admin)
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Only Super Administrators can remove staff members",
  "ref_id": "ref_6810234a"
}
```

##### `404 Not Found`
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Staff member not found",
  "ref_id": "ref_abcdef12"
}
```

---

### 4.6 Resign from Staff

Voluntary self-resignation. Available to any active staff member. Soft-deletes the caller's `staff` record, guarantees a `heroes` profile exists, revokes all current sessions, and immediately returns a new JWT token pair with pure Hero privileges (`is_staff = false`, `is_hero = true`).

* **Method:** `DELETE`
* **Path:** `/api/v1/staff/me`
* **Authentication:** Bearer JWT (`is_staff = true`)

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Resigned from staff successfully. You are now a Hero.",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "64b4dc1398358a04966a6ad2055e64d303c88a58b0af705d9aba844e8bba9454",
    "expires_in": 900,
    "user": {
      "id": "00000000-0000-0000-0000-000000000010",
      "email": "medinfo.sf@berbagibitesjogja.com",
      "name": "Medinfo Staff",
      "phone_number": "620000000010",
      "is_active": true,
      "is_staff": false,
      "is_hero": true,
      "email_verified_at": "2026-11-11T11:04:14+07:00"
    }
  }
}
```

##### `400 Bad Request` (Last Super Lockout Prevention)
```json
{
  "status": "error",
  "code": "ROLE_LAST_SUPER",
  "message": "Cannot resign as the only remaining Super Administrator",
  "ref_id": "ref_b019283f"
}
```

##### `401 Unauthorized`
```json
{
  "status": "error",
  "code": "UNAUTHORIZED",
  "message": "Authentication required",
  "ref_id": "ref_1829304a"
}
```

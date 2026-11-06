# API Contract: Auth & User Management (Invite-Only System)

> **Document Version:** `1.0.0`  
> **Topic ID:** `002`  
> **Base Path:** `/api/v1`  
> **Security:** Bearer JWT (`Authorization: Bearer <token>`) & Token Bucket Rate Limiting (60 req/min per IP)  
> **Specification Format:** REST / JSON  

---

## 1. Global Standards & Conventions

### 1.1 Headers

| Header | Direction | Type | Description |
| :--- | :--- | :--- | :--- |
| `Content-Type` | Request / Response | String | `application/json; charset=utf-8` |
| `Authorization` | Request | String | `Bearer <access_token>` (Required for protected endpoints) |
| `X-Request-Id` | Request / Response | String | Correlation ID format `ref_<hex>` (e.g. `ref_fcf501b3`). Propagated or auto-generated. |
| `Retry-After` | Response | Integer | Wait time in seconds when HTTP 429 is encountered. |

### 1.2 Response Envelopes

#### Success Envelope (`2xx`)
```json
{
  "status": "success",
  "message": "Human-readable description of the completed action",
  "data": {}
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
| `POST` | `/api/v1/invitations` | Bearer JWT | `super`, `core` | Create new user invitation & dispatch invite email |
| `GET` | `/api/v1/invitations/validate` | Public | None | Validate raw invitation token & return pre-fill data |
| `POST` | `/api/v1/auth/register` | Public | None (Token) | Complete invite-only registration and receive initial JWTs |
| `POST` | `/api/v1/auth/login` | Public | None | Authenticate active user via email & password |
| `POST` | `/api/v1/auth/refresh` | Public | None | Rotate refresh token and obtain fresh access token |
| `POST` | `/api/v1/auth/logout` | Public | None | Revoke active refresh token session |

---

## 3. Endpoint Specifications

### 3.1 Create Invitation

Creates a cryptographic invitation record, records inviter identity, normalizes telephone number via DB trigger, and dispatches an invitation email with a register link.

* **Method:** `POST`
* **Path:** `/api/v1/invitations`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:**
  * `super`: Permitted to invite `super`, `core`, and `staff`.
  * `core`: Permitted to invite **only** `staff`.
  * `staff`: Access Denied (`403 Forbidden`).

#### Request Body
```json
{
  "email": "sarah.core@berbagibitesjogja.com",
  "role": "core",
  "division_id": 2,
  "name": "Sarah Jenkins",
  "phone_number": "+62 812-3456-7890"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `email` | string | **Yes** | Valid email address format, max 255 chars. Must not belong to an existing active user. |
| `role` | string | **Yes** | One of: `"super"`, `"core"`, `"staff"`. Subject to caller role guardrails. |
| `division_id` | integer | **Yes** | Must match existing record in `divisions` table. |
| `name` | string | No | Max 255 characters. |
| `phone_number` | string | No | Max 20 characters. Automatically sanitized to Indonesian standard format (`628...`) by database trigger. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Invitation created successfully",
  "data": {
    "id": "337fa500-030d-4882-92d1-9f68765f5ecf",
    "email": "sarah.core@berbagibitesjogja.com",
    "role": "core",
    "division_id": 2,
    "name": "Sarah Jenkins",
    "phone_number": "6281234567890",
    "expires_at": "2026-11-02T08:12:54.757823+07:00",
    "token": "5baca0df9e4d2fdffddc8aacfa942406dac5fa1ac6e547a915ece961d298bcb9",
    "invite_link": "https://app.berbagibitesjogja.com/register?token=5baca0df9e4d2fdffddc8aacfa942406dac5fa1ac6e547a915ece961d298bcb9"
  }
}
```

##### `400 Bad Request` (Existing User or Invalid Reference)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "An active user with this email already exists",
  "ref_id": "ref_fcf501b3",
  "errors": {
    "email": "Email is already registered"
  }
}
```

##### `403 Forbidden` (Role Guardrail Violation)
```json
{
  "status": "error",
  "code": "FORBIDDEN",
  "message": "Forbidden: insufficient permissions to invite this role",
  "ref_id": "ref_18da9b02"
}
```

---

### 3.2 Validate Invitation

Validates the raw 64-character invitation token from the frontend register page URL. Provides pre-filled fields so the user cannot alter their assigned role, division, or email during registration.

* **Method:** `GET`
* **Path:** `/api/v1/invitations/validate`
* **Authentication:** None (Public)

#### Query Parameters
| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `token` | string | **Yes** | 64-character raw hex token received in query string. |

#### Responses

##### `200 OK` (Valid & Unexpired)
```json
{
  "status": "success",
  "message": "Invitation is valid",
  "data": {
    "valid": true,
    "email": "sarah.core@berbagibitesjogja.com",
    "role": "core",
    "division_id": 2,
    "name": "Sarah Jenkins",
    "phone_number": "6281234567890",
    "expires_at": "2026-11-02T08:12:54+07:00",
    "is_expired": false,
    "is_accepted": false
  }
}
```

##### `404 Not Found` (Non-existent Token)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Invitation token not found or invalid",
  "ref_id": "ref_d91823ab"
}
```

##### `410 Gone` (Expired or Already Accepted)
```json
{
  "status": "error",
  "code": "INVITATION_ALREADY_USED",
  "message": "This invitation has already been accepted",
  "ref_id": "ref_b3fd883c"
}
```

---

### 3.3 Register via Invitation

Consumes a valid invitation token, verifies password security, creates the user account in an atomic transaction, marks `email_verified_at = NOW()`, updates the invitation record as accepted, and generates the initial JWT auth session.

* **Method:** `POST`
* **Path:** `/api/v1/auth/register`
* **Authentication:** None (Public)

#### Request Body
```json
{
  "invitation_token": "5baca0df9e4d2fdffddc8aacfa942406dac5fa1ac6e547a915ece961d298bcb9",
  "password": "SecurePassword123!",
  "name": "Sarah Jenkins",
  "phone_number": "+62 812-3456-7890"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `invitation_token` | string | **Yes** | 64-character raw invitation token. |
| `password` | string | **Yes** | Minimum 8 characters. Hashed via bcrypt (cost 10). |
| `name` | string | **Yes** | Max 255 characters. Can be confirmed or edited by user. |
| `phone_number` | string | **Yes** | Max 20 characters. Normalized to `628...` by database trigger. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "User registered successfully",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "bf3f33e19749e42d00ed3cd54bb7ea555d38de710c7da978421721dccd270484",
    "expires_in": 900,
    "user": {
      "id": "8573d7a5-5bf8-457a-8b63-106965aafe64",
      "email": "sarah.core@berbagibitesjogja.com",
      "role": "core",
      "division_id": 2,
      "name": "Sarah Jenkins",
      "phone_number": "6281234567890",
      "is_active": true,
      "email_verified_at": "2026-10-31T08:13:07+07:00"
    }
  }
}
```

##### `400 Bad Request` (Duplicate Constraint Trap / Validation)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "An active user with this email already exists",
  "ref_id": "ref_ac1a1b86",
  "errors": {
    "email": "Email is already registered"
  }
}
```

##### `410 Gone` (Token Replay / Expired)
```json
{
  "status": "error",
  "code": "INVITATION_ALREADY_USED",
  "message": "This invitation has already been accepted",
  "ref_id": "ref_b3fd883c"
}
```

---

### 3.4 Login

Authenticates active registered user credentials and issues an access token (15-minute lifespan) and refresh token (7-day lifespan).

* **Method:** `POST`
* **Path:** `/api/v1/auth/login`
* **Authentication:** None (Public)

#### Request Body
```json
{
  "email": "it@berbagibitesjogja.com",
  "password": "password"
}
```

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Login successful",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "9ea957cd45d81b0d29080fa080b0ad8c857dc45b5c114dfe329fb376ca9660ab",
    "expires_in": 900,
    "user": {
      "id": "00000000-0000-0000-0000-000000000001",
      "email": "it@berbagibitesjogja.com",
      "role": "super",
      "division_id": 1,
      "name": "Tim IT",
      "phone_number": "620000000001",
      "is_active": true
    }
  }
}
```

##### `401 Unauthorized` (Bad Credentials or Inactive)
```json
{
  "status": "error",
  "code": "INVALID_CREDENTIALS",
  "message": "Invalid email or password",
  "ref_id": "ref_a2a590df"
}
```

---

### 3.5 Refresh Token Rotation

Exchanges an active refresh token for a newly rotated refresh token and a new access token. The old refresh token is revoked immediately.

* **Method:** `POST`
* **Path:** `/api/v1/auth/refresh`
* **Authentication:** None (Public)

#### Request Body
```json
{
  "refresh_token": "9ea957cd45d81b0d29080fa080b0ad8c857dc45b5c114dfe329fb376ca9660ab"
}
```

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Token refreshed successfully",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "4102941fa495810284ab91028301928410284192048102948120491820498120",
    "expires_in": 900
  }
}
```

##### `401 Unauthorized` (Revoked or Expired Refresh Token)
```json
{
  "status": "error",
  "code": "INVALID_TOKEN",
  "message": "Invalid or expired refresh token",
  "ref_id": "ref_e819b21a"
}
```

---

### 3.6 Logout

Revokes the supplied refresh token so it cannot be used again to issue access tokens.

* **Method:** `POST`
* **Path:** `/api/v1/auth/logout`
* **Authentication:** None (Public)

#### Request Body
```json
{
  "refresh_token": "4102941fa495810284ab91028301928410284192048102948120491820498120"
}
```

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Logged out successfully"
}
```

---

## 4. Rate Limiting Policy

* **Mechanism**: In-memory token bucket per client IP (`middleware.RateLimit`).
* **Limit**: `RATE_LIMIT` requests per minute (default: `60 req/min`, burst: `60`).
* **Trigger Response**:
  ```http
  HTTP/1.1 429 Too Many Requests
  Retry-After: 1
  X-Request-Id: ref_749bf9d2

  {
    "status": "error",
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Too many requests, please try again later",
    "ref_id": "ref_749bf9d2"
  }
  ```

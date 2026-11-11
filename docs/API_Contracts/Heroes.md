# API Contract: Heroes & Volunteer Management

> **Document Version:** `1.0.0`  
> **Topic Reference:** `010`, `011`  
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
| `X-Request-Id` | Request / Response | String | Correlation ID format `ref_<hex>` (e.g. `ref_b10a28f1`). Propagated or auto-generated. |
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

---

## 2. Endpoints Overview

| Method | Endpoint | Auth | Role Guardrails | Description |
| :--- | :--- | :--- | :--- | :--- |
| `POST` | `/api/v1/heroes/register` | Public | None | Register public hero user; sends 6-digit verification code |
| `POST` | `/api/v1/heroes/verify-email` | Public | None | Verify 6-digit email code; auto-enrolls university domain & issues initial JWTs |
| `POST` | `/api/v1/heroes/resend-verification` | Public | None | Resend 6-digit verification code (60-second cooldown, anti-enumeration) |
| `POST` | `/api/v1/heroes/me` | Bearer JWT | Authenticated User | Convert caller account to hero; auto-enrolls university domain if eligible |
| `GET` | `/api/v1/heroes/me` | Bearer JWT | Authenticated Hero | Retrieve current authenticated user's hero profile and enrolled categories |
| `GET` | `/api/v1/heroes/:id/categories` | Bearer JWT | `super`, `core`, `staff` | Staff lists all beneficiary categories assigned to a hero |
| `POST` | `/api/v1/heroes/:id/categories` | Bearer JWT | `super`, `core`, `staff` | Staff assigns an eligible category (`university` or `kipk`) to a hero |
| `DELETE` | `/api/v1/heroes/:id/categories/:category_id` | Bearer JWT | `super`, `core`, `staff` | Staff unassigns a beneficiary category from a hero |

---

## 3. Endpoint Specifications

### 3.1 Register Hero

Registers a new user with unverified email status and creates an associated 1:1 `heroes` record. Dispatches a 6-digit numeric verification code valid for 15 minutes to the user's email.

* **Method:** `POST`
* **Path:** `/api/v1/heroes/register`
* **Authentication:** None (Public)

#### Request Body
```json
{
  "email": "budi.santoso@mail.ugm.ac.id",
  "password": "SecurePassword123!",
  "name": "Budi Santoso",
  "phone_number": "+62 812-3456-7890"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `email` | string | **Yes** | Valid email address format, max 255 chars. Case-insensitive unique. Must not have an active account or pending staff invitation. |
| `password` | string | **Yes** | Min 8 characters, max 72 characters. Hashed via bcrypt (cost 10). |
| `name` | string | **Yes** | Max 255 characters. |
| `phone_number` | string | No | Max 20 characters. Automatically sanitized to Indonesian standard format (`628...`) by database trigger. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Registration successful. Please verify your email.",
  "data": {
    "email": "budi.santoso@mail.ugm.ac.id",
    "name": "Budi Santoso",
    "is_verified": false
  }
}
```

##### `400 Bad Request` (Existing User Conflict)
```json
{
  "status": "error",
  "code": "VAL_002",
  "message": "An account with this email already exists",
  "ref_id": "ref_8192a01f",
  "errors": {
    "email": "Email address is already registered"
  }
}
```

##### `400 Bad Request` (Pending Staff Invitation Collision)
```json
{
  "status": "error",
  "code": "VAL_003",
  "message": "A pending invitation for this email already exists",
  "ref_id": "ref_902a7b11",
  "errors": {
    "email": "Invitation already pending"
  }
}
```

---

### 3.2 Verify Email

Validates the 6-digit verification code. Upon successful verification:
1. Marks `email_verified_at = NOW()`.
2. Marks the verification code as used and invalidates open codes.
3. Revokes all prior refresh token sessions for security (anti-hijacking).
4. If the email domain matches an active `university` category, automatically enrolls the hero into that category (`hero_categories`).
5. Generates and returns an initial access token and refresh token session.

* **Method:** `POST`
* **Path:** `/api/v1/heroes/verify-email`
* **Authentication:** None (Public)

#### Request Body
```json
{
  "email": "budi.santoso@mail.ugm.ac.id",
  "code": "482019"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `email` | string | **Yes** | Email address belonging to the registered account. |
| `code` | string | **Yes** | Exactly 6 numeric digits. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Email verified successfully",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "4102941fa495810284ab91028301928410284192048102948120491820498120",
    "expires_in": 900,
    "user": {
      "id": "19b48c6f-5bf8-457a-8b63-106965aafe64",
      "email": "budi.santoso@mail.ugm.ac.id",
      "name": "Budi Santoso",
      "phone_number": "6281234567890",
      "is_active": true,
      "is_staff": false,
      "is_hero": true,
      "email_verified_at": "2026-11-11T10:15:00+07:00"
    }
  }
}
```

##### `400 Bad Request` (Invalid Code)
```json
{
  "status": "error",
  "code": "VERIFICATION_CODE_INVALID",
  "message": "Invalid verification code",
  "ref_id": "ref_37fa19b2"
}
```

##### `400 Bad Request` (Expired or Missing Code)
```json
{
  "status": "error",
  "code": "VERIFICATION_CODE_EXPIRED",
  "message": "Verification code is invalid or has expired. Please request a new code.",
  "ref_id": "ref_55c11029"
}
```

##### `429 Too Many Requests` (Exceeded Max Attempts)
```json
{
  "status": "error",
  "code": "VERIFICATION_MAX_ATTEMPTS",
  "message": "Maximum verification attempts exceeded. Please request a new code.",
  "ref_id": "ref_9011af34"
}
```

---

### 3.3 Resend Verification Code

Generates and emails a fresh 6-digit verification code. Enforces a 60-second cooldown between requests. Implements anti-enumeration: returns identical generic `200 OK` responses whether the email exists, is already verified, or is deactivated.

* **Method:** `POST`
* **Path:** `/api/v1/heroes/resend-verification`
* **Authentication:** None (Public)

#### Request Body
```json
{
  "email": "budi.santoso@mail.ugm.ac.id"
}
```

#### Responses

##### `200 OK` (Standard & Anti-Enumeration)
```json
{
  "status": "success",
  "message": "If an account with this email exists and requires verification, a verification code has been sent."
}
```

##### `429 Too Many Requests` (60-second Cooldown Hit)
```json
{
  "status": "error",
  "code": "RESEND_RATE_LIMITED",
  "message": "Please wait 42 seconds before requesting another verification code",
  "ref_id": "ref_aa129841",
  "errors": {
    "retry_after_seconds": 42
  }
}
```

---

### 3.4 Become a Hero

Allows an existing authenticated user (such as a staff member) to enroll as a Hero. Creates a 1:1 `heroes` record if one does not already exist, performs automatic university domain category enrollment if applicable, and returns an updated token session reflecting `is_hero = true`.

* **Method:** `POST`
* **Path:** `/api/v1/heroes/me`
* **Authentication:** Required (`Bearer <JWT>`)

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Hero profile created successfully",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "90ba419041284918294102941092841029481204918204981204918204918204",
    "expires_in": 900,
    "user": {
      "id": "00000000-0000-0000-0000-000000000004",
      "email": "staff.ugm@mail.ugm.ac.id",
      "name": "Staff Hero",
      "phone_number": "620000000004",
      "is_active": true,
      "is_staff": true,
      "role": "staff",
      "division_id": 2,
      "division_name": "Operational",
      "is_hero": true,
      "email_verified_at": "2026-10-27T10:04:53+07:00"
    }
  }
}
```

---

### 3.5 Get My Hero Profile

Retrieves the authenticated hero's profile details and list of assigned beneficiary categories.

* **Method:** `GET`
* **Path:** `/api/v1/heroes/me`
* **Authentication:** Required (`Bearer <JWT>`)

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Hero profile retrieved successfully",
  "data": {
    "id": "19b48c6f-5bf8-457a-8b63-106965aafe64",
    "email": "budi.santoso@mail.ugm.ac.id",
    "name": "Budi Santoso",
    "phone_number": "6281234567890",
    "remind_me_at": null,
    "categories": [
      {
        "id": "f83366d2-9a98-4003-94ae-eb09d79b065c",
        "name": "Universitas Gadjah Mada",
        "variant": "university",
        "is_priority": true
      }
    ]
  }
}
```

##### `404 Not Found` (Caller is not enrolled as a Hero)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Hero profile not found",
  "ref_id": "ref_6819a002"
}
```

---

### 3.6 List Hero Categories (Staff)

Allows staff members to view all categories assigned to a specified hero.

* **Method:** `GET`
* **Path:** `/api/v1/heroes/:id/categories`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Path Parameters
| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | **Yes** | Target hero user UUID. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Hero categories retrieved successfully",
  "data": [
    {
      "id": "f83366d2-9a98-4003-94ae-eb09d79b065c",
      "name": "Universitas Gadjah Mada",
      "variant": "university",
      "is_priority": true
    }
  ]
}
```

##### `404 Not Found` (Hero Does Not Exist)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Hero not found",
  "ref_id": "ref_5cd95d75"
}
```

---

### 3.7 Assign Category to Hero (Staff)

Allows staff members to manually assign a beneficiary category to a hero. Only categories with variants `university` or `kipk` are eligible for hero assignment.

* **Method:** `POST`
* **Path:** `/api/v1/heroes/:id/categories`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Request Body
```json
{
  "category_id": "4cd300ef-2abc-4c5c-a250-d0554cc0ab86"
}
```

| Field | Type | Required | Rules & Constraints |
| :--- | :--- | :--- | :--- |
| `category_id` | UUID string | **Yes** | Must refer to an active beneficiary category with variant `university` or `kipk`. |

#### Responses

##### `201 Created`
```json
{
  "status": "success",
  "message": "Category assigned to hero successfully"
}
```

##### `400 Bad Request` (Ineligible Variant)
```json
{
  "status": "error",
  "code": "VAL_001",
  "message": "Only university or KIP-K priority categories can be assigned to heroes",
  "ref_id": "ref_9011ba24"
}
```

##### `404 Not Found` (Hero or Category Does Not Exist)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Hero or beneficiary category not found",
  "ref_id": "ref_c1029410"
}
```

---

### 3.8 Remove Category from Hero (Staff)

Unassigns a beneficiary category from a hero.

* **Method:** `DELETE`
* **Path:** `/api/v1/heroes/:id/categories/:category_id`
* **Authentication:** Required (`Bearer <JWT>`)
* **Role Permissions:** `super`, `core`, `staff`

#### Path Parameters
| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | UUID | **Yes** | Target hero user UUID. |
| `category_id` | UUID | **Yes** | Beneficiary category UUID to remove. |

#### Responses

##### `200 OK`
```json
{
  "status": "success",
  "message": "Category removed from hero successfully"
}
```

##### `404 Not Found` (Hero Does Not Exist)
```json
{
  "status": "error",
  "code": "NOT_FOUND",
  "message": "Hero not found",
  "ref_id": "ref_c0bcb202"
}
```

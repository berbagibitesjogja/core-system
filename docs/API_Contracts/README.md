# Berbagi Bites Jogja API Contracts

This directory contains the official API specifications, data models, role guardrails, and request/response contracts for the Berbagi Bites Jogja Core System (`/api/v1`).

## Available Contracts

| Domain | File | Description |
| :--- | :--- | :--- |
| **Authentication** | [`Auth.md`](./Auth.md) | Local staff login, refresh token rotation, logout, and self session management |
| **Google OAuth** | [`GoogleOAuth.md`](./GoogleOAuth.md) | Single sign-on for Berbagi Bites Jogja staff and Google identity linking |
| **Staff & Roles** | [`Staff.md`](./Staff.md) | Staff directory, role promotion/demotion matrix, division transfers, and kick/resign governance |
| **Heroes & Volunteers** | [`Heroes.md`](./Heroes.md) | Hero beneficiary registration, university email auto-enrollment, profile, and category assignments |
| **Beneficiary Categories**| [`BeneficiaryCategories.md`](./BeneficiaryCategories.md) | Management of beneficiary criteria, university domain associations, and public visibility |
| **Sponsors** | [`Sponsors.md`](./Sponsors.md) | Sponsor directory, tiers, and logo management |
| **Food Items** | [`FoodItems.md`](./FoodItems.md) | Food inventory, categories, expiration tracking, and stock management |
| **Reimbursements** | [`Reimbursements.md`](./Reimbursements.md) | Operational expense claims, receipt file attachments, and payment approvals |

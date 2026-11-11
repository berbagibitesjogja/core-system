package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
	"github.com/berbagibitesjogja/core-system/internal/mailer"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HeroAuthHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
	mailer  *mailer.Mailer
}

func NewHeroAuthHandler(pool *pgxpool.Pool, cfg *config.Config, m *mailer.Mailer) *HeroAuthHandler {
	return &HeroAuthHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
		mailer:  m,
	}
}

type HeroRegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Name        string `json:"name"`
	PhoneNumber string `json:"phone_number"`
}

type HeroVerifyEmailRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type HeroResendVerificationRequest struct {
	Email string `json:"email"`
}

func (h *HeroAuthHandler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req HeroRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Password = strings.TrimSpace(req.Password)
	req.Name = strings.TrimSpace(req.Name)
	req.PhoneNumber = strings.TrimSpace(req.PhoneNumber)

	fieldErrors := make(map[string]string)
	if req.Email == "" {
		fieldErrors["email"] = "Email is required"
	} else if !auth.IsValidEmail(req.Email) {
		fieldErrors["email"] = "Invalid email format"
	}

	if req.Password == "" {
		fieldErrors["password"] = "Password is required"
	} else if len(req.Password) < 8 {
		fieldErrors["password"] = "Password must be at least 8 characters"
	} else if len(req.Password) > 72 {
		fieldErrors["password"] = "Password cannot exceed 72 characters"
	}

	if req.Name == "" {
		fieldErrors["name"] = "Name is required"
	} else if len(req.Name) > 255 {
		fieldErrors["name"] = "Name cannot exceed 255 characters"
	}

	if len(req.PhoneNumber) > 20 {
		fieldErrors["phone_number"] = "Phone number cannot exceed 20 characters"
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	existingUser, err := h.queries.GetUserByEmail(r.Context(), req.Email)
	if err == nil {
		response.SetError(r.Context(), fmt.Errorf("user with email %s already exists (id: %s)", req.Email, existingUser.ID))
		response.BadRequest(w, r, "VAL_002", "An account with this email already exists", map[string]string{
			"email": "Email address is already registered",
		})
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		response.InternalServerError(w, r, err, "Failed to verify email uniqueness")
		return
	}

	hasPending, err := h.queries.CheckPendingInvitationByEmail(r.Context(), req.Email)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to verify invitation status")
		return
	}
	if hasPending {
		response.SetError(r.Context(), fmt.Errorf("pending invitation exists for %s", req.Email))
		response.BadRequest(w, r, "VAL_003", "A pending invitation for this email already exists", map[string]string{
			"email": "Invitation already pending",
		})
		return
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to secure password")
		return
	}

	var phoneParam pgtype.Text
	if req.PhoneNumber != "" {
		phoneParam = pgtype.Text{String: req.PhoneNumber, Valid: true}
	}

	code, err := auth.GenerateVerificationCode()
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate verification code")
		return
	}

	codeHash := auth.HashToken(code)
	expiryMinutes := h.getVerificationExpiryMinutes()
	expiresAt := time.Now().Add(time.Duration(expiryMinutes) * time.Minute)

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer func() {
		_ = tx.Rollback(r.Context())
	}()

	qtx := h.queries.WithTx(tx)

	newUser, err := qtx.CreateUser(r.Context(), dbgen.CreateUserParams{
		Email:           req.Email,
		Name:            req.Name,
		PasswordHash:    pgtype.Text{String: hashedPassword, Valid: true},
		PhoneNumber:     phoneParam,
		PfpExternalUrl:  pgtype.Text{Valid: false},
		IsActive:        true,
		EmailVerifiedAt: pgtype.Timestamptz{Valid: false},
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create user record")
		return
	}

	_, err = qtx.CreateHero(r.Context(), dbgen.CreateHeroParams{
		UserID:     newUser.ID,
		RemindMeAt: pgtype.Timestamptz{Valid: false},
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create hero profile")
		return
	}

	_, err = qtx.CreateVerificationCode(r.Context(), dbgen.CreateVerificationCodeParams{
		UserID:    newUser.ID,
		CodeHash:  codeHash,
		Purpose:   dbgen.VerificationPurposeEmailVerification,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to record verification code")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit registration transaction")
		return
	}

	if h.mailer != nil {
		recipientName := newUser.Name
		userEmail := newUser.Email
		verificationCode := code
		go func() {
			_ = h.mailer.SendVerificationEmail(userEmail, recipientName, verificationCode, expiryMinutes)
		}()
	}

	response.Success(w, http.StatusCreated, "Registration successful. Please verify your email.", map[string]any{
		"email":       newUser.Email,
		"name":        newUser.Name,
		"is_verified": false,
	})
}

func (h *HeroAuthHandler) VerifyEmailHandler(w http.ResponseWriter, r *http.Request) {
	var req HeroVerifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Code = strings.TrimSpace(req.Code)

	fieldErrors := make(map[string]string)
	if req.Email == "" {
		fieldErrors["email"] = "Email is required"
	}
	if req.Code == "" {
		fieldErrors["code"] = "Verification code is required"
	} else if len(req.Code) != 6 {
		fieldErrors["code"] = "Verification code must be 6 digits"
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	user, err := h.queries.GetUserWithRolesByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), fmt.Errorf("user not found with email %s", req.Email))
			response.BadRequest(w, r, "USER_NOT_FOUND", "No account found with this email", nil)
			return
		}
		response.InternalServerError(w, r, err, "Failed to look up user")
		return
	}

	if user.EmailVerifiedAt.Valid {
		response.ErrorWithRequest(w, r, http.StatusBadRequest, "EMAIL_ALREADY_VERIFIED", "This email address is already verified", nil)
		return
	}

	if !user.IsActive {
		response.SetError(r.Context(), fmt.Errorf("deactivated user %s attempted email verification", user.ID))
		response.Forbidden(w, r, "Account has been deactivated. Please contact support.")
		return
	}

	vcode, err := h.queries.GetLatestActiveVerificationCode(r.Context(), dbgen.GetLatestActiveVerificationCodeParams{
		UserID:  user.ID,
		Purpose: dbgen.VerificationPurposeEmailVerification,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), fmt.Errorf("no active verification code for user %s", user.ID))
			response.BadRequest(w, r, "VERIFICATION_CODE_EXPIRED", "Verification code is invalid or has expired. Please request a new code.", nil)
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve verification code")
		return
	}

	if vcode.Attempts >= 5 {
		response.SetError(r.Context(), fmt.Errorf("verification code %s exceeded max attempts (%d)", vcode.ID, vcode.Attempts))
		response.ErrorWithRequest(w, r, http.StatusTooManyRequests, "VERIFICATION_MAX_ATTEMPTS", "Maximum verification attempts exceeded. Please request a new code.", nil)
		return
	}

	computedHash := auth.HashToken(req.Code)
	if computedHash != vcode.CodeHash {
		_, _ = h.queries.IncrementVerificationAttempts(r.Context(), vcode.ID)
		response.SetError(r.Context(), fmt.Errorf("verification code mismatch for user %s", user.ID))
		response.BadRequest(w, r, "VERIFICATION_CODE_INVALID", "Invalid verification code", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer func() {
		_ = tx.Rollback(r.Context())
	}()

	qtx := h.queries.WithTx(tx)

	_, err = qtx.MarkVerificationCodeUsed(r.Context(), vcode.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to mark verification code as used")
		return
	}

	err = qtx.MarkEmailVerified(r.Context(), user.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to mark email verified")
		return
	}

	_ = qtx.InvalidateOpenVerificationCodes(r.Context(), dbgen.InvalidateOpenVerificationCodesParams{
		UserID:  user.ID,
		Purpose: dbgen.VerificationPurposeEmailVerification,
	})

	if err := qtx.RevokeAllUserRefreshTokens(r.Context(), user.ID); err != nil {
		response.InternalServerError(w, r, err, "Failed to revoke existing sessions")
		return
	}

	emailParts := strings.Split(user.Email, "@")
	if len(emailParts) == 2 {
		domain := strings.ToLower(strings.TrimSpace(emailParts[1]))
		matchedCategory, matchErr := qtx.MatchUniversityCategoryByEmailDomain(r.Context(), domain)
		if matchErr == nil {
			_, _ = qtx.AddHeroCategory(r.Context(), dbgen.AddHeroCategoryParams{
				HeroID:                user.ID,
				BeneficiaryCategoryID: matchedCategory.ID,
			})
		}
	}

	roleStr, divID := ExtractStaffClaims(user.IsStaff, user.StaffRole, user.StaffDivisionID)

	session, err := IssueUserSession(r.Context(), qtx, h.cfg, UserSessionParams{
		UserID:     user.ID,
		Email:      user.Email,
		IsStaff:    user.IsStaff,
		Role:       roleStr,
		DivisionID: divID,
		IsHero:     true,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to issue authentication session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit verification transaction")
		return
	}

	verifiedAtStr := time.Now().Format(time.RFC3339)
	userResp := UserResponseFromRolesByEmail(user)
	userResp.IsHero = true
	userResp.EmailVerifiedAt = &verifiedAtStr

	response.Success(w, http.StatusOK, "Email verified successfully", AuthTokenResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		ExpiresIn:    int64(session.AccessExpiry.Seconds()),
		User:         userResp,
	})
}

func (h *HeroAuthHandler) ResendVerificationHandler(w http.ResponseWriter, r *http.Request) {
	var req HeroResendVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
			"email": "Email is required",
		})
		return
	} else if !auth.IsValidEmail(req.Email) {
		response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
			"email": "Invalid email format",
		})
		return
	}

	user, err := h.queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		response.Success(w, http.StatusOK, "If an account with this email exists and requires verification, a verification code has been sent.", nil)
		return
	}

	if user.EmailVerifiedAt.Valid {
		response.Success(w, http.StatusOK, "If an account with this email exists and requires verification, a verification code has been sent.", nil)
		return
	}

	if !user.IsActive {
		response.SetError(r.Context(), fmt.Errorf("resend verification requested for deactivated account %s (%s)", user.ID, user.Email))
		response.Success(w, http.StatusOK, "If an account with this email exists and requires verification, a verification code has been sent.", nil)
		return
	}

	latestCode, err := h.queries.GetLatestActiveVerificationCode(r.Context(), dbgen.GetLatestActiveVerificationCodeParams{
		UserID:  user.ID,
		Purpose: dbgen.VerificationPurposeEmailVerification,
	})
	if err == nil {
		if time.Since(latestCode.CreatedAt) < 60*time.Second {
			retryAfter := int(60 - time.Since(latestCode.CreatedAt).Seconds())
			response.SetError(r.Context(), fmt.Errorf("resend rate limit hit for user %s (retry after %ds)", user.ID, retryAfter))
			response.ErrorWithRequest(w, r, http.StatusTooManyRequests, "RESEND_RATE_LIMITED", fmt.Sprintf("Please wait %d seconds before requesting another verification code", retryAfter), map[string]any{
				"retry_after_seconds": retryAfter,
			})
			return
		}
	}

	_ = h.queries.InvalidateOpenVerificationCodes(r.Context(), dbgen.InvalidateOpenVerificationCodesParams{
		UserID:  user.ID,
		Purpose: dbgen.VerificationPurposeEmailVerification,
	})

	code, err := auth.GenerateVerificationCode()
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate verification code")
		return
	}

	codeHash := auth.HashToken(code)
	expiryMinutes := h.getVerificationExpiryMinutes()
	expiresAt := time.Now().Add(time.Duration(expiryMinutes) * time.Minute)

	_, err = h.queries.CreateVerificationCode(r.Context(), dbgen.CreateVerificationCodeParams{
		UserID:    user.ID,
		CodeHash:  codeHash,
		Purpose:   dbgen.VerificationPurposeEmailVerification,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate verification code")
		return
	}

	if h.mailer != nil {
		recipientName := user.Name
		userEmail := user.Email
		verificationCode := code
		go func() {
			_ = h.mailer.SendVerificationEmail(userEmail, recipientName, verificationCode, expiryMinutes)
		}()
	}

	response.Success(w, http.StatusOK, "If an account with this email exists and requires verification, a verification code has been sent.", nil)
}

func (h *HeroAuthHandler) getVerificationExpiryMinutes() int {
	if h.cfg != nil && h.cfg.SMTP.ExpiryMinutes > 0 {
		return h.cfg.SMTP.ExpiryMinutes
	}
	return 15
}

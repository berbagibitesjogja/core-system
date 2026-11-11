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
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewAuthHandler(pool *pgxpool.Pool, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type RegisterRequest struct {
	InvitationToken string `json:"invitation_token"`
	Password        string `json:"password"`
	Name            string `json:"name"`
	PhoneNumber     string `json:"phone_number"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type UserResponse struct {
	ID              string  `json:"id"`
	Email           string  `json:"email"`
	Name            string  `json:"name"`
	PhoneNumber     string  `json:"phone_number"`
	IsActive        bool    `json:"is_active"`
	IsStaff         bool    `json:"is_staff"`
	Role            string  `json:"role,omitempty"`
	DivisionID      int32   `json:"division_id,omitempty"`
	DivisionName    string  `json:"division_name,omitempty"`
	IsHero          bool    `json:"is_hero"`
	EmailVerifiedAt *string `json:"email_verified_at,omitempty"`
}

type AuthTokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int64        `json:"expires_in"`
	User         UserResponse `json:"user"`
}

type RefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (h *AuthHandler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.InvitationToken = strings.TrimSpace(req.InvitationToken)
	req.Password = strings.TrimSpace(req.Password)
	req.Name = strings.TrimSpace(req.Name)
	req.PhoneNumber = strings.TrimSpace(req.PhoneNumber)

	fieldErrors := make(map[string]string)
	if req.InvitationToken == "" {
		fieldErrors["invitation_token"] = "Invitation token is required"
	}
	if req.Password == "" {
		fieldErrors["password"] = "Password is required"
	} else if len(req.Password) < 8 {
		fieldErrors["password"] = "Password must be at least 8 characters long"
	} else if len(req.Password) > 72 {
		fieldErrors["password"] = "Password cannot exceed 72 characters"
	}
	if len(req.Name) > 255 {
		fieldErrors["name"] = "Name cannot exceed 255 characters"
	}
	if len(req.PhoneNumber) > 20 {
		fieldErrors["phone_number"] = "Phone number cannot exceed 20 characters"
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	tokenHash := auth.HashToken(req.InvitationToken)
	invitation, err := h.queries.GetInvitationByTokenHash(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), errors.New("invitation token not found in database"))
			response.Forbidden(w, r, "Invalid invitation token")
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify invitation token")
		return
	}

	if invitation.AcceptedAt.Valid {
		response.SetError(r.Context(), fmt.Errorf("invitation token %s already accepted", invitation.ID))
		response.ErrorWithRequest(w, r, http.StatusGone, "INVITATION_ALREADY_USED", "This invitation has already been accepted", nil)
		return
	}

	if time.Now().After(invitation.ExpiresAt) {
		response.SetError(r.Context(), fmt.Errorf("invitation token %s expired at %s", invitation.ID, invitation.ExpiresAt))
		response.ErrorWithRequest(w, r, http.StatusGone, "INVITATION_EXPIRED", "This invitation has expired", nil)
		return
	}

	name := invitation.Email
	if req.Name != "" {
		name = req.Name
	} else if invitation.Name.Valid && invitation.Name.String != "" {
		name = invitation.Name.String
	}

	var phoneStr string
	if req.PhoneNumber != "" {
		phoneStr = req.PhoneNumber
	} else if invitation.PhoneNumber.Valid {
		phoneStr = invitation.PhoneNumber.String
	}

	var phoneParam pgtype.Text
	if phoneStr != "" {
		phoneParam = pgtype.Text{String: phoneStr, Valid: true}
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to secure password")
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

	now := time.Now()
	user, err := qtx.CreateUser(r.Context(), dbgen.CreateUserParams{
		Email:           invitation.Email,
		Name:            name,
		PasswordHash:    pgtype.Text{String: hashedPassword, Valid: true},
		PhoneNumber:     phoneParam,
		PfpExternalUrl:  pgtype.Text{Valid: false},
		IsActive:        true,
		EmailVerifiedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create user record")
		return
	}

	_, err = qtx.CreateStaff(r.Context(), dbgen.CreateStaffParams{
		UserID:     user.ID,
		DivisionID: invitation.DivisionID,
		Role:       invitation.Role,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to assign staff record")
		return
	}

	_, err = qtx.MarkInvitationAccepted(r.Context(), invitation.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to update invitation status")
		return
	}

	session, err := IssueUserSession(r.Context(), qtx, h.cfg, UserSessionParams{
		UserID:     user.ID,
		Email:      user.Email,
		IsStaff:    true,
		Role:       string(invitation.Role),
		DivisionID: invitation.DivisionID,
		IsHero:     false,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to issue authentication session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to complete user registration")
		return
	}

	response.Success(w, http.StatusCreated, "User registered successfully", AuthTokenResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		ExpiresIn:    int64(session.AccessExpiry.Seconds()),
		User: ToUserResponse(
			user.ID,
			user.Email,
			user.Name,
			user.PhoneNumber,
			user.IsActive,
			true,
			string(invitation.Role),
			invitation.DivisionID,
			invitation.DivisionName,
			false,
			user.EmailVerifiedAt,
		),
	})
}

func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Password = strings.TrimSpace(req.Password)

	fieldErrors := make(map[string]string)
	if req.Email == "" {
		fieldErrors["email"] = "Email is required"
	} else if !auth.IsValidEmail(req.Email) {
		fieldErrors["email"] = "Invalid email format"
	}
	if req.Password == "" {
		fieldErrors["password"] = "Password is required"
	}
	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	user, err := h.queries.GetUserWithRolesByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), fmt.Errorf("user not found with email %s", req.Email))
			response.Unauthorized(w, r, "Invalid email or password")
			return
		}
		response.InternalServerError(w, r, err, "Failed to look up user")
		return
	}

	if !user.PasswordHash.Valid || !auth.CheckPassword(req.Password, user.PasswordHash.String) {
		response.SetError(r.Context(), fmt.Errorf("invalid password for user %s (%s)", user.ID, user.Email))
		response.Unauthorized(w, r, "Invalid email or password")
		return
	}

	if !user.IsActive {
		response.SetError(r.Context(), fmt.Errorf("account %s is deactivated", user.ID))
		response.Forbidden(w, r, "Account has been deactivated. Please contact support.")
		return
	}

	if !user.EmailVerifiedAt.Valid {
		response.SetError(r.Context(), fmt.Errorf("unverified user %s attempted login", user.ID))
		response.ErrorWithRequest(w, r, http.StatusForbidden, "EMAIL_NOT_VERIFIED", "Please verify your email address before logging in.", nil)
		return
	}

	roleStr, divID := ExtractStaffClaims(user.IsStaff, user.StaffRole, user.StaffDivisionID)

	session, err := IssueUserSession(r.Context(), h.queries, h.cfg, UserSessionParams{
		UserID:     user.ID,
		Email:      user.Email,
		IsStaff:    user.IsStaff,
		Role:       roleStr,
		DivisionID: divID,
		IsHero:     user.IsHero,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to issue authentication session")
		return
	}

	response.Success(w, http.StatusOK, "Login successful", AuthTokenResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		ExpiresIn:    int64(session.AccessExpiry.Seconds()),
		User:         UserResponseFromRolesByEmail(user),
	})
}

func (h *AuthHandler) RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	if req.RefreshToken == "" {
		response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
			"refresh_token": "Refresh token is required",
		})
		return
	}

	tokenHash := auth.HashToken(req.RefreshToken)
	storedToken, err := h.queries.GetRefreshTokenByHash(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), errors.New("refresh token not found in database"))
			response.Unauthorized(w, r, "Invalid refresh token")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve refresh token")
		return
	}

	if storedToken.RevokedAt.Valid {
		response.SetError(r.Context(), fmt.Errorf("refresh token already revoked at %s", storedToken.RevokedAt.Time))
		response.Unauthorized(w, r, "Refresh token has been revoked")
		return
	}

	now := time.Now()
	if now.After(storedToken.ExpiresAt) {
		response.SetError(r.Context(), fmt.Errorf("refresh token expired at %s", storedToken.ExpiresAt))
		response.Unauthorized(w, r, "Refresh token has expired")
		return
	}

	user, err := h.queries.GetUserWithRolesByID(r.Context(), storedToken.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), fmt.Errorf("user %s associated with refresh token no longer exists", storedToken.UserID))
			response.Unauthorized(w, r, "User account no longer exists")
			return
		}
		response.InternalServerError(w, r, err, "Failed to look up user account")
		return
	}

	if !user.IsActive {
		response.SetError(r.Context(), fmt.Errorf("account %s associated with refresh token is deactivated", user.ID))
		response.Forbidden(w, r, "Account has been deactivated")
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to initiate token rotation")
		return
	}
	defer func() {
		_ = tx.Rollback(r.Context())
	}()

	qtx := h.queries.WithTx(tx)

	_, err = qtx.RevokeRefreshToken(r.Context(), tokenHash)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to revoke active token")
		return
	}

	roleStr, divID := ExtractStaffClaims(user.IsStaff, user.StaffRole, user.StaffDivisionID)

	session, err := IssueUserSession(r.Context(), qtx, h.cfg, UserSessionParams{
		UserID:     user.ID,
		Email:      user.Email,
		IsStaff:    user.IsStaff,
		Role:       roleStr,
		DivisionID: divID,
		IsHero:     user.IsHero,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to rotate authentication session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to finalize token rotation")
		return
	}

	response.Success(w, http.StatusOK, "Token refreshed successfully", RefreshTokenResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		ExpiresIn:    int64(session.AccessExpiry.Seconds()),
	})
}

func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	if req.RefreshToken == "" {
		response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
			"refresh_token": "Refresh token is required",
		})
		return
	}

	tokenHash := auth.HashToken(req.RefreshToken)
	_, err := h.queries.RevokeRefreshToken(r.Context(), tokenHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.InternalServerError(w, r, err, "Failed to revoke token")
		return
	}

	response.Success(w, http.StatusOK, "Logged out successfully", nil)
}

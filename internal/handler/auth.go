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
	Role            string  `json:"role"`
	DivisionID      int32   `json:"division_id"`
	Name            string  `json:"name"`
	PhoneNumber     string  `json:"phone_number"`
	IsActive        bool    `json:"is_active"`
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
		fieldErrors["password"] = "Password must be at least 8 characters"
	} else if len(req.Password) > 72 {
		fieldErrors["password"] = "Password cannot exceed 72 characters"
	}
	if req.Name == "" {
		fieldErrors["name"] = "Name is required"
	} else if len(req.Name) > 255 {
		fieldErrors["name"] = "Name cannot exceed 255 characters"
	}
	if req.PhoneNumber == "" {
		fieldErrors["phone_number"] = "Phone number is required"
	} else if len(req.PhoneNumber) > 20 {
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
			response.SetError(r.Context(), errors.New("invitation token not found or invalid"))
			response.Forbidden(w, r, "Invalid invitation token")
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify invitation")
		return
	}

	if invitation.AcceptedAt.Valid {
		response.SetError(r.Context(), fmt.Errorf("invitation %s already accepted at %s", invitation.ID, invitation.AcceptedAt.Time))
		response.ErrorWithRequest(w, r, http.StatusGone, "INVITATION_ALREADY_USED", "This invitation has already been accepted", nil)
		return
	}

	if time.Now().After(invitation.ExpiresAt) {
		response.SetError(r.Context(), fmt.Errorf("invitation %s expired at %s", invitation.ID, invitation.ExpiresAt))
		response.ErrorWithRequest(w, r, http.StatusGone, "INVITATION_EXPIRED", "This invitation has expired", nil)
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to process password")
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to initiate transaction")
		return
	}
	defer func() {
		_ = tx.Rollback(r.Context())
	}()

	qtx := h.queries.WithTx(tx)

	now := time.Now()
	verifiedAt := pgtype.Timestamptz{Time: now, Valid: true}

	user, err := qtx.CreateUser(r.Context(), dbgen.CreateUserParams{
		DivisionID:      invitation.DivisionID,
		Email:           invitation.Email,
		PasswordHash:    pgtype.Text{String: passwordHash, Valid: true},
		Role:            invitation.Role,
		Name:            req.Name,
		PhoneNumber:     pgtype.Text{String: req.PhoneNumber, Valid: req.PhoneNumber != ""},
		IsActive:        true,
		EmailVerifiedAt: verifiedAt,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create user record")
		return
	}

	_, err = qtx.MarkInvitationAccepted(r.Context(), invitation.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to update invitation status")
		return
	}

	accessExpiry := h.getAccessExpiry()
	refreshExpiry := h.getRefreshExpiry()

	accessToken, err := auth.SignAccessToken(user.ID, user.Email, string(user.Role), user.DivisionID, h.cfg.AppConfig.JWTSecret, accessExpiry)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate access token")
		return
	}

	rawRefreshToken, refreshTokenHash, err := auth.GenerateSecureToken()
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate refresh token")
		return
	}

	_, err = qtx.CreateRefreshToken(r.Context(), dbgen.CreateRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: refreshTokenHash,
		ExpiresAt: now.Add(refreshExpiry),
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to store refresh token")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit registration")
		return
	}

	var verifiedAtStr *string
	if user.EmailVerifiedAt.Valid {
		formatted := user.EmailVerifiedAt.Time.Format(time.RFC3339)
		verifiedAtStr = &formatted
	}

	response.Success(w, http.StatusCreated, "User registered successfully", AuthTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
		ExpiresIn:    int64(accessExpiry.Seconds()),
		User: UserResponse{
			ID:              user.ID.String(),
			Email:           user.Email,
			Role:            string(user.Role),
			DivisionID:      user.DivisionID,
			Name:            user.Name,
			PhoneNumber:     user.PhoneNumber.String,
			IsActive:        user.IsActive,
			EmailVerifiedAt: verifiedAtStr,
		},
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
	}
	if req.Password == "" {
		fieldErrors["password"] = "Password is required"
	}
	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	user, err := h.queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), fmt.Errorf("user not found with email %s", req.Email))
			response.Unauthorized(w, r, "Invalid email or password")
			return
		}
		response.InternalServerError(w, r, err, "Failed to look up user")
		return
	}

	if !user.IsActive {
		response.SetError(r.Context(), fmt.Errorf("account %s is deactivated", user.ID))
		response.Forbidden(w, r, "Account has been deactivated. Please contact support.")
		return
	}

	if !user.PasswordHash.Valid || !auth.CheckPassword(req.Password, user.PasswordHash.String) {
		response.SetError(r.Context(), fmt.Errorf("invalid password for user %s (%s)", user.ID, user.Email))
		response.Unauthorized(w, r, "Invalid email or password")
		return
	}

	accessExpiry := h.getAccessExpiry()
	refreshExpiry := h.getRefreshExpiry()

	accessToken, err := auth.SignAccessToken(user.ID, user.Email, string(user.Role), user.DivisionID, h.cfg.AppConfig.JWTSecret, accessExpiry)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate access token")
		return
	}

	rawRefreshToken, refreshTokenHash, err := auth.GenerateSecureToken()
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate refresh token")
		return
	}

	now := time.Now()
	_, err = h.queries.CreateRefreshToken(r.Context(), dbgen.CreateRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: refreshTokenHash,
		ExpiresAt: now.Add(refreshExpiry),
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to record refresh token")
		return
	}

	var verifiedAtStr *string
	if user.EmailVerifiedAt.Valid {
		formatted := user.EmailVerifiedAt.Time.Format(time.RFC3339)
		verifiedAtStr = &formatted
	}

	response.Success(w, http.StatusOK, "Login successful", AuthTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
		ExpiresIn:    int64(accessExpiry.Seconds()),
		User: UserResponse{
			ID:              user.ID.String(),
			Email:           user.Email,
			Role:            string(user.Role),
			DivisionID:      user.DivisionID,
			Name:            user.Name,
			PhoneNumber:     user.PhoneNumber.String,
			IsActive:        user.IsActive,
			EmailVerifiedAt: verifiedAtStr,
		},
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

	user, err := h.queries.GetUserByID(r.Context(), storedToken.UserID)
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

	accessExpiry := h.getAccessExpiry()
	refreshExpiry := h.getRefreshExpiry()

	newAccessToken, err := auth.SignAccessToken(user.ID, user.Email, string(user.Role), user.DivisionID, h.cfg.AppConfig.JWTSecret, accessExpiry)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate new access token")
		return
	}

	newRawRefreshToken, newRefreshTokenHash, err := auth.GenerateSecureToken()
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate new refresh token")
		return
	}

	_, err = qtx.CreateRefreshToken(r.Context(), dbgen.CreateRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: newRefreshTokenHash,
		ExpiresAt: now.Add(refreshExpiry),
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to record new refresh token")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to finalize token rotation")
		return
	}

	response.Success(w, http.StatusOK, "Token refreshed successfully", RefreshTokenResponse{
		AccessToken:  newAccessToken,
		RefreshToken: newRawRefreshToken,
		ExpiresIn:    int64(accessExpiry.Seconds()),
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

func (h *AuthHandler) getAccessExpiry() time.Duration {
	if h.cfg != nil && h.cfg.AppConfig.JWTAccessExpiry > 0 {
		return h.cfg.AppConfig.JWTAccessExpiry
	}
	return 15 * time.Minute
}

func (h *AuthHandler) getRefreshExpiry() time.Duration {
	if h.cfg != nil && h.cfg.AppConfig.JWTRefreshExpiry > 0 {
		return h.cfg.AppConfig.JWTRefreshExpiry
	}
	return 7 * 24 * time.Hour
}

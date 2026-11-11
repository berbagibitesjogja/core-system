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
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type InvitationHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
	mailer  *mailer.Mailer
}

func NewInvitationHandler(pool *pgxpool.Pool, cfg *config.Config, m *mailer.Mailer) *InvitationHandler {
	return &InvitationHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
		mailer:  m,
	}
}

type CreateInvitationRequest struct {
	Email       string `json:"email"`
	Role        string `json:"role"`
	DivisionID  int32  `json:"division_id"`
	Name        string `json:"name"`
	PhoneNumber string `json:"phone_number"`
}

type InvitationResponse struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	DivisionID   int32     `json:"division_id"`
	DivisionName string    `json:"division_name,omitempty"`
	Name         *string   `json:"name,omitempty"`
	PhoneNumber  *string   `json:"phone_number,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Token        string    `json:"token,omitempty"`
	InviteLink   string    `json:"invite_link,omitempty"`
}

type ValidateInvitationResponse struct {
	Valid        bool    `json:"valid"`
	Email        string  `json:"email"`
	Role         string  `json:"role"`
	DivisionID   int32   `json:"division_id"`
	DivisionName string  `json:"division_name,omitempty"`
	Name         *string `json:"name,omitempty"`
	PhoneNumber  *string `json:"phone_number,omitempty"`
	ExpiresAt    string  `json:"expires_at"`
	IsExpired    bool    `json:"is_expired"`
	IsAccepted   bool    `json:"is_accepted"`
}

type AcceptInvitationRequest struct {
	Token string `json:"token"`
}

func (h *InvitationHandler) CreateInvitationHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Role = strings.TrimSpace(strings.ToLower(req.Role))
	req.Name = strings.TrimSpace(req.Name)
	req.PhoneNumber = strings.TrimSpace(req.PhoneNumber)

	fieldErrors := make(map[string]string)
	if req.Email == "" {
		fieldErrors["email"] = "Email is required"
	} else if !auth.IsValidEmail(req.Email) {
		fieldErrors["email"] = "Invalid email format"
	}

	var division dbgen.GetDivisionByIDRow
	if req.DivisionID <= 0 {
		fieldErrors["division_id"] = "Valid division_id is required"
	} else {
		d, err := h.queries.GetDivisionByID(r.Context(), req.DivisionID)
		if err != nil {
			fieldErrors["division_id"] = "Division not found"
		} else {
			division = d
		}
	}

	if len(req.Name) > 255 {
		fieldErrors["name"] = "Name cannot exceed 255 characters"
	}
	if len(req.PhoneNumber) > 20 {
		fieldErrors["phone_number"] = "Phone number cannot exceed 20 characters"
	}

	var targetRole dbgen.UserRole
	switch req.Role {
	case string(dbgen.UserRoleSuper):
		targetRole = dbgen.UserRoleSuper
	case string(dbgen.UserRoleCore):
		targetRole = dbgen.UserRoleCore
	case string(dbgen.UserRoleStaff):
		targetRole = dbgen.UserRoleStaff
	default:
		fieldErrors["role"] = "Role must be 'super', 'core', or 'staff'"
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	callerRole := claims.Role
	switch callerRole {
	case string(dbgen.UserRoleSuper):
	case string(dbgen.UserRoleCore):
		if targetRole != dbgen.UserRoleStaff {
			response.SetError(r.Context(), fmt.Errorf("role violation: core user %s attempted to invite role %s", claims.Email, targetRole))
			response.Forbidden(w, r, "Core role can only invite Staff members")
			return
		}
	default:
		response.SetError(r.Context(), fmt.Errorf("role violation: staff user %s attempted to create invitation", claims.Email))
		response.Forbidden(w, r, "Staff members are not permitted to invite users")
		return
	}

	existingUser, err := h.queries.GetUserWithRolesByEmail(r.Context(), req.Email)
	if err == nil {
		if existingUser.IsStaff {
			response.BadRequest(w, r, "VAL_002", "An active staff user with this email already exists", map[string]string{
				"email": "Email is already registered as staff",
			})
			return
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		response.InternalServerError(w, r, err, "Failed to verify email availability")
		return
	}

	hasPending, err := h.queries.CheckPendingInvitationByEmail(r.Context(), req.Email)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check existing invitations")
		return
	}
	if hasPending {
		response.SetError(r.Context(), fmt.Errorf("pending invitation already exists for email %s", req.Email))
		response.BadRequest(w, r, "VAL_003", "A pending invitation for this email already exists", map[string]string{
			"email": "Invitation already pending",
		})
		return
	}

	rawToken, tokenHash, err := auth.GenerateSecureToken()
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate invitation token")
		return
	}

	expiresAt := time.Now().Add(48 * time.Hour)

	var nameParam pgtype.Text
	if req.Name != "" {
		nameParam = pgtype.Text{String: req.Name, Valid: true}
	}

	var phoneParam pgtype.Text
	if req.PhoneNumber != "" {
		phoneParam = pgtype.Text{String: req.PhoneNumber, Valid: true}
	}

	invitation, err := h.queries.CreateInvitation(r.Context(), dbgen.CreateInvitationParams{
		InvitedBy:   claims.UserID,
		Email:       req.Email,
		Role:        targetRole,
		DivisionID:  req.DivisionID,
		Name:        nameParam,
		PhoneNumber: phoneParam,
		TokenHash:   tokenHash,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create invitation record")
		return
	}

	baseURL := ""
	if h.cfg != nil {
		baseURL = strings.TrimRight(h.cfg.AppConfig.FrontendURL, "/")
	}
	inviteLink := fmt.Sprintf("%s/register?token=%s", baseURL, rawToken)

	divisionName := division.Name
	if h.mailer != nil {
		recipientName := req.Name
		if recipientName == "" {
			recipientName = req.Email
		}
		inviterName := claims.Email
		go func() {
			_ = h.mailer.SendInvitationEmail(req.Email, recipientName, inviterName, string(targetRole), divisionName, inviteLink, 48)
		}()
	}

	var respName *string
	if invitation.Name.Valid {
		respName = &invitation.Name.String
	}
	var respPhone *string
	if invitation.PhoneNumber.Valid {
		respPhone = &invitation.PhoneNumber.String
	}

	response.Success(w, http.StatusCreated, "Invitation created successfully", InvitationResponse{
		ID:           invitation.ID.String(),
		Email:        invitation.Email,
		Role:         string(invitation.Role),
		DivisionID:   invitation.DivisionID,
		DivisionName: divisionName,
		Name:         respName,
		PhoneNumber:  respPhone,
		ExpiresAt:    invitation.ExpiresAt,
		Token:        rawToken,
		InviteLink:   inviteLink,
	})
}

func (h *InvitationHandler) ValidateInvitationHandler(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if rawToken == "" {
		response.BadRequest(w, r, "VAL_001", "Token parameter is required", nil)
		return
	}

	tokenHash := auth.HashToken(rawToken)
	invitation, err := h.queries.GetInvitationByTokenHash(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.SetError(r.Context(), errors.New("invitation token not found or invalid"))
			response.NotFound(w, r, "Invitation not found or invalid")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve invitation")
		return
	}

	isExpired := time.Now().After(invitation.ExpiresAt)
	isAccepted := invitation.AcceptedAt.Valid

	if isExpired || isAccepted {
		msg := "Invitation is no longer valid"
		if isAccepted {
			msg = "Invitation has already been accepted"
		} else if isExpired {
			msg = "Invitation has expired"
		}
		response.SetError(r.Context(), fmt.Errorf("invitation is invalid: is_expired=%v is_accepted=%v", isExpired, isAccepted))
		response.ErrorWithRequest(w, r, http.StatusGone, "INVITATION_INVALID", msg, map[string]any{
			"is_expired":  isExpired,
			"is_accepted": isAccepted,
		})
		return
	}

	var respName *string
	if invitation.Name.Valid {
		respName = &invitation.Name.String
	}
	var respPhone *string
	if invitation.PhoneNumber.Valid {
		respPhone = &invitation.PhoneNumber.String
	}

	response.Success(w, http.StatusOK, "Invitation is valid", ValidateInvitationResponse{
		Valid:        true,
		Email:        invitation.Email,
		Role:         string(invitation.Role),
		DivisionID:   invitation.DivisionID,
		DivisionName: invitation.DivisionName,
		Name:         respName,
		PhoneNumber:  respPhone,
		ExpiresAt:    invitation.ExpiresAt.Format(time.RFC3339),
		IsExpired:    false,
		IsAccepted:   false,
	})
}

func (h *InvitationHandler) AcceptInvitationHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	var req AcceptInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
			"token": "Token is required",
		})
		return
	}

	tokenHash := auth.HashToken(req.Token)
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

	if strings.ToLower(strings.TrimSpace(invitation.Email)) != strings.ToLower(strings.TrimSpace(claims.Email)) {
		response.SetError(r.Context(), fmt.Errorf("email mismatch: invitation email %s != caller email %s", invitation.Email, claims.Email))
		response.Forbidden(w, r, "This invitation was issued to a different email address")
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

	_, staffErr := qtx.GetStaffByUserID(r.Context(), claims.UserID)
	if staffErr == nil {
		_, err = qtx.UpdateStaffRoleAndDivision(r.Context(), dbgen.UpdateStaffRoleAndDivisionParams{
			UserID:     claims.UserID,
			Role:       invitation.Role,
			DivisionID: invitation.DivisionID,
		})
	} else {
		_, err = qtx.CreateStaff(r.Context(), dbgen.CreateStaffParams{
			UserID:     claims.UserID,
			DivisionID: invitation.DivisionID,
			Role:       invitation.Role,
		})
	}
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to assign staff record")
		return
	}

	_, err = qtx.MarkInvitationAccepted(r.Context(), invitation.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to mark invitation accepted")
		return
	}

	if err := qtx.RevokeAllUserRefreshTokens(r.Context(), claims.UserID); err != nil {
		response.InternalServerError(w, r, err, "Failed to revoke existing sessions")
		return
	}

	user, err := qtx.GetUserWithRolesByID(r.Context(), claims.UserID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to reload user account")
		return
	}

	session, err := IssueUserSession(r.Context(), qtx, h.cfg, UserSessionParams{
		UserID:     user.ID,
		Email:      user.Email,
		IsStaff:    true,
		Role:       string(invitation.Role),
		DivisionID: invitation.DivisionID,
		IsHero:     user.IsHero,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to issue updated authentication session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit invitation acceptance")
		return
	}

	response.Success(w, http.StatusOK, "Invitation accepted successfully", AuthTokenResponse{
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
			user.IsHero,
			user.EmailVerifiedAt,
		),
	})
}

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
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	DivisionID  int32     `json:"division_id"`
	Name        *string   `json:"name,omitempty"`
	PhoneNumber *string   `json:"phone_number,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	Token       string    `json:"token,omitempty"`
	InviteLink  string    `json:"invite_link,omitempty"`
}

func (h *InvitationHandler) CreateInvitationHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.SetError(r.Context(), errors.New("unauthorized: missing claims in context"))
		response.Unauthorized(w, r, "Unauthorized")
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
	} else if !strings.Contains(req.Email, "@") {
		fieldErrors["email"] = "Invalid email format"
	}

	if req.DivisionID <= 0 {
		fieldErrors["division_id"] = "Valid division_id is required"
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

	emailExists, err := h.queries.CheckEmailExists(r.Context(), req.Email)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to verify email availability")
		return
	}
	if emailExists {
		response.SetError(r.Context(), fmt.Errorf("active user already exists with email %s", req.Email))
		response.BadRequest(w, r, "VAL_002", "An active user with this email already exists", map[string]string{
			"email": "Email is already registered",
		})
		return
	}

	pendingExists, err := h.queries.CheckPendingInvitationByEmail(r.Context(), req.Email)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check existing invitations")
		return
	}
	if pendingExists {
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

	baseURL := "http://localhost:8080"
	if h.cfg != nil && h.cfg.AppConfig.FrontendURL != "" {
		baseURL = h.cfg.AppConfig.FrontendURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	inviteLink := fmt.Sprintf("%s/register?token=%s", baseURL, rawToken)

	if h.mailer != nil {
		recipientName := req.Name
		if recipientName == "" {
			recipientName = req.Email
		}
		inviterName := claims.Email
		divisionName := fmt.Sprintf("Division %d", req.DivisionID)
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
		ID:          invitation.ID.String(),
		Email:       invitation.Email,
		Role:        string(invitation.Role),
		DivisionID:  invitation.DivisionID,
		Name:        respName,
		PhoneNumber: respPhone,
		ExpiresAt:   invitation.ExpiresAt,
		Token:       rawToken,
		InviteLink:  inviteLink,
	})
}

type ValidateInvitationResponse struct {
	Valid       bool    `json:"valid"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	DivisionID  int32   `json:"division_id"`
	Name        *string `json:"name,omitempty"`
	PhoneNumber *string `json:"phone_number,omitempty"`
	ExpiresAt   string  `json:"expires_at"`
	IsExpired   bool    `json:"is_expired"`
	IsAccepted  bool    `json:"is_accepted"`
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
		Valid:       true,
		Email:       invitation.Email,
		Role:        string(invitation.Role),
		DivisionID:  invitation.DivisionID,
		Name:        respName,
		PhoneNumber: respPhone,
		ExpiresAt:   invitation.ExpiresAt.Format(time.RFC3339),
		IsExpired:   false,
		IsAccepted:  false,
	})
}

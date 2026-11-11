package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StaffHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewStaffHandler(pool *pgxpool.Pool, cfg *config.Config) *StaffHandler {
	return &StaffHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type StaffMemberResponse struct {
	UserID         string  `json:"user_id"`
	Email          string  `json:"email"`
	Name           string  `json:"name"`
	PhoneNumber    *string `json:"phone_number,omitempty"`
	ProfilePic     *string `json:"profile_pic,omitempty"`
	PfpExternalURL *string `json:"pfp_external_url,omitempty"`
	IsActive       bool    `json:"is_active"`
	DivisionID     int32   `json:"division_id"`
	DivisionName   string  `json:"division_name"`
	Role           string  `json:"role"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func (h *StaffHandler) ListStaffHandler(w http.ResponseWriter, r *http.Request) {
	page := 1
	limit := 20

	query := r.URL.Query()
	if pageStr := query.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if limitStr := query.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
	}
	offset := (page - 1) * limit

	var divParam pgtype.Int4
	if divStr := strings.TrimSpace(query.Get("division_id")); divStr != "" {
		if divID, err := strconv.Atoi(divStr); err == nil && divID > 0 {
			divParam = pgtype.Int4{Int32: int32(divID), Valid: true}
		} else {
			response.BadRequest(w, r, "VAL_001", "Invalid division_id filter", nil)
			return
		}
	}

	var roleParam dbgen.NullUserRole
	if roleStr := strings.TrimSpace(strings.ToLower(query.Get("role"))); roleStr != "" {
		switch roleStr {
		case string(dbgen.UserRoleSuper):
			roleParam = dbgen.NullUserRole{UserRole: dbgen.UserRoleSuper, Valid: true}
		case string(dbgen.UserRoleCore):
			roleParam = dbgen.NullUserRole{UserRole: dbgen.UserRoleCore, Valid: true}
		case string(dbgen.UserRoleStaff):
			roleParam = dbgen.NullUserRole{UserRole: dbgen.UserRoleStaff, Valid: true}
		default:
			response.BadRequest(w, r, "VAL_001", "Invalid role filter. Allowed: super, core, staff", nil)
			return
		}
	}

	var searchParam pgtype.Text
	if searchStr := strings.TrimSpace(query.Get("search")); searchStr != "" {
		searchParam = pgtype.Text{String: searchStr, Valid: true}
	}

	total, err := h.queries.CountStaff(r.Context(), dbgen.CountStaffParams{
		DivisionID: divParam,
		Role:       roleParam,
		Search:     searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count staff members")
		return
	}

	rows, err := h.queries.ListStaff(r.Context(), dbgen.ListStaffParams{
		Limit:      int32(limit),
		Offset:     int32(offset),
		DivisionID: divParam,
		Role:       roleParam,
		Search:     searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve staff list")
		return
	}

	items := make([]StaffMemberResponse, 0, len(rows))
	for _, row := range rows {
		var phone *string
		if row.PhoneNumber.Valid {
			phone = &row.PhoneNumber.String
		}
		var pic *string
		if row.ProfilePic.Valid {
			s := uuid.UUID(row.ProfilePic.Bytes).String()
			pic = &s
		}
		var pfpExt *string
		if row.PfpExternalUrl.Valid {
			pfpExt = &row.PfpExternalUrl.String
		}

		items = append(items, StaffMemberResponse{
			UserID:         row.UserID.String(),
			Email:          row.Email,
			Name:           row.Name,
			PhoneNumber:    phone,
			ProfilePic:     pic,
			PfpExternalURL: pfpExt,
			IsActive:       row.IsActive,
			DivisionID:     row.DivisionID,
			DivisionName:   row.DivisionName,
			Role:           string(row.Role),
			CreatedAt:      row.CreatedAt.Format(time.RFC3339),
			UpdatedAt:      row.UpdatedAt.Format(time.RFC3339),
		})
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}

	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(total),
		TotalPages: totalPages,
	}

	response.PaginatedSuccess(w, http.StatusOK, "Staff members retrieved successfully", items, meta)
}

func (h *StaffHandler) GetStaffHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimSpace(r.PathValue("id"))
	if idStr == "" {
		response.BadRequest(w, r, "VAL_001", "Staff ID is required", nil)
		return
	}

	var targetID uuid.UUID
	if idStr == "me" {
		claims, ok := middleware.GetClaims(r.Context())
		if !ok || claims == nil {
			response.Unauthorized(w, r, "Authentication required")
			return
		}
		targetID = claims.UserID
	} else {
		parsed, err := uuid.Parse(idStr)
		if err != nil {
			response.BadRequest(w, r, "VAL_001", "Invalid staff user ID format", nil)
			return
		}
		targetID = parsed
	}

	row, err := h.queries.GetStaffDetailsByUserID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Staff member not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve staff member")
		return
	}

	var phone *string
	if row.PhoneNumber.Valid {
		phone = &row.PhoneNumber.String
	}
	var pic *string
	if row.ProfilePic.Valid {
		s := uuid.UUID(row.ProfilePic.Bytes).String()
		pic = &s
	}
	var pfpExt *string
	if row.PfpExternalUrl.Valid {
		pfpExt = &row.PfpExternalUrl.String
	}

	response.Success(w, http.StatusOK, "Staff member retrieved successfully", StaffMemberResponse{
		UserID:         row.UserID.String(),
		Email:          row.Email,
		Name:           row.Name,
		PhoneNumber:    phone,
		ProfilePic:     pic,
		PfpExternalURL: pfpExt,
		IsActive:       row.IsActive,
		DivisionID:     row.DivisionID,
		DivisionName:   row.DivisionName,
		Role:           string(row.Role),
		CreatedAt:      row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Format(time.RFC3339),
	})
}

func (h *StaffHandler) SelfDemoteStaffHandler(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("id", "me")
	h.UpdateStaffHandler(w, r)
}

func (h *StaffHandler) UpdateStaffHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	idStr := strings.TrimSpace(r.PathValue("id"))
	if idStr == "" && (strings.HasSuffix(r.URL.Path, "/me") || r.URL.Path == "/me") {
		idStr = "me"
	}
	if idStr == "" {
		response.BadRequest(w, r, "VAL_001", "Staff ID is required", nil)
		return
	}

	var targetID uuid.UUID
	if idStr == "me" {
		targetID = claims.UserID
	} else {
		parsed, err := uuid.Parse(idStr)
		if err != nil {
			response.BadRequest(w, r, "VAL_001", "Invalid staff user ID format", nil)
			return
		}
		targetID = parsed
	}

	existingStaff, err := h.queries.GetStaffByUserID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Staff member not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lookup staff member")
		return
	}

	var rawMap map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMap); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	if len(rawMap) == 0 {
		response.BadRequest(w, r, "VAL_001", "No fields provided to update", nil)
		return
	}

	finalRole := existingStaff.Role
	finalDivisionID := existingStaff.DivisionID
	isSelf := (claims.UserID == targetID)

	if rawDiv, exists := rawMap["division_id"]; exists {
		var newDivID int32
		if err := json.Unmarshal(rawDiv, &newDivID); err != nil {
			response.BadRequest(w, r, "VAL_001", "division_id must be a valid integer", nil)
			return
		}

		if newDivID != existingStaff.DivisionID {
			if claims.Role != string(dbgen.UserRoleSuper) {
				response.SetError(r.Context(), fmt.Errorf("role violation: user %s (%s) attempted division reassignment", claims.Email, claims.Role))
				response.Forbidden(w, r, "Only Super Administrators can reassign staff divisions")
				return
			}

			division, err := h.queries.GetDivisionByID(r.Context(), newDivID)
			if err != nil {
				response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
					"division_id": "Division not found",
				})
				return
			}
			finalDivisionID = division.ID
		}
	}

	if rawRole, exists := rawMap["role"]; exists {
		var roleStr string
		if err := json.Unmarshal(rawRole, &roleStr); err != nil {
			response.BadRequest(w, r, "VAL_001", "role must be a valid string", nil)
			return
		}
		roleStr = strings.TrimSpace(strings.ToLower(roleStr))

		var targetNewRole dbgen.UserRole
		switch roleStr {
		case string(dbgen.UserRoleSuper):
			targetNewRole = dbgen.UserRoleSuper
		case string(dbgen.UserRoleCore):
			targetNewRole = dbgen.UserRoleCore
		case string(dbgen.UserRoleStaff):
			targetNewRole = dbgen.UserRoleStaff
		default:
			response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
				"role": "Role must be 'super', 'core', or 'staff'",
			})
			return
		}

		if targetNewRole == existingStaff.Role {
			response.BadRequest(w, r, "VAL_001", fmt.Sprintf("Staff member already has role '%s'", targetNewRole), nil)
			return
		}

		switch claims.Role {
		case string(dbgen.UserRoleSuper):
			if existingStaff.Role == dbgen.UserRoleSuper && targetNewRole != dbgen.UserRoleSuper {
				superCount, countErr := h.queries.CountActiveSuperAdmins(r.Context())
				if countErr != nil {
					response.InternalServerError(w, r, countErr, "Failed to verify super admin count")
					return
				}
				if superCount <= 1 {
					response.SetError(r.Context(), fmt.Errorf("lockout prevention: attempted demotion of last super %s", targetID))
					response.BadRequest(w, r, "ROLE_LAST_SUPER", "Cannot demote the only remaining Super Administrator", nil)
					return
				}
			}
			finalRole = targetNewRole

		case string(dbgen.UserRoleCore):
			if isSelf {
				if targetNewRole != dbgen.UserRoleStaff {
					response.SetError(r.Context(), fmt.Errorf("role violation: core user %s attempted self-promotion to %s", claims.Email, targetNewRole))
					response.Forbidden(w, r, "Core members can only self-demote to Staff")
					return
				}
				finalRole = targetNewRole
			} else {
				if claims.DivisionID != existingStaff.DivisionID {
					response.SetError(r.Context(), fmt.Errorf("division boundary violation: core user %s (div %d) attempted to modify staff %s (div %d)", claims.Email, claims.DivisionID, targetID, existingStaff.DivisionID))
					response.Forbidden(w, r, "Core members can only promote staff within their own division")
					return
				}
				if existingStaff.Role != dbgen.UserRoleStaff {
					response.SetError(r.Context(), fmt.Errorf("role violation: core user %s attempted to modify non-staff target %s (%s)", claims.Email, targetID, existingStaff.Role))
					response.Forbidden(w, r, "Core members can only promote staff members")
					return
				}
				if targetNewRole != dbgen.UserRoleCore {
					response.SetError(r.Context(), fmt.Errorf("role violation: core user %s attempted to assign role %s", claims.Email, targetNewRole))
					response.Forbidden(w, r, "Core members can only promote staff to Core")
					return
				}
				finalRole = targetNewRole
			}

		default:
			response.SetError(r.Context(), fmt.Errorf("role violation: staff user %s attempted role management", claims.Email))
			response.Forbidden(w, r, "Staff members are not permitted to manage roles")
			return
		}
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

	_, err = qtx.UpdateStaffRoleAndDivision(r.Context(), dbgen.UpdateStaffRoleAndDivisionParams{
		UserID:     targetID,
		Role:       finalRole,
		DivisionID: finalDivisionID,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to update staff record")
		return
	}

	if err := qtx.RevokeAllUserRefreshTokens(r.Context(), targetID); err != nil {
		response.InternalServerError(w, r, err, "Failed to invalidate user sessions")
		return
	}

	if isSelf {
		userWithRoles, err := qtx.GetUserWithRolesByID(r.Context(), claims.UserID)
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to reload user account")
			return
		}

		session, err := IssueUserSession(r.Context(), qtx, h.cfg, UserSessionParams{
			UserID:     claims.UserID,
			Email:      claims.Email,
			IsStaff:    true,
			Role:       string(finalRole),
			DivisionID: finalDivisionID,
			IsHero:     userWithRoles.IsHero,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to issue updated authentication session")
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			response.InternalServerError(w, r, err, "Failed to commit role change")
			return
		}

		divNameStr := ""
		if userWithRoles.StaffDivisionName.Valid {
			divNameStr = userWithRoles.StaffDivisionName.String
		}

		response.Success(w, http.StatusOK, "Role updated successfully", AuthTokenResponse{
			AccessToken:  session.AccessToken,
			RefreshToken: session.RefreshToken,
			ExpiresIn:    int64(session.AccessExpiry.Seconds()),
			User: ToUserResponse(
				userWithRoles.ID,
				userWithRoles.Email,
				userWithRoles.Name,
				userWithRoles.PhoneNumber,
				userWithRoles.IsActive,
				true,
				string(finalRole),
				finalDivisionID,
				divNameStr,
				userWithRoles.IsHero,
				userWithRoles.EmailVerifiedAt,
			),
		})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit staff update")
		return
	}

	row, err := h.queries.GetStaffDetailsByUserID(r.Context(), targetID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to fetch updated staff details")
		return
	}

	var phone *string
	if row.PhoneNumber.Valid {
		phone = &row.PhoneNumber.String
	}
	var pic *string
	if row.ProfilePic.Valid {
		s := uuid.UUID(row.ProfilePic.Bytes).String()
		pic = &s
	}
	var pfpExt *string
	if row.PfpExternalUrl.Valid {
		pfpExt = &row.PfpExternalUrl.String
	}

	response.Success(w, http.StatusOK, "Staff member updated successfully", StaffMemberResponse{
		UserID:         row.UserID.String(),
		Email:          row.Email,
		Name:           row.Name,
		PhoneNumber:    phone,
		ProfilePic:     pic,
		PfpExternalURL: pfpExt,
		IsActive:       row.IsActive,
		DivisionID:     row.DivisionID,
		DivisionName:   row.DivisionName,
		Role:           string(row.Role),
		CreatedAt:      row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Format(time.RFC3339),
	})
}

func (h *StaffHandler) KickStaffHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	if claims.Role != string(dbgen.UserRoleSuper) {
		response.SetError(r.Context(), fmt.Errorf("role violation: user %s (%s) attempted to kick staff", claims.Email, claims.Role))
		response.Forbidden(w, r, "Only Super Administrators can remove staff members")
		return
	}

	idStr := strings.TrimSpace(r.PathValue("id"))
	if idStr == "" {
		response.BadRequest(w, r, "VAL_001", "Staff ID is required", nil)
		return
	}

	targetID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid staff user ID format", nil)
		return
	}

	if targetID == claims.UserID {
		response.BadRequest(w, r, "VAL_001", "Cannot kick yourself. Use the resign endpoint to step down.", nil)
		return
	}

	targetStaff, err := h.queries.GetStaffByUserID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Staff member not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to look up staff member")
		return
	}

	if targetStaff.Role == dbgen.UserRoleSuper {
		superCount, countErr := h.queries.CountActiveSuperAdmins(r.Context())
		if countErr != nil {
			response.InternalServerError(w, r, countErr, "Failed to verify super admin count")
			return
		}
		if superCount <= 1 {
			response.SetError(r.Context(), fmt.Errorf("lockout prevention: attempted removal of last super %s", targetID))
			response.BadRequest(w, r, "ROLE_LAST_SUPER", "Cannot remove the only remaining Super Administrator", nil)
			return
		}
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

	_, err = qtx.SoftDeleteStaff(r.Context(), targetID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to remove staff record")
		return
	}

	_, _ = qtx.CreateHero(r.Context(), dbgen.CreateHeroParams{
		UserID:     targetID,
		RemindMeAt: pgtype.Timestamptz{Valid: false},
	})

	if err := qtx.RevokeAllUserRefreshTokens(r.Context(), targetID); err != nil {
		response.InternalServerError(w, r, err, "Failed to revoke active sessions")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit staff removal")
		return
	}

	response.Success(w, http.StatusOK, "Staff member removed successfully", nil)
}

func (h *StaffHandler) ResignStaffHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	if claims.Role == string(dbgen.UserRoleSuper) {
		superCount, countErr := h.queries.CountActiveSuperAdmins(r.Context())
		if countErr != nil {
			response.InternalServerError(w, r, countErr, "Failed to verify super admin count")
			return
		}
		if superCount <= 1 {
			response.SetError(r.Context(), fmt.Errorf("lockout prevention: attempted resignation of last super %s", claims.UserID))
			response.BadRequest(w, r, "ROLE_LAST_SUPER", "Cannot resign as the only remaining Super Administrator", nil)
			return
		}
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

	_, err = qtx.SoftDeleteStaff(r.Context(), claims.UserID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to process resignation")
		return
	}

	_, _ = qtx.CreateHero(r.Context(), dbgen.CreateHeroParams{
		UserID:     claims.UserID,
		RemindMeAt: pgtype.Timestamptz{Valid: false},
	})

	if err := qtx.RevokeAllUserRefreshTokens(r.Context(), claims.UserID); err != nil {
		response.InternalServerError(w, r, err, "Failed to revoke active sessions")
		return
	}

	session, err := IssueUserSession(r.Context(), qtx, h.cfg, UserSessionParams{
		UserID:     claims.UserID,
		Email:      claims.Email,
		IsStaff:    false,
		Role:       "",
		DivisionID: 0,
		IsHero:     true,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to issue updated authentication session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit resignation")
		return
	}

	user, err := h.queries.GetUserWithRolesByID(r.Context(), claims.UserID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to reload user profile")
		return
	}

	response.Success(w, http.StatusOK, "Resigned from staff successfully. You are now a Hero.", AuthTokenResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		ExpiresIn:    int64(session.AccessExpiry.Seconds()),
		User: ToUserResponse(
			user.ID,
			user.Email,
			user.Name,
			user.PhoneNumber,
			user.IsActive,
			false,
			"",
			0,
			"",
			true,
			user.EmailVerifiedAt,
		),
	})
}

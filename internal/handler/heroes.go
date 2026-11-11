package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

type HeroHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewHeroHandler(pool *pgxpool.Pool, cfg *config.Config) *HeroHandler {
	return &HeroHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type HeroCategoryItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Variant     string    `json:"variant"`
	EmailDomain *string   `json:"email_domain,omitempty"`
	IsPriority  bool      `json:"is_priority"`
	EnrolledAt  time.Time `json:"enrolled_at"`
}

type HeroProfileResponse struct {
	ID          string             `json:"id"`
	Email       string             `json:"email"`
	Name        string             `json:"name"`
	PhoneNumber string             `json:"phone_number"`
	RemindMeAt  *string            `json:"remind_me_at,omitempty"`
	Categories  []HeroCategoryItem `json:"categories"`
}

type AddHeroCategoryRequest struct {
	CategoryID string `json:"category_id"`
}

func (h *HeroHandler) BecomeHeroHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	user, err := h.queries.GetUserWithRolesByID(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Unauthorized(w, r, "User account not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to look up user account")
		return
	}

	if !user.IsActive {
		response.Forbidden(w, r, "Account has been deactivated")
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

	if !user.IsHero {
		_, err = qtx.CreateHero(r.Context(), dbgen.CreateHeroParams{
			UserID:     user.ID,
			RemindMeAt: pgtype.Timestamptz{Valid: false},
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to create hero record")
			return
		}
	}

	if user.EmailVerifiedAt.Valid {
		parts := strings.Split(user.Email, "@")
		if len(parts) == 2 {
			domain := strings.ToLower(strings.TrimSpace(parts[1]))
			matchedCat, matchErr := qtx.MatchUniversityCategoryByEmailDomain(r.Context(), domain)
			if matchErr == nil {
				_, err = qtx.AddHeroCategory(r.Context(), dbgen.AddHeroCategoryParams{
					HeroID:                user.ID,
					BeneficiaryCategoryID: matchedCat.ID,
				})
				if err != nil {
					response.InternalServerError(w, r, err, "Failed to auto-enroll hero into matched category")
					return
				}
			}
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
		response.InternalServerError(w, r, err, "Failed to issue updated authentication session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit become-hero transaction")
		return
	}

	userResp := UserResponseFromRolesByID(user)
	userResp.IsHero = true

	response.Success(w, http.StatusOK, "Successfully registered as a beneficiary hero", AuthTokenResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		ExpiresIn:    int64(session.AccessExpiry.Seconds()),
		User:         userResp,
	})
}

func (h *HeroHandler) GetMyHeroProfileHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	profile, err := h.queries.GetHeroProfileByUserID(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Hero profile not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve hero profile")
		return
	}

	catRows, err := h.queries.ListCategoriesByHeroID(r.Context(), claims.UserID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to list hero categories")
		return
	}

	categories := make([]HeroCategoryItem, 0, len(catRows))
	for _, c := range catRows {
		var domain *string
		if c.EmailDomain.Valid {
			domain = &c.EmailDomain.String
		}
		categories = append(categories, HeroCategoryItem{
			ID:          c.ID.String(),
			Name:        c.Name,
			Variant:     c.Variant,
			EmailDomain: domain,
			IsPriority:  c.IsPriority,
			EnrolledAt:  c.EnrolledAt,
		})
	}

	var remindStr *string
	if profile.RemindMeAt.Valid {
		formatted := profile.RemindMeAt.Time.Format(time.RFC3339)
		remindStr = &formatted
	}

	response.Success(w, http.StatusOK, "Hero profile retrieved successfully", HeroProfileResponse{
		ID:          profile.UserID.String(),
		Email:       profile.Email,
		Name:        profile.Name,
		PhoneNumber: profile.PhoneNumber.String,
		RemindMeAt:  remindStr,
		Categories:  categories,
	})
}

func (h *HeroHandler) ListHeroCategoriesHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	heroID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid hero ID", map[string]string{
			"id": "Must be a valid UUID",
		})
		return
	}

	_, err = h.queries.GetHeroByUserID(r.Context(), heroID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Hero not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify hero")
		return
	}

	catRows, err := h.queries.ListCategoriesByHeroID(r.Context(), heroID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve hero categories")
		return
	}

	categories := make([]HeroCategoryItem, 0, len(catRows))
	for _, c := range catRows {
		var domain *string
		if c.EmailDomain.Valid {
			domain = &c.EmailDomain.String
		}
		categories = append(categories, HeroCategoryItem{
			ID:          c.ID.String(),
			Name:        c.Name,
			Variant:     c.Variant,
			EmailDomain: domain,
			IsPriority:  c.IsPriority,
			EnrolledAt:  c.EnrolledAt,
		})
	}

	response.Success(w, http.StatusOK, "Categories retrieved successfully", categories)
}

func (h *HeroHandler) AddHeroCategoryHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	heroID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid hero ID", map[string]string{
			"id": "Must be a valid UUID",
		})
		return
	}

	var req AddHeroCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	categoryID, err := uuid.Parse(strings.TrimSpace(req.CategoryID))
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid category ID", map[string]string{
			"category_id": "Must be a valid UUID",
		})
		return
	}

	_, err = h.queries.GetHeroByUserID(r.Context(), heroID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Hero not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify hero")
		return
	}

	cat, err := h.queries.GetBeneficiaryCategoryByID(r.Context(), categoryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Category not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify category")
		return
	}

	if cat.Variant != "university" && cat.Variant != "kipk" {
		response.BadRequest(w, r, "VAL_001", "Only university or KIPK categories can be assigned to heroes", map[string]string{
			"variant": fmt.Sprintf("Variant %q cannot be assigned to heroes", cat.Variant),
		})
		return
	}

	enrolled, err := h.queries.AddHeroCategory(r.Context(), dbgen.AddHeroCategoryParams{
		HeroID:                heroID,
		BeneficiaryCategoryID: categoryID,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to assign category to hero")
		return
	}

	response.Success(w, http.StatusCreated, "Category assigned to hero successfully", map[string]any{
		"hero_id":                 enrolled.HeroID.String(),
		"beneficiary_category_id": enrolled.BeneficiaryCategoryID.String(),
		"enrolled_at":             enrolled.CreatedAt,
	})
}

func (h *HeroHandler) RemoveHeroCategoryHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	heroID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid hero ID", map[string]string{
			"id": "Must be a valid UUID",
		})
		return
	}

	catIDStr := r.PathValue("category_id")
	categoryID, err := uuid.Parse(catIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid category ID", map[string]string{
			"category_id": "Must be a valid UUID",
		})
		return
	}

	_, err = h.queries.GetHeroByUserID(r.Context(), heroID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Hero not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify hero")
		return
	}

	err = h.queries.RemoveHeroCategory(r.Context(), dbgen.RemoveHeroCategoryParams{
		HeroID:                heroID,
		BeneficiaryCategoryID: categoryID,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to remove category from hero")
		return
	}

	response.Success(w, http.StatusOK, "Category removed from hero successfully", nil)
}

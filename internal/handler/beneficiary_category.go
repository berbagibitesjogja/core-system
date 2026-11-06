package handler

import (
	"encoding/json"
	"errors"
	"math"
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

var allowedBeneficiaryCategoryVariants = map[string]bool{
	"student":             true,
	"orphanage":           true,
	"nursing_home":        true,
	"shelter":             true,
	"religious_community": true,
	"foundation":          true,
	"society":             true,
	"other":               true,
}

type BeneficiaryCategoryHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewBeneficiaryCategoryHandler(pool *pgxpool.Pool, cfg *config.Config) *BeneficiaryCategoryHandler {
	return &BeneficiaryCategoryHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type CreateBeneficiaryCategoryRequest struct {
	Name    string `json:"name"`
	Variant string `json:"variant"`
	Hidden  *bool  `json:"hidden"`
}

type BeneficiaryCategoryResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Variant   string `json:"variant"`
	Hidden    bool   `json:"hidden"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toBeneficiaryCategoryResponse(cat dbgen.BeneficiaryCategory) BeneficiaryCategoryResponse {
	return BeneficiaryCategoryResponse{
		ID:        cat.ID.String(),
		Name:      cat.Name,
		Variant:   cat.Variant,
		Hidden:    cat.Hidden,
		CreatedAt: cat.CreatedAt.Format(time.RFC3339),
		UpdatedAt: cat.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *BeneficiaryCategoryHandler) ListBeneficiaryCategoriesHandler(w http.ResponseWriter, r *http.Request) {
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

	var hiddenParam pgtype.Bool
	claims, hasClaims := middleware.GetClaims(r.Context())
	isStaff := hasClaims && claims != nil

	if !isStaff {
		hiddenParam = pgtype.Bool{Bool: false, Valid: true}
	} else if hiddenStr := query.Get("hidden"); hiddenStr != "" {
		if hVal, err := strconv.ParseBool(hiddenStr); err == nil {
			hiddenParam = pgtype.Bool{Bool: hVal, Valid: true}
		}
	}

	var variantParam pgtype.Text
	if v := strings.TrimSpace(strings.ToLower(query.Get("variant"))); v != "" {
		if !allowedBeneficiaryCategoryVariants[v] {
			response.BadRequest(w, r, "VAL_001", "Invalid variant filter", nil)
			return
		}
		variantParam = pgtype.Text{String: v, Valid: true}
	}

	var searchParam pgtype.Text
	if s := strings.TrimSpace(query.Get("search")); s != "" {
		searchParam = pgtype.Text{String: s, Valid: true}
	}

	total, err := h.queries.CountBeneficiaryCategories(r.Context(), dbgen.CountBeneficiaryCategoriesParams{
		Hidden:  hiddenParam,
		Variant: variantParam,
		Search:  searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count beneficiary categories")
		return
	}

	offset := int32((page - 1) * limit)
	items, err := h.queries.ListBeneficiaryCategories(r.Context(), dbgen.ListBeneficiaryCategoriesParams{
		Limit:   int32(limit),
		Offset:  offset,
		Hidden:  hiddenParam,
		Variant: variantParam,
		Search:  searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to list beneficiary categories")
		return
	}

	data := make([]BeneficiaryCategoryResponse, 0, len(items))
	for _, item := range items {
		data = append(data, toBeneficiaryCategoryResponse(item))
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(total),
		TotalPages: totalPages,
	}

	response.PaginatedSuccess(w, http.StatusOK, "Beneficiary categories retrieved successfully", data, meta)
}

func (h *BeneficiaryCategoryHandler) GetBeneficiaryCategoryByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	catID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid beneficiary category ID format", nil)
		return
	}

	cat, err := h.queries.GetBeneficiaryCategoryByID(r.Context(), catID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Beneficiary category not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve beneficiary category")
		return
	}

	if cat.Hidden {
		claims, ok := middleware.GetClaims(r.Context())
		if !ok || claims == nil {
			response.NotFound(w, r, "Beneficiary category not found")
			return
		}
	}

	response.Success(w, http.StatusOK, "Beneficiary category retrieved successfully", toBeneficiaryCategoryResponse(cat))
}

func (h *BeneficiaryCategoryHandler) CreateBeneficiaryCategoryHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateBeneficiaryCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Variant = strings.TrimSpace(strings.ToLower(req.Variant))

	fieldErrors := make(map[string]string)
	if req.Name == "" {
		fieldErrors["name"] = "Beneficiary category name is required"
	} else if len(req.Name) > 150 {
		fieldErrors["name"] = "Beneficiary category name cannot exceed 150 characters"
	}

	if req.Variant == "" {
		fieldErrors["variant"] = "Variant is required"
	} else if !allowedBeneficiaryCategoryVariants[req.Variant] {
		fieldErrors["variant"] = "Invalid variant. Allowed: student, orphanage, nursing_home, shelter, religious_community, foundation, society, other"
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	hidden := false
	if req.Hidden != nil {
		hidden = *req.Hidden
	}

	item, err := h.queries.CreateBeneficiaryCategory(r.Context(), dbgen.CreateBeneficiaryCategoryParams{
		Name:    req.Name,
		Variant: req.Variant,
		Hidden:  hidden,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create beneficiary category")
		return
	}

	response.Success(w, http.StatusCreated, "Beneficiary category created successfully", toBeneficiaryCategoryResponse(item))
}

func (h *BeneficiaryCategoryHandler) UpdateBeneficiaryCategoryHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	catID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid beneficiary category ID format", nil)
		return
	}

	existing, err := h.queries.GetBeneficiaryCategoryByID(r.Context(), catID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Beneficiary category not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve beneficiary category")
		return
	}

	var rawMap map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMap); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	updateParams := dbgen.UpdateBeneficiaryCategoryParams{
		ID:      existing.ID,
		Name:    existing.Name,
		Variant: existing.Variant,
		Hidden:  existing.Hidden,
	}

	fieldErrors := make(map[string]string)

	if rawName, exists := rawMap["name"]; exists {
		var name string
		if err := json.Unmarshal(rawName, &name); err != nil {
			fieldErrors["name"] = "Name must be a valid string"
		} else {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				fieldErrors["name"] = "Beneficiary category name cannot be empty"
			} else if len(trimmed) > 150 {
				fieldErrors["name"] = "Beneficiary category name cannot exceed 150 characters"
			} else {
				updateParams.Name = trimmed
			}
		}
	}

	if rawVariant, exists := rawMap["variant"]; exists {
		var variant string
		if err := json.Unmarshal(rawVariant, &variant); err != nil {
			fieldErrors["variant"] = "Variant must be a valid string"
		} else {
			trimmed := strings.TrimSpace(strings.ToLower(variant))
			if !allowedBeneficiaryCategoryVariants[trimmed] {
				fieldErrors["variant"] = "Invalid variant. Allowed: student, orphanage, nursing_home, shelter, religious_community, foundation, society, other"
			} else {
				updateParams.Variant = trimmed
			}
		}
	}

	if rawHidden, exists := rawMap["hidden"]; exists {
		var hidden bool
		if err := json.Unmarshal(rawHidden, &hidden); err != nil {
			fieldErrors["hidden"] = "Hidden must be a boolean"
		} else {
			updateParams.Hidden = hidden
		}
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	updated, err := h.queries.UpdateBeneficiaryCategory(r.Context(), updateParams)
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to update beneficiary category")
		return
	}

	response.Success(w, http.StatusOK, "Beneficiary category updated successfully", toBeneficiaryCategoryResponse(updated))
}

func (h *BeneficiaryCategoryHandler) DeleteBeneficiaryCategoryHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	catID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid beneficiary category ID format", nil)
		return
	}

	deleted, err := h.queries.SoftDeleteBeneficiaryCategory(r.Context(), catID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Beneficiary category not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to delete beneficiary category")
		return
	}

	response.Success(w, http.StatusOK, "Beneficiary category deleted successfully", map[string]string{
		"id": deleted.ID.String(),
	})
}

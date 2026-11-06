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

type SponsorHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewSponsorHandler(pool *pgxpool.Pool, cfg *config.Config) *SponsorHandler {
	return &SponsorHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type CreateSponsorRequest struct {
	Name    string  `json:"name"`
	Variant *string `json:"variant,omitempty"`
	Address *string `json:"address,omitempty"`
	Email   *string `json:"email,omitempty"`
	Phone   *string `json:"phone,omitempty"`
	Logo    *string `json:"logo,omitempty"`
	Hidden  *bool   `json:"hidden,omitempty"`
}

type UpdateSponsorRequest struct {
	Name    *string `json:"name,omitempty"`
	Variant *string `json:"variant,omitempty"`
	Address *string `json:"address,omitempty"`
	Email   *string `json:"email,omitempty"`
	Phone   *string `json:"phone,omitempty"`
	Logo    *string `json:"logo,omitempty"`
	Hidden  *bool   `json:"hidden,omitempty"`
}

type SponsorResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Variant   string  `json:"variant"`
	Address   *string `json:"address,omitempty"`
	Email     *string `json:"email,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Logo      *string `json:"logo,omitempty"`
	Hidden    bool    `json:"hidden"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func toSponsorResponse(s dbgen.Sponsor) SponsorResponse {
	variant := string(dbgen.SponsorVariantIndividual)
	if s.Variant.Valid {
		variant = string(s.Variant.SponsorVariant)
	}

	var address *string
	if s.Address.Valid {
		address = &s.Address.String
	}

	var email *string
	if s.Email.Valid {
		email = &s.Email.String
	}

	var phone *string
	if s.Phone.Valid {
		phone = &s.Phone.String
	}

	var logo *string
	if s.Logo.Valid {
		str := uuid.UUID(s.Logo.Bytes).String()
		logo = &str
	}

	return SponsorResponse{
		ID:        s.ID.String(),
		Name:      s.Name,
		Variant:   variant,
		Address:   address,
		Email:     email,
		Phone:     phone,
		Logo:      logo,
		Hidden:    s.Hidden,
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *SponsorHandler) ListSponsorsHandler(w http.ResponseWriter, r *http.Request) {
	page := 1
	limit := 10

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

	var variantParam dbgen.NullSponsorVariant
	if v := strings.TrimSpace(strings.ToLower(query.Get("variant"))); v != "" {
		switch v {
		case string(dbgen.SponsorVariantCompany):
			variantParam = dbgen.NullSponsorVariant{SponsorVariant: dbgen.SponsorVariantCompany, Valid: true}
		case string(dbgen.SponsorVariantIndividual):
			variantParam = dbgen.NullSponsorVariant{SponsorVariant: dbgen.SponsorVariantIndividual, Valid: true}
		default:
			response.BadRequest(w, r, "VAL_001", "Invalid variant filter. Must be 'company' or 'individual'", nil)
			return
		}
	}

	var searchParam pgtype.Text
	if s := strings.TrimSpace(query.Get("search")); s != "" {
		searchParam = pgtype.Text{String: s, Valid: true}
	}

	total, err := h.queries.CountSponsors(r.Context(), dbgen.CountSponsorsParams{
		Hidden:  hiddenParam,
		Variant: variantParam,
		Search:  searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count sponsors")
		return
	}

	offset := int32((page - 1) * limit)
	sponsors, err := h.queries.ListSponsors(r.Context(), dbgen.ListSponsorsParams{
		Limit:   int32(limit),
		Offset:  offset,
		Hidden:  hiddenParam,
		Variant: variantParam,
		Search:  searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to list sponsors")
		return
	}

	data := make([]SponsorResponse, 0, len(sponsors))
	for _, item := range sponsors {
		data = append(data, toSponsorResponse(item))
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(total),
		TotalPages: totalPages,
	}

	response.PaginatedSuccess(w, http.StatusOK, "Sponsors retrieved successfully", data, meta)
}

func (h *SponsorHandler) GetSponsorByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sponsorID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid sponsor ID format", nil)
		return
	}

	sponsor, err := h.queries.GetSponsorByID(r.Context(), sponsorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Sponsor not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve sponsor")
		return
	}

	if sponsor.Hidden {
		claims, ok := middleware.GetClaims(r.Context())
		if !ok || claims == nil {
			response.NotFound(w, r, "Sponsor not found")
			return
		}
	}

	response.Success(w, http.StatusOK, "Sponsor retrieved successfully", toSponsorResponse(sponsor))
}

func (h *SponsorHandler) CreateSponsorHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateSponsorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	fieldErrors := make(map[string]string)

	if req.Name == "" {
		fieldErrors["name"] = "Sponsor name is required"
	} else if len(req.Name) > 50 {
		fieldErrors["name"] = "Sponsor name cannot exceed 50 characters"
	}

	variantParam := dbgen.NullSponsorVariant{SponsorVariant: dbgen.SponsorVariantIndividual, Valid: true}
	if req.Variant != nil {
		v := strings.TrimSpace(strings.ToLower(*req.Variant))
		switch v {
		case string(dbgen.SponsorVariantCompany):
			variantParam = dbgen.NullSponsorVariant{SponsorVariant: dbgen.SponsorVariantCompany, Valid: true}
		case string(dbgen.SponsorVariantIndividual):
			variantParam = dbgen.NullSponsorVariant{SponsorVariant: dbgen.SponsorVariantIndividual, Valid: true}
		default:
			fieldErrors["variant"] = "Variant must be 'company' or 'individual'"
		}
	}

	var addressParam pgtype.Text
	if req.Address != nil {
		addr := strings.TrimSpace(*req.Address)
		if len(addr) > 500 {
			fieldErrors["address"] = "Address cannot exceed 500 characters"
		} else if addr != "" {
			addressParam = pgtype.Text{String: addr, Valid: true}
		}
	}

	var emailParam pgtype.Text
	if req.Email != nil {
		email := strings.TrimSpace(strings.ToLower(*req.Email))
		if email != "" {
			if !strings.Contains(email, "@") {
				fieldErrors["email"] = "Invalid email format"
			} else if len(email) > 255 {
				fieldErrors["email"] = "Email cannot exceed 255 characters"
			} else {
				emailParam = pgtype.Text{String: email, Valid: true}
			}
		}
	}

	var phoneParam pgtype.Text
	if req.Phone != nil {
		phone := strings.TrimSpace(*req.Phone)
		if len(phone) > 20 {
			fieldErrors["phone"] = "Phone cannot exceed 20 characters"
		} else if phone != "" {
			phoneParam = pgtype.Text{String: phone, Valid: true}
		}
	}

	var logoParam pgtype.UUID
	if req.Logo != nil && strings.TrimSpace(*req.Logo) != "" {
		parsedLogo, err := uuid.Parse(strings.TrimSpace(*req.Logo))
		if err != nil {
			fieldErrors["logo"] = "Invalid logo file ID format"
		} else {
			logoParam = pgtype.UUID{Bytes: parsedLogo, Valid: true}
		}
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	hidden := false
	if req.Hidden != nil {
		hidden = *req.Hidden
	}

	sponsor, err := h.queries.CreateSponsor(r.Context(), dbgen.CreateSponsorParams{
		Name:    req.Name,
		Variant: variantParam,
		Address: addressParam,
		Email:   emailParam,
		Phone:   phoneParam,
		Logo:    logoParam,
		Hidden:  hidden,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create sponsor")
		return
	}

	response.Success(w, http.StatusCreated, "Sponsor created successfully", toSponsorResponse(sponsor))
}

func (h *SponsorHandler) UpdateSponsorHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sponsorID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid sponsor ID format", nil)
		return
	}

	existing, err := h.queries.GetSponsorByID(r.Context(), sponsorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Sponsor not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve sponsor")
		return
	}

	var req UpdateSponsorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	fieldErrors := make(map[string]string)
	name := existing.Name
	if req.Name != nil {
		trimmedName := strings.TrimSpace(*req.Name)
		if trimmedName == "" {
			fieldErrors["name"] = "Sponsor name cannot be empty"
		} else if len(trimmedName) > 50 {
			fieldErrors["name"] = "Sponsor name cannot exceed 50 characters"
		} else {
			name = trimmedName
		}
	}

	variant := existing.Variant
	if req.Variant != nil {
		v := strings.TrimSpace(strings.ToLower(*req.Variant))
		switch v {
		case string(dbgen.SponsorVariantCompany):
			variant = dbgen.NullSponsorVariant{SponsorVariant: dbgen.SponsorVariantCompany, Valid: true}
		case string(dbgen.SponsorVariantIndividual):
			variant = dbgen.NullSponsorVariant{SponsorVariant: dbgen.SponsorVariantIndividual, Valid: true}
		default:
			fieldErrors["variant"] = "Variant must be 'company' or 'individual'"
		}
	}

	address := existing.Address
	if req.Address != nil {
		addr := strings.TrimSpace(*req.Address)
		if len(addr) > 500 {
			fieldErrors["address"] = "Address cannot exceed 500 characters"
		} else if addr == "" {
			address = pgtype.Text{Valid: false}
		} else {
			address = pgtype.Text{String: addr, Valid: true}
		}
	}

	email := existing.Email
	if req.Email != nil {
		e := strings.TrimSpace(strings.ToLower(*req.Email))
		if e == "" {
			email = pgtype.Text{Valid: false}
		} else if !strings.Contains(e, "@") {
			fieldErrors["email"] = "Invalid email format"
		} else if len(e) > 255 {
			fieldErrors["email"] = "Email cannot exceed 255 characters"
		} else {
			email = pgtype.Text{String: e, Valid: true}
		}
	}

	phone := existing.Phone
	if req.Phone != nil {
		p := strings.TrimSpace(*req.Phone)
		if len(p) > 20 {
			fieldErrors["phone"] = "Phone cannot exceed 20 characters"
		} else if p == "" {
			phone = pgtype.Text{Valid: false}
		} else {
			phone = pgtype.Text{String: p, Valid: true}
		}
	}

	logo := existing.Logo
	if req.Logo != nil {
		l := strings.TrimSpace(*req.Logo)
		if l == "" {
			logo = pgtype.UUID{Valid: false}
		} else {
			parsedLogo, err := uuid.Parse(l)
			if err != nil {
				fieldErrors["logo"] = "Invalid logo file ID format"
			} else {
				logo = pgtype.UUID{Bytes: parsedLogo, Valid: true}
			}
		}
	}

	hidden := existing.Hidden
	if req.Hidden != nil {
		hidden = *req.Hidden
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	updated, err := h.queries.UpdateSponsor(r.Context(), dbgen.UpdateSponsorParams{
		ID:      sponsorID,
		Name:    name,
		Variant: variant,
		Address: address,
		Email:   email,
		Phone:   phone,
		Logo:    logo,
		Hidden:  hidden,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to update sponsor")
		return
	}

	response.Success(w, http.StatusOK, "Sponsor updated successfully", toSponsorResponse(updated))
}

func (h *SponsorHandler) DeleteSponsorHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sponsorID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid sponsor ID format", nil)
		return
	}

	row, err := h.queries.SoftDeleteSponsor(r.Context(), sponsorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Sponsor not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to delete sponsor")
		return
	}

	response.Success(w, http.StatusOK, "Sponsor deleted successfully", map[string]string{
		"id": row.ID.String(),
	})
}

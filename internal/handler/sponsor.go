package handler

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/mail"
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
	Name          string  `json:"name"`
	Variant       *string `json:"variant,omitempty"`
	Address       *string `json:"address,omitempty"`
	Email         *string `json:"email,omitempty"`
	Phone         *string `json:"phone,omitempty"`
	Logo          *string `json:"logo,omitempty"`
	Hidden        *bool   `json:"hidden,omitempty"`
	HideDonations *bool   `json:"hide_donations,omitempty"`
}

type UpdateSponsorRequest struct {
	Name          *string `json:"name,omitempty"`
	Variant       *string `json:"variant,omitempty"`
	Address       *string `json:"address,omitempty"`
	Email         *string `json:"email,omitempty"`
	Phone         *string `json:"phone,omitempty"`
	Logo          *string `json:"logo,omitempty"`
	Hidden        *bool   `json:"hidden,omitempty"`
	HideDonations *bool   `json:"hide_donations,omitempty"`
}

type PublicSponsorListItem struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Variant string  `json:"variant"`
	Logo    *string `json:"logo"`
}

type SponsorResponse struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Variant       string  `json:"variant"`
	Address       *string `json:"address,omitempty"`
	Email         *string `json:"email,omitempty"`
	Phone         *string `json:"phone,omitempty"`
	Logo          *string `json:"logo,omitempty"`
	Hidden        bool    `json:"hidden"`
	HideDonations bool    `json:"hide_donations"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func toPublicSponsorListItem(s dbgen.Sponsor) PublicSponsorListItem {
	variant := string(s.Variant)
	var logo *string
	if s.Logo.Valid {
		str := uuid.UUID(s.Logo.Bytes).String()
		logo = &str
	}
	return PublicSponsorListItem{
		ID:      s.ID.String(),
		Name:    s.Name,
		Variant: variant,
		Logo:    logo,
	}
}

func toSponsorResponse(s dbgen.Sponsor) SponsorResponse {
	variant := string(s.Variant)

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
		ID:            s.ID.String(),
		Name:          s.Name,
		Variant:       variant,
		Address:       address,
		Email:         email,
		Phone:         phone,
		Logo:          logo,
		Hidden:        s.Hidden,
		HideDonations: s.HideDonations,
		CreatedAt:     s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     s.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *SponsorHandler) ListSponsorsHandler(w http.ResponseWriter, r *http.Request) {
	page := 1
	defaultLimit := 20
	maxLimit := 100
	if h.cfg != nil {
		if h.cfg.AppConfig.DefaultLimit > 0 {
			defaultLimit = h.cfg.AppConfig.DefaultLimit
		}
		if h.cfg.AppConfig.MaxLimit > 0 {
			maxLimit = h.cfg.AppConfig.MaxLimit
		}
	}
	limit := defaultLimit

	query := r.URL.Query()
	if pageStr := query.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if limitStr := query.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			if l > maxLimit {
				limit = maxLimit
			} else {
				limit = l
			}
		}
	}

	var hiddenParam pgtype.Bool
	claims, hasClaims := middleware.GetClaims(r.Context())
	isStaff := hasClaims && claims != nil && claims.IsStaff

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
		searchParam = pgtype.Text{String: EscapeLikePattern(s), Valid: true}
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

	offset := (page - 1) * limit
	sponsors, err := h.queries.ListSponsors(r.Context(), dbgen.ListSponsorsParams{
		Limit:   int32(limit),
		Offset:  int32(offset),
		Hidden:  hiddenParam,
		Variant: variantParam,
		Search:  searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to list sponsors")
		return
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	if totalPages == 0 {
		totalPages = 1
	}

	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(total),
		TotalPages: totalPages,
	}

	if isStaff {
		res := make([]SponsorResponse, 0, len(sponsors))
		for _, s := range sponsors {
			res = append(res, toSponsorResponse(s))
		}
		response.PaginatedSuccess(w, http.StatusOK, "Sponsors retrieved successfully", res, meta)
		return
	}

	pubRes := make([]PublicSponsorListItem, 0, len(sponsors))
	for _, s := range sponsors {
		pubRes = append(pubRes, toPublicSponsorListItem(s))
	}
	response.PaginatedSuccess(w, http.StatusOK, "Sponsors retrieved successfully", pubRes, meta)
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

	claims, hasClaims := middleware.GetClaims(r.Context())
	isStaff := hasClaims && claims != nil && claims.IsStaff

	if !isStaff && sponsor.Hidden {
		response.NotFound(w, r, "Sponsor not found")
		return
	}

	if isStaff {
		response.Success(w, http.StatusOK, "Sponsor retrieved successfully", toSponsorResponse(sponsor))
		return
	}

	response.Success(w, http.StatusOK, "Sponsor retrieved successfully", toPublicSponsorListItem(sponsor))
}

func (h *SponsorHandler) CreateSponsorHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateSponsorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	fieldErrors := make(map[string]string)

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		fieldErrors["name"] = "Sponsor name is required"
	} else if len(req.Name) > 150 {
		fieldErrors["name"] = "Sponsor name cannot exceed 150 characters"
	}

	variantParam := dbgen.SponsorVariantIndividual
	if req.Variant != nil {
		v := strings.TrimSpace(strings.ToLower(*req.Variant))
		switch v {
		case string(dbgen.SponsorVariantCompany):
			variantParam = dbgen.SponsorVariantCompany
		case string(dbgen.SponsorVariantIndividual):
			variantParam = dbgen.SponsorVariantIndividual
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
			parsed, err := mail.ParseAddress(email)
			if err != nil || parsed.Address != email {
				fieldErrors["email"] = "Invalid email format"
			} else if len(email) > 255 {
				fieldErrors["email"] = "Email cannot exceed 255 characters"
			} else {
				emailParam = pgtype.Text{String: parsed.Address, Valid: true}
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
	var logoNotFound bool
	var logoInvalid bool
	if req.Logo != nil && strings.TrimSpace(*req.Logo) != "" {
		parsedLogo, err := uuid.Parse(strings.TrimSpace(*req.Logo))
		if err != nil {
			fieldErrors["logo"] = "Invalid logo file ID format"
		} else {
			var fileStatus, storageBucket, mimeType string
			err = h.pool.QueryRow(r.Context(), "SELECT status, storage_bucket, mime_type FROM files WHERE id = $1", parsedLogo).Scan(&fileStatus, &storageBucket, &mimeType)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					logoNotFound = true
				} else {
					logoInvalid = true
				}
			} else if fileStatus != "uploaded" || storageBucket != "public" || !strings.HasPrefix(mimeType, "image/") {
				logoInvalid = true
			} else {
				logoParam = pgtype.UUID{Bytes: parsedLogo, Valid: true}
			}
		}
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}
	if logoNotFound {
		response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
			"logo": "Referenced logo file does not exist",
		})
		return
	}
	if logoInvalid {
		response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
			"logo": "File not found or not uploaded",
		})
		return
	}
	hidden := false
	if req.Hidden != nil {
		hidden = *req.Hidden
	}

	hideDonations := true
	if req.HideDonations != nil {
		hideDonations = *req.HideDonations
	}

	sponsor, err := h.queries.CreateSponsor(r.Context(), dbgen.CreateSponsorParams{
		Name:          req.Name,
		Variant:       variantParam,
		Address:       addressParam,
		Email:         emailParam,
		Phone:         phoneParam,
		Logo:          logoParam,
		Hidden:        hidden,
		HideDonations: hideDonations,
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

	var req UpdateSponsorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txQueries := h.queries.WithTx(tx)

	existing, err := txQueries.GetSponsorByIDForUpdate(r.Context(), sponsorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Sponsor not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve sponsor")
		return
	}

	fieldErrors := make(map[string]string)
	name := existing.Name
	if req.Name != nil {
		trimmedName := strings.TrimSpace(*req.Name)
		if trimmedName == "" {
			fieldErrors["name"] = "Sponsor name cannot be empty"
		} else if len(trimmedName) > 150 {
			fieldErrors["name"] = "Sponsor name cannot exceed 150 characters"
		} else {
			name = trimmedName
		}
	}

	variant := existing.Variant
	if req.Variant != nil {
		v := strings.TrimSpace(strings.ToLower(*req.Variant))
		switch v {
		case string(dbgen.SponsorVariantCompany):
			variant = dbgen.SponsorVariantCompany
		case string(dbgen.SponsorVariantIndividual):
			variant = dbgen.SponsorVariantIndividual
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
		} else {
			parsed, err := mail.ParseAddress(e)
			if err != nil || parsed.Address != e {
				fieldErrors["email"] = "Invalid email format"
			} else if len(e) > 255 {
				fieldErrors["email"] = "Email cannot exceed 255 characters"
			} else {
				email = pgtype.Text{String: parsed.Address, Valid: true}
			}
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
	var updateLogoNotFound bool
	var updateLogoInvalid bool
	if req.Logo != nil {
		l := strings.TrimSpace(*req.Logo)
		if l == "" {
			logo = pgtype.UUID{Valid: false}
		} else {
			parsedLogo, err := uuid.Parse(l)
			if err != nil {
				fieldErrors["logo"] = "Invalid logo file ID format"
			} else {
				var fileStatus, storageBucket, mimeType string
				err = tx.QueryRow(r.Context(), "SELECT status, storage_bucket, mime_type FROM files WHERE id = $1", parsedLogo).Scan(&fileStatus, &storageBucket, &mimeType)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						updateLogoNotFound = true
					} else {
						updateLogoInvalid = true
					}
				} else if fileStatus != "uploaded" || storageBucket != "public" || !strings.HasPrefix(mimeType, "image/") {
					updateLogoInvalid = true
				} else {
					logo = pgtype.UUID{Bytes: parsedLogo, Valid: true}
				}
			}
		}
	}

	hidden := existing.Hidden
	if req.Hidden != nil {
		hidden = *req.Hidden
	}

	hideDonations := existing.HideDonations
	if req.HideDonations != nil {
		hideDonations = *req.HideDonations
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}
	if updateLogoNotFound {
		response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
			"logo": "Referenced logo file does not exist",
		})
		return
	}
	if updateLogoInvalid {
		response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
			"logo": "File not found or not uploaded",
		})
		return
	}
	updated, err := txQueries.UpdateSponsor(r.Context(), dbgen.UpdateSponsorParams{
		ID:            sponsorID,
		Name:          name,
		Variant:       variant,
		Address:       address,
		Email:         email,
		Phone:         phone,
		Logo:          logo,
		Hidden:        hidden,
		HideDonations: hideDonations,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to update sponsor")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit sponsor update transaction")
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


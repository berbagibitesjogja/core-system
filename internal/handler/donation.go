package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type DonationHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewDonationHandler(pool *pgxpool.Pool, cfg *config.Config) *DonationHandler {
	return &DonationHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type FoodItemInput struct {
	FoodItemID int32   `json:"food_item_id"`
	Quantity   float64 `json:"quantity"`
	Unit       string  `json:"unit"`
}

type CreateDonationRequest struct {
	SponsorID              string          `json:"sponsor_id"`
	EventAt                string          `json:"event_at"`
	Quota                  int             `json:"quota"`
	HeroQuota              *int            `json:"hero_quota,omitempty"`
	Address                *string         `json:"address,omitempty"`
	Maps                   *string         `json:"maps,omitempty"`
	Message                *string         `json:"message,omitempty"`
	HideFromPublic         *bool           `json:"hide_from_public,omitempty"`
	HideSponsor            *bool           `json:"hide_sponsor,omitempty"`
	Foods                  []FoodItemInput `json:"foods"`
	BeneficiaryCategoryIDs []string        `json:"beneficiary_category_ids"`
}

type UpdateDonationRequest struct {
	SponsorID              *string         `json:"sponsor_id,omitempty"`
	EventAt                *string         `json:"event_at,omitempty"`
	Quota                  *int            `json:"quota,omitempty"`
	HeroQuota              *int            `json:"hero_quota,omitempty"`
	Address                *string         `json:"address,omitempty"`
	Maps                   *string         `json:"maps,omitempty"`
	Message                *string         `json:"message,omitempty"`
	HideFromPublic         *bool           `json:"hide_from_public,omitempty"`
	HideSponsor            *bool           `json:"hide_sponsor,omitempty"`
	Foods                  []FoodItemInput `json:"foods,omitempty"`
	BeneficiaryCategoryIDs []string        `json:"beneficiary_category_ids,omitempty"`
}

type FoodResponse struct {
	FoodItemID int32   `json:"food_item_id"`
	Name       string  `json:"name"`
	Quantity   float64 `json:"quantity"`
	Unit       string  `json:"unit"`
}

type TargetResponse struct {
	BeneficiaryCategoryID *string `json:"beneficiary_category_id"`
	Name                  string  `json:"name"`
	Variant               *string `json:"variant"`
}

type StaffSponsorResponse struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Logo          *string `json:"logo"`
	Hidden        bool    `json:"hidden"`
	HideDonations bool    `json:"hide_donations"`
	IsDeleted     bool    `json:"is_deleted"`
	Email         *string `json:"email"`
	Phone         *string `json:"phone"`
	Address       *string `json:"address"`
}

type PublicSponsorResponse struct {
	ID   *string `json:"id"`
	Name string  `json:"name"`
	Logo *string `json:"logo"`
}

type CreatedByResponse struct {
	ID    string  `json:"id"`
	Email *string `json:"email,omitempty"`
}

type DonationStaffResponse struct {
	ID             string               `json:"id"`
	EventAt        string               `json:"event_at"`
	Quota          int32                `json:"quota"`
	HeroQuota      int32                `json:"hero_quota"`
	Remain         int32                `json:"remain"`
	Address        *string              `json:"address,omitempty"`
	Maps           *string              `json:"maps,omitempty"`
	Message        *string              `json:"message,omitempty"`
	Status         string               `json:"status"`
	HideFromPublic bool                 `json:"hide_from_public"`
	HideSponsor    bool                 `json:"hide_sponsor"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
	Sponsor        StaffSponsorResponse `json:"sponsor"`
	CreatedBy      *CreatedByResponse   `json:"created_by,omitempty"`
	Foods          []FoodResponse       `json:"foods"`
	Targets        []TargetResponse     `json:"targets"`
}

type DonationPublicResponse struct {
	ID        string                `json:"id"`
	EventAt   string                `json:"event_at"`
	HeroQuota int32                 `json:"hero_quota"`
	Remain    int32                 `json:"remain"`
	IsFull    bool                  `json:"is_full"`
	Address   *string               `json:"address,omitempty"`
	Maps      *string               `json:"maps,omitempty"`
	Message   *string               `json:"message,omitempty"`
	Status    string                `json:"status"`
	CreatedAt string                `json:"created_at"`
	UpdatedAt string                `json:"updated_at"`
	Sponsor   PublicSponsorResponse `json:"sponsor"`
	Foods     []FoodResponse        `json:"foods"`
	Targets   []TargetResponse      `json:"targets"`
}

var validFoodUnits = map[string]dbgen.FoodUnit{
	"kg":      dbgen.FoodUnitKg,
	"g":       dbgen.FoodUnitG,
	"l":       dbgen.FoodUnitL,
	"ml":      dbgen.FoodUnitMl,
	"portion": dbgen.FoodUnitPortion,
	"pack":    dbgen.FoodUnitPack,
	"piece":   dbgen.FoodUnitPiece,
}

func NumericToFloat64(num pgtype.Numeric) float64 {
	val, err := num.Float64Value()
	if err != nil || !val.Valid {
		return 0
	}
	return val.Float64
}

func Float64ToNumeric(val float64) (pgtype.Numeric, error) {
	var num pgtype.Numeric
	strVal := strconv.FormatFloat(val, 'f', 2, 64)
	err := num.Scan(strVal)
	return num, err
}

func ValidateCreateDonationRequest(req CreateDonationRequest) (uuid.UUID, time.Time, int32, int32, bool, bool, map[string]string) {
	fieldErrors := make(map[string]string)

	var sponsorUUID uuid.UUID
	trimmedSponsorID := strings.TrimSpace(req.SponsorID)
	if trimmedSponsorID == "" {
		fieldErrors["sponsor_id"] = "Sponsor ID is required"
	} else {
		parsed, err := uuid.Parse(trimmedSponsorID)
		if err != nil {
			fieldErrors["sponsor_id"] = "Invalid sponsor ID format"
		} else {
			sponsorUUID = parsed
		}
	}

	var eventTime time.Time
	trimmedEventAt := strings.TrimSpace(req.EventAt)
	if trimmedEventAt == "" {
		fieldErrors["event_at"] = "Event datetime is required"
	} else {
		t, err := time.Parse(time.RFC3339, trimmedEventAt)
		if err != nil {
			fieldErrors["event_at"] = "Event datetime must be in valid RFC3339 format"
		} else {
			eventTime = t
		}
	}

	if req.Quota <= 0 {
		fieldErrors["quota"] = "Quota must be greater than zero"
	}
	quota := int32(req.Quota)

	heroQuota := int32(0)
	if req.HeroQuota != nil {
		if *req.HeroQuota < 0 {
			fieldErrors["hero_quota"] = "Hero quota cannot be negative"
		} else if *req.HeroQuota > req.Quota {
			fieldErrors["hero_quota"] = "Hero quota cannot exceed total quota"
		} else {
			heroQuota = int32(*req.HeroQuota)
		}
	}

	if req.Address != nil && len(strings.TrimSpace(*req.Address)) > 500 {
		fieldErrors["address"] = "Address cannot exceed 500 characters"
	}

	if len(req.Foods) == 0 {
		fieldErrors["foods"] = "At least one food item is required"
	} else {
		seenFoodItems := make(map[int32]bool)
		for _, f := range req.Foods {
			if f.FoodItemID <= 0 {
				fieldErrors["foods"] = "Invalid food item ID"
				break
			}
			if f.Quantity <= 0 {
				fieldErrors["foods"] = "Food item quantity must be greater than zero"
				break
			}
			unitClean := strings.ToLower(strings.TrimSpace(f.Unit))
			if _, ok := validFoodUnits[unitClean]; !ok {
				fieldErrors["foods"] = "Invalid food unit. Must be one of: kg, g, l, ml, portion, pack, piece"
				break
			}
			if seenFoodItems[f.FoodItemID] {
				fieldErrors["foods"] = fmt.Sprintf("Duplicate food item ID %d in food list", f.FoodItemID)
				break
			}
			seenFoodItems[f.FoodItemID] = true
		}
	}

	if len(req.BeneficiaryCategoryIDs) > 0 {
		seenTargets := make(map[uuid.UUID]bool)
		for _, tStr := range req.BeneficiaryCategoryIDs {
			parsed, err := uuid.Parse(strings.TrimSpace(tStr))
			if err != nil {
				fieldErrors["beneficiary_category_ids"] = "Invalid beneficiary category ID format"
				break
			}
			if seenTargets[parsed] {
				fieldErrors["beneficiary_category_ids"] = fmt.Sprintf("Duplicate beneficiary category ID %s in target list", parsed.String())
				break
			}
			seenTargets[parsed] = true
		}
	}

	hideFromPublic := true
	if req.HideFromPublic != nil {
		hideFromPublic = *req.HideFromPublic
	}

	hideSponsor := false
	if req.HideSponsor != nil {
		hideSponsor = *req.HideSponsor
	}

	return sponsorUUID, eventTime, quota, heroQuota, hideFromPublic, hideSponsor, fieldErrors
}

func ValidateUpdateDonationRequest(req UpdateDonationRequest) (
	sponsorUUID *uuid.UUID,
	eventTime *time.Time,
	fieldErrors map[string]string,
) {
	fieldErrors = make(map[string]string)

	if req.SponsorID != nil {
		trimmed := strings.TrimSpace(*req.SponsorID)
		if trimmed == "" {
			fieldErrors["sponsor_id"] = "Sponsor ID cannot be empty"
		} else {
			parsed, err := uuid.Parse(trimmed)
			if err != nil {
				fieldErrors["sponsor_id"] = "Invalid sponsor ID format"
			} else {
				sponsorUUID = &parsed
			}
		}
	}

	if req.EventAt != nil {
		trimmed := strings.TrimSpace(*req.EventAt)
		if trimmed == "" {
			fieldErrors["event_at"] = "Event datetime cannot be empty"
		} else {
			t, err := time.Parse(time.RFC3339, trimmed)
			if err != nil {
				fieldErrors["event_at"] = "Event datetime must be in valid RFC3339 format"
			} else {
				eventTime = &t
			}
		}
	}

	if req.Quota != nil {
		if *req.Quota <= 0 {
			fieldErrors["quota"] = "Quota must be greater than zero"
		}
	}

	if req.HeroQuota != nil {
		if *req.HeroQuota < 0 {
			fieldErrors["hero_quota"] = "Hero quota cannot be negative"
		}
	}

	if req.Address != nil && len(strings.TrimSpace(*req.Address)) > 500 {
		fieldErrors["address"] = "Address cannot exceed 500 characters"
	}

	if req.Foods != nil {
		if len(req.Foods) == 0 {
			fieldErrors["foods"] = "At least one food item is required"
		} else {
			seenFoodItems := make(map[int32]bool)
			for _, f := range req.Foods {
				if f.FoodItemID <= 0 {
					fieldErrors["foods"] = "Invalid food item ID"
					break
				}
				if f.Quantity <= 0 {
					fieldErrors["foods"] = "Food item quantity must be greater than zero"
					break
				}
				unitClean := strings.ToLower(strings.TrimSpace(f.Unit))
				if _, ok := validFoodUnits[unitClean]; !ok {
					fieldErrors["foods"] = "Invalid food unit. Must be one of: kg, g, l, ml, portion, pack, piece"
					break
				}
				if seenFoodItems[f.FoodItemID] {
					fieldErrors["foods"] = fmt.Sprintf("Duplicate food item ID %d in food list", f.FoodItemID)
					break
				}
				seenFoodItems[f.FoodItemID] = true
			}
		}
	}

	if req.BeneficiaryCategoryIDs != nil && len(req.BeneficiaryCategoryIDs) > 0 {
		seenTargets := make(map[uuid.UUID]bool)
		for _, tStr := range req.BeneficiaryCategoryIDs {
			parsed, err := uuid.Parse(strings.TrimSpace(tStr))
			if err != nil {
				fieldErrors["beneficiary_category_ids"] = "Invalid beneficiary category ID format"
				break
			}
			if seenTargets[parsed] {
				fieldErrors["beneficiary_category_ids"] = fmt.Sprintf("Duplicate beneficiary category ID %s in target list", parsed.String())
				break
			}
			seenTargets[parsed] = true
		}
	}

	return sponsorUUID, eventTime, fieldErrors
}

func BuildStaffSponsorResponse(sponsorID uuid.UUID, name string, logo pgtype.UUID, hidden, hideDonations, isDeleted bool, email, phone, address pgtype.Text) StaffSponsorResponse {
	var logoStr *string
	if logo.Valid {
		str := uuid.UUID(logo.Bytes).String()
		logoStr = &str
	}

	var emailStr *string
	if email.Valid {
		emailStr = &email.String
	}

	var phoneStr *string
	if phone.Valid {
		phoneStr = &phone.String
	}

	var addressStr *string
	if address.Valid {
		addressStr = &address.String
	}

	return StaffSponsorResponse{
		ID:            sponsorID.String(),
		Name:          name,
		Logo:          logoStr,
		Hidden:        hidden,
		HideDonations: hideDonations,
		IsDeleted:     isDeleted,
		Email:         emailStr,
		Phone:         phoneStr,
		Address:       addressStr,
	}
}

func BuildPublicSponsorResponse(sponsorID uuid.UUID, name string, logo pgtype.UUID, sponsorHidden, donationHideSponsor bool) PublicSponsorResponse {

	if donationHideSponsor || sponsorHidden {
		return PublicSponsorResponse{
			ID:   nil,
			Name: "Generous sponsor",
			Logo: nil,
		}
	}

	idStr := sponsorID.String()
	var logoStr *string
	if logo.Valid {
		str := uuid.UUID(logo.Bytes).String()
		logoStr = &str
	}

	return PublicSponsorResponse{
		ID:   &idStr,
		Name: name,
		Logo: logoStr,
	}
}

func textPtr(t pgtype.Text) *string {
	if t.Valid {
		return &t.String
	}
	return nil
}

func buildFoodResponses(foodsList []dbgen.ListFoodsByDonationIDRow) []FoodResponse {
	foodResponses := make([]FoodResponse, 0, len(foodsList))
	for _, fl := range foodsList {
		foodResponses = append(foodResponses, FoodResponse{
			FoodItemID: fl.FoodItemID,
			Name:       fl.FoodName,
			Quantity:   NumericToFloat64(fl.Quantity),
			Unit:       string(fl.Unit),
		})
	}
	return foodResponses
}

func buildTargetResponses(targetsList []dbgen.ListTargetsByDonationIDRow, isStaff bool) []TargetResponse {
	targetResponses := make([]TargetResponse, 0, len(targetsList))
	hasHidden := false
	for _, tl := range targetsList {
		if !isStaff && tl.CategoryHidden {
			hasHidden = true
			continue
		}
		catID := tl.BeneficiaryCategoryID.String()
		variant := string(tl.CategoryVariant)
		targetResponses = append(targetResponses, TargetResponse{
			BeneficiaryCategoryID: &catID,
			Name:                  tl.CategoryName,
			Variant:               &variant,
		})
	}
	if !isStaff && hasHidden {
		targetResponses = append(targetResponses, TargetResponse{
			BeneficiaryCategoryID: nil,
			Name:                  "Hidden institution",
			Variant:               nil,
		})
	}
	return targetResponses
}

func buildDonationStaffResponse(details dbgen.GetDonationDetailsByIDRow, foods []FoodResponse, targets []TargetResponse) DonationStaffResponse {
	var createdByResp *CreatedByResponse
	if details.CreatedBy.Valid {
		cID := uuid.UUID(details.CreatedBy.Bytes).String()
		var cEmail *string
		if details.CreatedByEmail.Valid {
			cEmail = &details.CreatedByEmail.String
		}
		createdByResp = &CreatedByResponse{
			ID:    cID,
			Email: cEmail,
		}
	}

	return DonationStaffResponse{
		ID:             details.ID.String(),
		EventAt:        details.EventAt.Format(time.RFC3339),
		Quota:          details.Quota,
		HeroQuota:      details.HeroQuota,
		Remain:         details.Remain,
		Address:        textPtr(details.Address),
		Maps:           textPtr(details.Maps),
		Message:        textPtr(details.Message),
		Status:         string(details.Status),
		HideFromPublic: details.HideFromPublic,
		HideSponsor:    details.HideSponsor,
		CreatedAt:      details.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      details.UpdatedAt.Format(time.RFC3339),
		Sponsor:        BuildStaffSponsorResponse(details.SponsorID, details.SponsorName, details.SponsorLogo, details.SponsorHidden, details.SponsorHideDonations, details.SponsorDeletedAt.Valid, details.SponsorEmail, details.SponsorPhone, details.SponsorAddress),
		CreatedBy:      createdByResp,
		Foods:          foods,
		Targets:        targets,
	}
}

func buildDonationPublicResponse(details dbgen.GetDonationDetailsByIDRow, foods []FoodResponse, targets []TargetResponse) DonationPublicResponse {
	isFull := details.HeroQuota > 0 && details.Remain == 0

	return DonationPublicResponse{
		ID:        details.ID.String(),
		EventAt:   details.EventAt.Format(time.RFC3339),
		HeroQuota: details.HeroQuota,
		Remain:    details.Remain,
		IsFull:    isFull,
		Address:   textPtr(details.Address),
		Maps:      textPtr(details.Maps),
		Message:   textPtr(details.Message),
		Status:    string(details.Status),
		CreatedAt: details.CreatedAt.Format(time.RFC3339),
		UpdatedAt: details.UpdatedAt.Format(time.RFC3339),
		Sponsor:   BuildPublicSponsorResponse(details.SponsorID, details.SponsorName, details.SponsorLogo, details.SponsorHidden || details.SponsorHideDonations || details.SponsorDeletedAt.Valid, details.HideSponsor),
		Foods:     foods,
		Targets:   targets,
	}
}

func (h *DonationHandler) CreateDonationHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateDonationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	sponsorUUID, eventTime, quota, heroQuota, hideFromPublic, hideSponsor, fieldErrors := ValidateCreateDonationRequest(req)
	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	var sponsorActive bool
	err := h.pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM sponsors WHERE id = $1 AND deleted_at IS NULL)", sponsorUUID).Scan(&sponsorActive)
	if err != nil || !sponsorActive {
		response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
			"sponsor_id": "Referenced sponsor does not exist or has been deleted",
		})
		return
	}

	foodItemIDs := make([]int32, 0, len(req.Foods))
	for _, f := range req.Foods {
		foodItemIDs = append(foodItemIDs, f.FoodItemID)
	}
	var foodCount int
	err = h.pool.QueryRow(r.Context(), "SELECT COUNT(*) FROM food_items WHERE id = ANY($1)", foodItemIDs).Scan(&foodCount)
	if err != nil || foodCount != len(foodItemIDs) {
		response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
			"foods": "One or more referenced food items do not exist",
		})
		return
	}

	targetUUIDs := make([]uuid.UUID, 0, len(req.BeneficiaryCategoryIDs))
	for _, tStr := range req.BeneficiaryCategoryIDs {
		u, _ := uuid.Parse(strings.TrimSpace(tStr))
		targetUUIDs = append(targetUUIDs, u)
	}

	hasHeroVariant := false
	foundCatCount := 0
	if len(targetUUIDs) > 0 {
		rows, err := h.pool.Query(r.Context(), "SELECT id, variant FROM beneficiary_categories WHERE id = ANY($1) AND deleted_at IS NULL", targetUUIDs)
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to verify beneficiary categories")
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id uuid.UUID
			var variant string
			if err := rows.Scan(&id, &variant); err != nil {
				response.InternalServerError(w, r, err, "Failed to scan beneficiary categories")
				return
			}
			foundCatCount++
			if variant == "university" || variant == "kipk" {
				hasHeroVariant = true
			}
		}
		if err := rows.Err(); err != nil {
			response.InternalServerError(w, r, err, "Failed to iterate beneficiary categories")
			return
		}

		if foundCatCount != len(targetUUIDs) {
			response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
				"beneficiary_category_ids": "One or more referenced beneficiary categories do not exist or have been deleted",
			})
			return
		}

		if !hasHeroVariant && heroQuota > 0 {
			response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
				"hero_quota": "Hero quota must be 0 when only organization categories are targeted",
			})
			return
		}
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	txQueries := h.queries.WithTx(tx)

	var addrParam, mapsParam, msgParam pgtype.Text
	if req.Address != nil && strings.TrimSpace(*req.Address) != "" {
		addrParam = pgtype.Text{String: strings.TrimSpace(*req.Address), Valid: true}
	}
	if req.Maps != nil && strings.TrimSpace(*req.Maps) != "" {
		mapsParam = pgtype.Text{String: strings.TrimSpace(*req.Maps), Valid: true}
	}
	if req.Message != nil && strings.TrimSpace(*req.Message) != "" {
		msgParam = pgtype.Text{String: strings.TrimSpace(*req.Message), Valid: true}
	}

	donation, err := txQueries.CreateDonation(r.Context(), dbgen.CreateDonationParams{
		SponsorID:      sponsorUUID,
		CreatedBy:      pgtype.UUID{Bytes: claims.UserID, Valid: true},
		EventAt:        eventTime,
		Quota:          quota,
		HeroQuota:      heroQuota,
		Remain:         heroQuota,
		Address:        addrParam,
		Maps:           mapsParam,
		Message:        msgParam,
		Status:         dbgen.DonationStatusDraft,
		HideFromPublic: hideFromPublic,
		HideSponsor:    hideSponsor,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create donation record")
		return
	}

	for _, f := range req.Foods {
		num, err := Float64ToNumeric(f.Quantity)
		if err != nil {
			response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
				"foods": "Invalid food quantity numeric format",
			})
			return
		}
		unitClean := strings.ToLower(strings.TrimSpace(f.Unit))
		_, err = txQueries.CreateFood(r.Context(), dbgen.CreateFoodParams{
			DonationID: donation.ID,
			FoodItemID: f.FoodItemID,
			Quantity:   num,
			Unit:       validFoodUnits[unitClean],
		})
		if err != nil {
			if response.HandleDBError(w, r, err) {
				return
			}
			response.InternalServerError(w, r, err, "Failed to attach food item to donation")
			return
		}
	}

	for _, catUUID := range targetUUIDs {
		_, err = txQueries.CreateDonationTarget(r.Context(), dbgen.CreateDonationTargetParams{
			DonationID:            donation.ID,
			BeneficiaryCategoryID: catUUID,
		})
		if err != nil {
			if response.HandleDBError(w, r, err) {
				return
			}
			response.InternalServerError(w, r, err, "Failed to attach target category to donation")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit donation creation")
		return
	}

	staffResp, err := h.getDonationStaffResponse(r.Context(), donation.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve created donation details")
		return
	}
	response.Success(w, http.StatusCreated, "Donation created successfully", staffResp)
}

func (h *DonationHandler) GetDonationByIDHandler(w http.ResponseWriter, r *http.Request) {
	idParam := strings.TrimSpace(r.PathValue("id"))
	donationUUID, err := uuid.Parse(idParam)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	details, err := h.queries.GetDonationDetailsByID(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation details")
		return
	}

	claims, ok := middleware.GetClaims(r.Context())
	isStaff := ok && claims != nil && claims.IsStaff

	if !isStaff {

		if details.HideFromPublic {
			response.NotFound(w, r, "Donation not found")
			return
		}

		if details.Status == dbgen.DonationStatusDraft || details.Status == dbgen.DonationStatusCancelled {
			response.NotFound(w, r, "Donation not found")
			return
		}

		if details.Status == dbgen.DonationStatusCompleted {
			cutoff := time.Now().Add(-h.cfg.Donation.PastDaysWindow)
			if details.EventAt.Before(cutoff) {
				response.NotFound(w, r, "Donation not found")
				return
			}
		}
	}

	foodsList, err := h.queries.ListFoodsByDonationID(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve donation food items")
		return
	}
	foodResponses := buildFoodResponses(foodsList)

	targetsList, err := h.queries.ListTargetsByDonationID(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve donation target categories")
		return
	}
	targetResponses := buildTargetResponses(targetsList, isStaff)

	if isStaff {
		staffResp := buildDonationStaffResponse(details, foodResponses, targetResponses)
		response.Success(w, http.StatusOK, "Donation retrieved successfully", staffResp)
		return
	}

	publicResp := buildDonationPublicResponse(details, foodResponses, targetResponses)
	response.Success(w, http.StatusOK, "Donation retrieved successfully", publicResp)
}

func (h *DonationHandler) UpdateDonationHandler(w http.ResponseWriter, r *http.Request) {
	idParam := strings.TrimSpace(r.PathValue("id"))
	donationUUID, err := uuid.Parse(idParam)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	var req UpdateDonationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	sponsorUUID, eventTime, fieldErrors := ValidateUpdateDonationRequest(req)
	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	txQueries := h.queries.WithTx(tx)

	current, err := txQueries.GetDonationByIDForUpdate(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation")
		return
	}

	if current.Status == dbgen.DonationStatusCompleted || current.Status == dbgen.DonationStatusCancelled {
		response.BadRequest(w, r, "VAL_001", "Cannot edit completed or cancelled donation", nil)
		return
	}

	finalQuota := current.Quota
	if req.Quota != nil {
		finalQuota = int32(*req.Quota)
	}

	finalHeroQuota := current.HeroQuota
	finalRemain := current.Remain
	if req.HeroQuota != nil {
		newHero := int32(*req.HeroQuota)
		delta := newHero - current.HeroQuota
		finalRemain = current.Remain + delta
		if finalRemain < 0 {
			response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
				"hero_quota": "New hero quota would drop remaining spots below zero",
			})
			return
		}
		finalHeroQuota = newHero
	}

	if finalHeroQuota > finalQuota {
		response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
			"hero_quota": "Hero quota cannot exceed total quota",
		})
		return
	}

	finalEventAt := current.EventAt
	if eventTime != nil {
		finalEventAt = *eventTime
	}

	if eventTime != nil && current.Status == dbgen.DonationStatusOpen && !finalEventAt.After(time.Now()) {
		response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
			"event_at": "Distribution event cannot be in the past for an open donation",
		})
		return
	}

	heroQuotaChanged := req.HeroQuota != nil && finalHeroQuota != current.HeroQuota
	finalStatus := current.Status
	if heroQuotaChanged {
		if current.Status == dbgen.DonationStatusOpen && finalRemain == 0 {
			finalStatus = dbgen.DonationStatusClosed
		} else if current.Status == dbgen.DonationStatusClosed && finalRemain > 0 {
			if finalEventAt.After(time.Now()) {
				finalStatus = dbgen.DonationStatusOpen
			}
		}
	}

	finalSponsorID := current.SponsorID
	if sponsorUUID != nil {
		var sponsorActive bool
		err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM sponsors WHERE id = $1 AND deleted_at IS NULL)", *sponsorUUID).Scan(&sponsorActive)
		if err != nil || !sponsorActive {
			response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
				"sponsor_id": "Referenced sponsor does not exist or has been deleted",
			})
			return
		}
		finalSponsorID = *sponsorUUID
	}

	finalAddress := current.Address
	if req.Address != nil {
		addrClean := strings.TrimSpace(*req.Address)
		finalAddress = pgtype.Text{String: addrClean, Valid: addrClean != ""}
	}

	finalMaps := current.Maps
	if req.Maps != nil {
		mapsClean := strings.TrimSpace(*req.Maps)
		finalMaps = pgtype.Text{String: mapsClean, Valid: mapsClean != ""}
	}

	finalMessage := current.Message
	if req.Message != nil {
		msgClean := strings.TrimSpace(*req.Message)
		finalMessage = pgtype.Text{String: msgClean, Valid: msgClean != ""}
	}

	finalHideFromPublic := current.HideFromPublic
	if req.HideFromPublic != nil {
		finalHideFromPublic = *req.HideFromPublic
	}

	finalHideSponsor := current.HideSponsor
	if req.HideSponsor != nil {
		finalHideSponsor = *req.HideSponsor
	}

	if req.BeneficiaryCategoryIDs != nil {
		targetUUIDs := make([]uuid.UUID, 0, len(req.BeneficiaryCategoryIDs))
		for _, tStr := range req.BeneficiaryCategoryIDs {
			u, _ := uuid.Parse(strings.TrimSpace(tStr))
			targetUUIDs = append(targetUUIDs, u)
		}

		hasHeroVariant := false
		foundCatCount := 0
		if len(targetUUIDs) > 0 {
			rows, err := tx.Query(r.Context(), "SELECT id, variant FROM beneficiary_categories WHERE id = ANY($1) AND deleted_at IS NULL", targetUUIDs)
			if err != nil {
				response.InternalServerError(w, r, err, "Failed to verify beneficiary categories")
				return
			}
			defer rows.Close()

			for rows.Next() {
				var id uuid.UUID
				var variant string
				if err := rows.Scan(&id, &variant); err != nil {
					response.InternalServerError(w, r, err, "Failed to scan beneficiary categories")
					return
				}
				foundCatCount++
				if variant == "university" || variant == "kipk" {
					hasHeroVariant = true
				}
			}
			if err := rows.Err(); err != nil {
				response.InternalServerError(w, r, err, "Failed to iterate beneficiary categories")
				return
			}

			if foundCatCount != len(targetUUIDs) {
				response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
					"beneficiary_category_ids": "One or more referenced beneficiary categories do not exist or have been deleted",
				})
				return
			}

			if !hasHeroVariant && finalHeroQuota > 0 {
				response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
					"hero_quota": "Hero quota must be 0 when only organization categories are targeted",
				})
				return
			}
		}

		if err := txQueries.DeleteTargetsByDonationID(r.Context(), donationUUID); err != nil {
			response.InternalServerError(w, r, err, "Failed to update target categories")
			return
		}

		for _, tid := range targetUUIDs {
			if _, err := txQueries.CreateDonationTarget(r.Context(), dbgen.CreateDonationTargetParams{
				DonationID:            donationUUID,
				BeneficiaryCategoryID: tid,
			}); err != nil {
				response.InternalServerError(w, r, err, "Failed to attach target category")
				return
			}
		}
	} else if req.HeroQuota != nil && finalHeroQuota > 0 {

		existingTargets, err := txQueries.ListTargetsByDonationID(r.Context(), donationUUID)
		if err == nil && len(existingTargets) > 0 {
			hasHeroVariant := false
			for _, et := range existingTargets {
				if et.CategoryVariant == "university" || et.CategoryVariant == "kipk" {
					hasHeroVariant = true
					break
				}
			}
			if !hasHeroVariant {
				response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
					"hero_quota": "Hero quota must be 0 when only organization categories are targeted",
				})
				return
			}
		}
	}

	if req.Foods != nil {
		foodItemIDs := make([]int32, 0, len(req.Foods))
		for _, f := range req.Foods {
			foodItemIDs = append(foodItemIDs, f.FoodItemID)
		}
		var foodCount int
		err = tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM food_items WHERE id = ANY($1)", foodItemIDs).Scan(&foodCount)
		if err != nil || foodCount != len(foodItemIDs) {
			response.BadRequest(w, r, "VAL_002", "Validation failed", map[string]string{
				"foods": "One or more referenced food items do not exist",
			})
			return
		}

		if err := txQueries.DeleteFoodsByDonationID(r.Context(), donationUUID); err != nil {
			response.InternalServerError(w, r, err, "Failed to update food items")
			return
		}

		for _, f := range req.Foods {
			qtyNumeric, err := Float64ToNumeric(f.Quantity)
			if err != nil {
				response.BadRequest(w, r, "VAL_001", "Invalid food quantity", nil)
				return
			}
			unitClean := dbgen.FoodUnit(strings.ToLower(strings.TrimSpace(f.Unit)))
			if _, err := txQueries.CreateFood(r.Context(), dbgen.CreateFoodParams{
				DonationID: donationUUID,
				FoodItemID: f.FoodItemID,
				Quantity:   qtyNumeric,
				Unit:       unitClean,
			}); err != nil {
				response.InternalServerError(w, r, err, "Failed to attach food item")
				return
			}
		}
	}

	_, err = txQueries.UpdateDonation(r.Context(), dbgen.UpdateDonationParams{
		ID:             donationUUID,
		SponsorID:      finalSponsorID,
		EventAt:        finalEventAt,
		Quota:          finalQuota,
		HeroQuota:      finalHeroQuota,
		Remain:         finalRemain,
		Address:        finalAddress,
		Maps:           finalMaps,
		Message:        finalMessage,
		Status:         finalStatus,
		HideFromPublic: finalHideFromPublic,
		HideSponsor:    finalHideSponsor,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to update donation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit transaction")
		return
	}

	staffResp, err := h.getDonationStaffResponse(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve updated donation details")
		return
	}
	response.Success(w, http.StatusOK, "Donation updated successfully", staffResp)
}

func (h *DonationHandler) getDonationStaffResponse(ctx context.Context, donationID uuid.UUID) (*DonationStaffResponse, error) {
	details, err := h.queries.GetDonationDetailsByID(ctx, donationID)
	if err != nil {
		return nil, err
	}

	foodsList, err := h.queries.ListFoodsByDonationID(ctx, donationID)
	if err != nil {
		return nil, err
	}
	foodResponses := buildFoodResponses(foodsList)

	targetsList, err := h.queries.ListTargetsByDonationID(ctx, donationID)
	if err != nil {
		return nil, err
	}
	targetResponses := buildTargetResponses(targetsList, true)

	staffResp := buildDonationStaffResponse(details, foodResponses, targetResponses)
	return &staffResp, nil
}

func (h *DonationHandler) PublishDonationHandler(w http.ResponseWriter, r *http.Request) {
	donationIDStr := r.PathValue("id")
	donationUUID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txQueries := h.queries.WithTx(tx)
	donation, err := txQueries.GetDonationByIDForUpdate(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation")
		return
	}

	if donation.Status != dbgen.DonationStatusDraft {
		response.BadRequest(w, r, "VAL_001", "Only draft donations can be published", map[string]string{
			"status": fmt.Sprintf("Current status is %s", donation.Status),
		})
		return
	}

	var sponsorDeletedAt pgtype.Timestamptz
	err = tx.QueryRow(r.Context(), "SELECT deleted_at FROM sponsors WHERE id = $1", donation.SponsorID).Scan(&sponsorDeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.BadRequest(w, r, "VAL_002", "Sponsor has been deleted", map[string]string{
				"sponsor_id": "Sponsor has been deleted",
			})
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify sponsor")
		return
	}
	if sponsorDeletedAt.Valid {
		response.BadRequest(w, r, "VAL_002", "Sponsor has been deleted", map[string]string{
			"sponsor_id": "Sponsor has been deleted",
		})
		return
	}

	if donation.Quota <= 0 {
		response.BadRequest(w, r, "VAL_001", "Donation quota must be greater than 0", nil)
		return
	}

	if !donation.EventAt.After(time.Now()) {
		response.BadRequest(w, r, "VAL_001", "Distribution event must be in the future to publish", map[string]string{
			"event_at": "Must be a future time",
		})
		return
	}

	foods, err := txQueries.ListFoodsByDonationID(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check donation food items")
		return
	}
	if len(foods) == 0 {
		response.BadRequest(w, r, "VAL_001", "Cannot publish donation without food items", map[string]string{
			"foods": "At least one food item is required",
		})
		return
	}

	targets, err := txQueries.ListTargetsByDonationID(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check donation targets")
		return
	}
	if len(targets) > 0 && donation.HeroQuota > 0 {
		hasHeroVariant := false
		for _, t := range targets {
			if t.CategoryVariant == "university" || t.CategoryVariant == "kipk" {
				hasHeroVariant = true
				break
			}
		}
		if !hasHeroVariant {
			response.BadRequest(w, r, "VAL_001", "Validation failed", map[string]string{
				"hero_quota": "Hero quota must be 0 when only organization categories are targeted",
			})
			return
		}
	}

	targetStatus := dbgen.DonationStatusOpen
	if donation.HeroQuota > 0 && donation.Remain == 0 {
		targetStatus = dbgen.DonationStatusClosed
	}

	if _, err := txQueries.UpdateDonationStatus(r.Context(), dbgen.UpdateDonationStatusParams{
		ID:     donationUUID,
		Status: targetStatus,
	}); err != nil {
		response.InternalServerError(w, r, err, "Failed to publish donation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit donation publish")
		return
	}

	staffResp, err := h.getDonationStaffResponse(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve donation details")
		return
	}

	response.Success(w, http.StatusOK, "Donation published successfully", staffResp)
}

func (h *DonationHandler) CloseDonationHandler(w http.ResponseWriter, r *http.Request) {
	donationIDStr := r.PathValue("id")
	donationUUID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txQueries := h.queries.WithTx(tx)
	donation, err := txQueries.GetDonationByIDForUpdate(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation")
		return
	}

	if donation.Status == dbgen.DonationStatusClosed {
		response.BadRequest(w, r, "VAL_001", "Donation is already closed", nil)
		return
	}

	if donation.Status != dbgen.DonationStatusOpen {
		response.BadRequest(w, r, "VAL_001", "Only open donations can be manually closed", map[string]string{
			"status": fmt.Sprintf("Current status is %s", donation.Status),
		})
		return
	}

	if _, err := txQueries.UpdateDonationStatus(r.Context(), dbgen.UpdateDonationStatusParams{
		ID:     donationUUID,
		Status: dbgen.DonationStatusClosed,
	}); err != nil {
		response.InternalServerError(w, r, err, "Failed to close donation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit donation close")
		return
	}

	staffResp, err := h.getDonationStaffResponse(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve donation details")
		return
	}

	response.Success(w, http.StatusOK, "Donation closed successfully", staffResp)
}

func (h *DonationHandler) ReopenDonationHandler(w http.ResponseWriter, r *http.Request) {
	donationIDStr := r.PathValue("id")
	donationUUID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txQueries := h.queries.WithTx(tx)
	donation, err := txQueries.GetDonationByIDForUpdate(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation")
		return
	}

	if donation.Status == dbgen.DonationStatusOpen {
		response.BadRequest(w, r, "VAL_001", "Donation is already open", nil)
		return
	}

	if donation.Status != dbgen.DonationStatusClosed {
		response.BadRequest(w, r, "VAL_001", "Only closed donations can be reopened", map[string]string{
			"status": fmt.Sprintf("Current status is %s", donation.Status),
		})
		return
	}

	if donation.HeroQuota > 0 && donation.Remain <= 0 {
		response.BadRequest(w, r, "VAL_001", "Cannot reopen donation with zero remaining quota", map[string]string{
			"remain": "Hero quota must be increased before reopening",
		})
		return
	}

	if !donation.EventAt.After(time.Now()) {
		response.BadRequest(w, r, "VAL_001", "Cannot reopen past donation event", map[string]string{
			"event_at": "Distribution event time has already passed",
		})
		return
	}

	if _, err := txQueries.UpdateDonationStatus(r.Context(), dbgen.UpdateDonationStatusParams{
		ID:     donationUUID,
		Status: dbgen.DonationStatusOpen,
	}); err != nil {
		response.InternalServerError(w, r, err, "Failed to reopen donation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit donation reopen")
		return
	}

	staffResp, err := h.getDonationStaffResponse(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve donation details")
		return
	}

	response.Success(w, http.StatusOK, "Donation reopened successfully", staffResp)
}

func (h *DonationHandler) CompleteDonationHandler(w http.ResponseWriter, r *http.Request) {
	donationIDStr := r.PathValue("id")
	donationUUID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txQueries := h.queries.WithTx(tx)
	donation, err := txQueries.GetDonationByIDForUpdate(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation")
		return
	}

	if donation.Status == dbgen.DonationStatusCompleted {
		response.BadRequest(w, r, "VAL_001", "Donation is already completed", nil)
		return
	}

	if donation.Status != dbgen.DonationStatusOpen && donation.Status != dbgen.DonationStatusClosed {
		response.BadRequest(w, r, "VAL_001", "Only open or closed donations can be completed", map[string]string{
			"status": fmt.Sprintf("Current status is %s", donation.Status),
		})
		return
	}

	if _, err := txQueries.UpdateDonationStatus(r.Context(), dbgen.UpdateDonationStatusParams{
		ID:     donationUUID,
		Status: dbgen.DonationStatusCompleted,
	}); err != nil {
		response.InternalServerError(w, r, err, "Failed to complete donation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit donation completion")
		return
	}

	staffResp, err := h.getDonationStaffResponse(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve donation details")
		return
	}

	response.Success(w, http.StatusOK, "Donation marked as completed", staffResp)
}

func (h *DonationHandler) CancelDonationHandler(w http.ResponseWriter, r *http.Request) {
	donationIDStr := r.PathValue("id")
	donationUUID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txQueries := h.queries.WithTx(tx)
	donation, err := txQueries.GetDonationByIDForUpdate(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation")
		return
	}

	if donation.Status == dbgen.DonationStatusCancelled {
		response.BadRequest(w, r, "VAL_001", "Donation is already cancelled", nil)
		return
	}

	if donation.Status == dbgen.DonationStatusCompleted {
		response.BadRequest(w, r, "VAL_001", "Cannot cancel completed donation", map[string]string{
			"status": "Current status is completed",
		})
		return
	}

	if _, err := txQueries.UpdateDonationStatus(r.Context(), dbgen.UpdateDonationStatusParams{
		ID:     donationUUID,
		Status: dbgen.DonationStatusCancelled,
	}); err != nil {
		response.InternalServerError(w, r, err, "Failed to cancel donation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit donation cancellation")
		return
	}

	staffResp, err := h.getDonationStaffResponse(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve donation details")
		return
	}

	response.Success(w, http.StatusOK, "Donation cancelled successfully", staffResp)
}

func (h *DonationHandler) DeleteDonationHandler(w http.ResponseWriter, r *http.Request) {
	donationIDStr := r.PathValue("id")
	donationUUID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid donation ID format", nil)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start database transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txQueries := h.queries.WithTx(tx)
	donation, err := txQueries.GetDonationByIDForUpdate(r.Context(), donationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve donation")
		return
	}

	if donation.Status != dbgen.DonationStatusDraft {
		response.BadRequest(w, r, "VAL_001", "Only draft donations can be deleted; non-draft donations must be cancelled instead", map[string]string{
			"status": fmt.Sprintf("Current status is %s", donation.Status),
		})
		return
	}

	row, err := txQueries.SoftDeleteDonation(r.Context(), donationUUID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to delete donation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit donation deletion")
		return
	}

	response.Success(w, http.StatusOK, "Donation deleted successfully", map[string]string{
		"id": row.ID.String(),
	})
}

func (h *DonationHandler) ListDonationsHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	page := 1
	if p := query.Get("page"); p != "" {
		if pVal, err := strconv.Atoi(p); err == nil && pVal > 0 {
			page = pVal
		}
	}

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
	if l := query.Get("limit"); l != "" {
		if lVal, err := strconv.Atoi(l); err == nil && lVal > 0 {
			if lVal > maxLimit {
				limit = maxLimit
			} else {
				limit = lVal
			}
		}
	}

	claims, ok := middleware.GetClaims(r.Context())
	isStaff := ok && claims != nil && claims.IsStaff

	if isStaff {

		var statusParam dbgen.NullDonationStatus
		if st := strings.TrimSpace(strings.ToLower(query.Get("status"))); st != "" {
			switch st {
			case string(dbgen.DonationStatusDraft):
				statusParam = dbgen.NullDonationStatus{DonationStatus: dbgen.DonationStatusDraft, Valid: true}
			case string(dbgen.DonationStatusOpen):
				statusParam = dbgen.NullDonationStatus{DonationStatus: dbgen.DonationStatusOpen, Valid: true}
			case string(dbgen.DonationStatusClosed):
				statusParam = dbgen.NullDonationStatus{DonationStatus: dbgen.DonationStatusClosed, Valid: true}
			case string(dbgen.DonationStatusCompleted):
				statusParam = dbgen.NullDonationStatus{DonationStatus: dbgen.DonationStatusCompleted, Valid: true}
			case string(dbgen.DonationStatusCancelled):
				statusParam = dbgen.NullDonationStatus{DonationStatus: dbgen.DonationStatusCancelled, Valid: true}
			default:
				response.BadRequest(w, r, "VAL_001", "Invalid status filter. Must be one of: draft, open, closed, completed, cancelled", nil)
				return
			}
		}

		var searchParam pgtype.Text
		if s := strings.TrimSpace(query.Get("search")); s != "" {
			searchParam = pgtype.Text{String: EscapeLikePattern(s), Valid: true}
		}

		total, err := h.queries.CountDonationsStaff(r.Context(), dbgen.CountDonationsStaffParams{
			Status: statusParam,
			Search: searchParam,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to count donations")
			return
		}

		offset := int32((page - 1) * limit)
		donations, err := h.queries.ListDonationsStaff(r.Context(), dbgen.ListDonationsStaffParams{
			Limit:  int32(limit),
			Offset: offset,
			Status: statusParam,
			Search: searchParam,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to list donations")
			return
		}

		donationIDs := make([]uuid.UUID, 0, len(donations))
		for _, d := range donations {
			donationIDs = append(donationIDs, d.ID)
		}

		foodsMap := make(map[uuid.UUID][]FoodResponse)
		targetsMap := make(map[uuid.UUID][]TargetResponse)

		if len(donationIDs) > 0 {
			allFoods, err := h.queries.ListFoodsByDonationIDs(r.Context(), donationIDs)
			if err != nil {
				response.InternalServerError(w, r, err, "Failed to batch retrieve donation food items")
				return
			}
			for _, f := range allFoods {
				foodsMap[f.DonationID] = append(foodsMap[f.DonationID], FoodResponse{
					FoodItemID: f.FoodItemID,
					Name:       f.FoodName,
					Quantity:   NumericToFloat64(f.Quantity),
					Unit:       string(f.Unit),
				})
			}

			allTargets, err := h.queries.ListTargetsByDonationIDs(r.Context(), donationIDs)
			if err != nil {
				response.InternalServerError(w, r, err, "Failed to batch retrieve donation target categories")
				return
			}
			for _, t := range allTargets {
				catID := t.BeneficiaryCategoryID.String()
				variant := string(t.CategoryVariant)
				targetsMap[t.DonationID] = append(targetsMap[t.DonationID], TargetResponse{
					BeneficiaryCategoryID: &catID,
					Name:                  t.CategoryName,
					Variant:               &variant,
				})
			}
		}

		data := make([]DonationStaffResponse, 0, len(donations))
		for _, d := range donations {
			fList := foodsMap[d.ID]
			if fList == nil {
				fList = []FoodResponse{}
			}
			tList := targetsMap[d.ID]
			if tList == nil {
				tList = []TargetResponse{}
			}

			var createdByResp *CreatedByResponse
			if d.CreatedBy.Valid {
				cID := uuid.UUID(d.CreatedBy.Bytes).String()
				var cEmail *string
				if d.CreatedByEmail.Valid {
					cEmail = &d.CreatedByEmail.String
				}
				createdByResp = &CreatedByResponse{
					ID:    cID,
					Email: cEmail,
				}
			}

			data = append(data, DonationStaffResponse{
				ID:             d.ID.String(),
				EventAt:        d.EventAt.Format(time.RFC3339),
				Quota:          d.Quota,
				HeroQuota:      d.HeroQuota,
				Remain:         d.Remain,
				Address:        textPtr(d.Address),
				Maps:           textPtr(d.Maps),
				Message:        textPtr(d.Message),
				Status:         string(d.Status),
				HideFromPublic: d.HideFromPublic,
				HideSponsor:    d.HideSponsor,
				CreatedAt:      d.CreatedAt.Format(time.RFC3339),
				UpdatedAt:      d.UpdatedAt.Format(time.RFC3339),
				Sponsor:        BuildStaffSponsorResponse(d.SponsorID, d.SponsorName, d.SponsorLogo, d.SponsorHidden, d.SponsorHideDonations, d.SponsorDeletedAt.Valid, d.SponsorEmail, d.SponsorPhone, d.SponsorAddress),
				CreatedBy:      createdByResp,
				Foods:          fList,
				Targets:        tList,
			})
		}

		totalPages := 0
		if total > 0 {
			totalPages = int(math.Ceil(float64(total) / float64(limit)))
		}
		meta := response.ResponseMeta{
			Page:       page,
			Limit:      limit,
			Total:      int(total),
			TotalPages: totalPages,
		}
		response.PaginatedSuccess(w, http.StatusOK, "Donations retrieved successfully", data, meta)
		return
	}

	st := strings.TrimSpace(strings.ToLower(query.Get("status")))
	if st != "" && st != "open" && st != "completed" {
		response.BadRequest(w, r, "VAL_001", "Public status filter must be 'open' or 'completed'", nil)
		return
	}

	offset := int32((page - 1) * limit)
	var total int64
	var donationIDs []uuid.UUID

	type publicDonationItem struct {
		ID            uuid.UUID
		EventAt       time.Time
		Quota         int32
		HeroQuota     int32
		Remain        int32
		Address       pgtype.Text
		Maps          pgtype.Text
		Message       pgtype.Text
		Status        dbgen.DonationStatus
		HideSponsor   bool
		CreatedAt     time.Time
		UpdatedAt     time.Time
		SponsorID     uuid.UUID
		SponsorName   string
		SponsorLogo   pgtype.UUID
		SponsorHidden bool
	}

	var items []publicDonationItem

	if st == "completed" {
		cutoff := time.Now().Add(-h.cfg.Donation.PastDaysWindow)
		tCount, err := h.queries.CountDonationsPublicPast(r.Context(), cutoff)
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to count past donations")
			return
		}
		total = tCount

		pastDonations, err := h.queries.ListDonationsPublicPast(r.Context(), dbgen.ListDonationsPublicPastParams{
			EventAt: cutoff,
			Limit:   int32(limit),
			Offset:  offset,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to list past donations")
			return
		}

		for _, d := range pastDonations {
			donationIDs = append(donationIDs, d.ID)
			items = append(items, publicDonationItem{
				ID:            d.ID,
				EventAt:       d.EventAt,
				Quota:         d.Quota,
				HeroQuota:     d.HeroQuota,
				Remain:        d.Remain,
				Address:       d.Address,
				Maps:          d.Maps,
				Message:       d.Message,
				Status:        d.Status,
				HideSponsor:   d.HideSponsor,
				CreatedAt:     d.CreatedAt,
				UpdatedAt:     d.UpdatedAt,
				SponsorID:     d.SponsorID,
				SponsorName:   d.SponsorName,
				SponsorLogo:   d.SponsorLogo,
				SponsorHidden: d.SponsorHidden,
			})
		}
	} else {

		tCount, err := h.queries.CountDonationsPublicOpen(r.Context())
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to count active donations")
			return
		}
		total = tCount

		openDonations, err := h.queries.ListDonationsPublicOpen(r.Context(), dbgen.ListDonationsPublicOpenParams{
			Limit:  int32(limit),
			Offset: offset,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to list active donations")
			return
		}

		for _, d := range openDonations {
			donationIDs = append(donationIDs, d.ID)
			items = append(items, publicDonationItem{
				ID:            d.ID,
				EventAt:       d.EventAt,
				Quota:         d.Quota,
				HeroQuota:     d.HeroQuota,
				Remain:        d.Remain,
				Address:       d.Address,
				Maps:          d.Maps,
				Message:       d.Message,
				Status:        d.Status,
				HideSponsor:   d.HideSponsor,
				CreatedAt:     d.CreatedAt,
				UpdatedAt:     d.UpdatedAt,
				SponsorID:     d.SponsorID,
				SponsorName:   d.SponsorName,
				SponsorLogo:   d.SponsorLogo,
				SponsorHidden: d.SponsorHidden,
			})
		}
	}

	foodsMap := make(map[uuid.UUID][]FoodResponse)
	targetsMap := make(map[uuid.UUID][]TargetResponse)

	if len(donationIDs) > 0 {
		allFoods, err := h.queries.ListFoodsByDonationIDs(r.Context(), donationIDs)
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to batch retrieve donation food items")
			return
		}
		for _, f := range allFoods {
			foodsMap[f.DonationID] = append(foodsMap[f.DonationID], FoodResponse{
				FoodItemID: f.FoodItemID,
				Name:       f.FoodName,
				Quantity:   NumericToFloat64(f.Quantity),
				Unit:       string(f.Unit),
			})
		}

		allTargets, err := h.queries.ListTargetsByDonationIDs(r.Context(), donationIDs)
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to batch retrieve donation target categories")
			return
		}
		hasHiddenByDonation := make(map[uuid.UUID]bool)
		for _, t := range allTargets {
			if t.CategoryHidden {
				hasHiddenByDonation[t.DonationID] = true
				continue
			}
			catID := t.BeneficiaryCategoryID.String()
			variant := string(t.CategoryVariant)
			targetsMap[t.DonationID] = append(targetsMap[t.DonationID], TargetResponse{
				BeneficiaryCategoryID: &catID,
				Name:                  t.CategoryName,
				Variant:               &variant,
			})
		}
		for donID, hasHidden := range hasHiddenByDonation {
			if hasHidden {
				targetsMap[donID] = append(targetsMap[donID], TargetResponse{
					BeneficiaryCategoryID: nil,
					Name:                  "Hidden institution",
					Variant:               nil,
				})
			}
		}
	}

	data := make([]DonationPublicResponse, 0, len(items))
	for _, it := range items {
		fList := foodsMap[it.ID]
		if fList == nil {
			fList = []FoodResponse{}
		}
		tList := targetsMap[it.ID]
		if tList == nil {
			tList = []TargetResponse{}
		}

		isFull := it.HeroQuota > 0 && it.Remain == 0

		data = append(data, DonationPublicResponse{
			ID:        it.ID.String(),
			EventAt:   it.EventAt.Format(time.RFC3339),
			HeroQuota: it.HeroQuota,
			Remain:    it.Remain,
			IsFull:    isFull,
			Address:   textPtr(it.Address),
			Maps:      textPtr(it.Maps),
			Message:   textPtr(it.Message),
			Status:    string(it.Status),
			CreatedAt: it.CreatedAt.Format(time.RFC3339),
			UpdatedAt: it.UpdatedAt.Format(time.RFC3339),
			Sponsor:   BuildPublicSponsorResponse(it.SponsorID, it.SponsorName, it.SponsorLogo, it.SponsorHidden, it.HideSponsor),
			Foods:     fList,
			Targets:   tList,
		})
	}

	totalPages := 0
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}
	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(total),
		TotalPages: totalPages,
	}
	response.PaginatedSuccess(w, http.StatusOK, "Donations retrieved successfully", data, meta)
}

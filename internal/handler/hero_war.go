package handler

import (
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

type HeroWarHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewHeroWarHandler(pool *pgxpool.Pool, cfg *config.Config) *HeroWarHandler {
	return &HeroWarHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type HeroReservationResponse struct {
	ID             uuid.UUID  `json:"id"`
	DonationID     uuid.UUID  `json:"donation_id"`
	HeroID         uuid.UUID  `json:"hero_id"`
	Status         string     `json:"status"`
	CancelledLate  bool       `json:"cancelled_late"`
	CancelledAt    *time.Time `json:"cancelled_at"`
	AttendedAt     *time.Time `json:"attended_at"`
	Notes          *string    `json:"notes"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	EventAt        time.Time  `json:"event_at"`
	Address        *string    `json:"address"`
	Maps           *string    `json:"maps"`
	Message        *string    `json:"message"`
	DonationStatus string     `json:"donation_status"`
	SponsorName    string     `json:"sponsor_name"`
}

func (h *HeroWarHandler) ClaimDonationHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsHero {
		response.Forbidden(w, r, "Hero access required")
		return
	}

	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	bans, err := h.queries.GetActiveHeroBans(ctx, claims.UserID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to evaluate ban status")
		return
	}
	for _, ban := range bans {
		count, err := h.queries.CountCompletedEligibleDonationsForHero(ctx, dbgen.CountCompletedEligibleDonationsForHeroParams{
			HeroID:     claims.UserID,
			BaselineAt: ban.BaselineAt,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to evaluate completed donations for ban")
			return
		}
		if count < ban.DonationCount {
			response.ForbiddenWithCode(w, r, "WAR_002", "Account is temporarily suspended from reserving food packages")
			return
		}
	}

	hasTarget, err := h.queries.HasDonationTargets(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check donation targets")
		return
	}
	if hasTarget {
		eligible, err := h.queries.IsHeroEligibleForDonation(ctx, dbgen.IsHeroEligibleForDonationParams{
			DonationID: donationID,
			HeroID:     claims.UserID,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to check category eligibility")
			return
		}
		if !eligible {
			response.Forbidden(w, r, "You are not eligible for this targeted donation event")
			return
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	don, err := qtx.GetDonationByIDForUpdate(ctx, donationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to query donation")
		return
	}

	if don.Status != dbgen.DonationStatusOpen || !don.EventAt.After(time.Now()) {
		response.BadRequest(w, r, "VAL_002", "Donation event is not open for reservations", nil)
		return
	}

	if don.Remain <= 0 {
		response.Conflict(w, r, "WAR_001", "No slots remaining for this donation")
		return
	}

	_, err = qtx.GetActiveHeroAttendance(ctx, dbgen.GetActiveHeroAttendanceParams{
		DonationID: donationID,
		HeroID:     claims.UserID,
	})
	if err == nil {
		response.Conflict(w, r, "ALREADY_CLAIMED", "You already have an active reservation for this donation")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		response.InternalServerError(w, r, err, "Failed to check existing reservation")
		return
	}

	_, err = qtx.DecrementDonationRemain(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to decrement remaining quota")
		return
	}

	attendance, err := qtx.CreateHeroAttendance(ctx, dbgen.CreateHeroAttendanceParams{
		DonationID: donationID,
		HeroID:     claims.UserID,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to create reservation")
		return
	}

	if err := qtx.ClearHeroRemindMeAt(ctx, claims.UserID); err != nil {
		response.InternalServerError(w, r, err, "Failed to clear reminder preference")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit transaction")
		return
	}

	response.Success(w, http.StatusCreated, "Reservation created successfully", toHeroAttendanceResponse(attendance))
}

func (h *HeroWarHandler) CancelClaimHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsHero {
		response.Forbidden(w, r, "Hero access required")
		return
	}

	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	don, err := qtx.GetDonationByIDForUpdate(ctx, donationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lock donation")
		return
	}

	now := time.Now()

	if !don.EventAt.After(now) {
		response.BadRequest(w, r, "VAL_002", "Cannot cancel reservation after distribution event has started", nil)
		return
	}

	att, err := qtx.GetActiveHeroAttendanceForUpdate(ctx, dbgen.GetActiveHeroAttendanceForUpdateParams{
		DonationID: donationID,
		HeroID:     claims.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Active reservation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lock reservation")
		return
	}

	if att.Status == dbgen.AttendanceStatusAttended {
		response.BadRequest(w, r, "VAL_002", "Cannot cancel reservation after attendance has been recorded", nil)
		return
	}
	if att.Status != dbgen.AttendanceStatusRegistered {
		response.BadRequest(w, r, "VAL_002", "Only registered reservations can be cancelled", nil)
		return
	}

	isLate := false
	if don.Status != dbgen.DonationStatusCancelled {
		if now.After(don.EventAt.Add(-h.cfg.War.LateCancellationWindow)) {
			hadPriorCancel, err := qtx.HasPriorCancelledHeroAttendance(ctx, dbgen.HasPriorCancelledHeroAttendanceParams{
				DonationID: donationID,
				HeroID:     claims.UserID,
			})
			if err != nil {
				response.InternalServerError(w, r, err, "Failed to check prior cancellation history")
				return
			}

			if hadPriorCancel || now.After(att.CreatedAt.Add(h.cfg.War.HeroCancelGracePeriod)) {
				isLate = true
			}
		}
	}

	if isLate {
		_, err = qtx.UpsertHeroBanOnLateCancel(ctx, dbgen.UpsertHeroBanOnLateCancelParams{
			UserID:                 claims.UserID,
			DonationID:             pgtype.UUID{Bytes: donationID, Valid: true},
			DonationCount:          int32(h.cfg.War.HeroBanEventCount),
			SourceHeroAttendanceID: pgtype.UUID{Bytes: att.ID, Valid: true},
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to record late cancellation penalty")
			return
		}
	}

	_, err = qtx.CancelHeroAttendance(ctx, dbgen.CancelHeroAttendanceParams{
		ID:            att.ID,
		CancelledLate: isLate,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to cancel reservation")
		return
	}

	_, err = qtx.IncrementDonationRemain(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to restore donation quota")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit transaction")
		return
	}

	response.Success(w, http.StatusOK, "Reservation cancelled successfully", map[string]any{
		"cancelled_late": isLate,
	})
}

func (h *HeroWarHandler) ListReservationsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsHero {
		response.Forbidden(w, r, "Hero access required")
		return
	}

	page := 1
	limit := 20
	if pStr := r.URL.Query().Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	offset := (page - 1) * limit

	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	var statusParam dbgen.NullAttendanceStatus
	switch statusFilter {
	case "":
	case "registered", "attended", "absent", "cancelled":
		statusParam = dbgen.NullAttendanceStatus{AttendanceStatus: dbgen.AttendanceStatus(statusFilter), Valid: true}
	default:
		response.BadRequest(w, r, "VAL_001", "Invalid status filter; must be one of registered, attended, absent, cancelled", nil)
		return
	}

	totalItems, err := h.queries.CountHeroReservations(ctx, dbgen.CountHeroReservationsParams{
		HeroID: claims.UserID,
		Status: statusParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count reservations")
		return
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(limit)))
	}

	rows, err := h.queries.ListHeroReservations(ctx, dbgen.ListHeroReservationsParams{
		HeroID: claims.UserID,
		Status: statusParam,
		Offset: int32(offset),
		Limit:  int32(limit),
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to query reservations")
		return
	}

	resp := make([]HeroReservationResponse, 0, len(rows))
	for _, row := range rows {
		item := HeroReservationResponse{
			ID:             row.ID,
			DonationID:     row.DonationID,
			HeroID:         row.HeroID,
			Status:         string(row.Status),
			CancelledLate:  row.CancelledLate,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
			EventAt:        row.EventAt,
			DonationStatus: string(row.DonationStatus),
			SponsorName:    row.SponsorName,
		}
		if row.CancelledAt.Valid {
			t := row.CancelledAt.Time
			item.CancelledAt = &t
		}
		if row.AttendedAt.Valid {
			t := row.AttendedAt.Time
			item.AttendedAt = &t
		}
		if row.Notes.Valid {
			s := row.Notes.String
			item.Notes = &s
		}
		if row.Address.Valid {
			s := row.Address.String
			item.Address = &s
		}
		if row.Maps.Valid {
			s := row.Maps.String
			item.Maps = &s
		}
		if row.Message.Valid {
			s := row.Message.String
			item.Message = &s
		}
		resp = append(resp, item)
	}

	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(totalItems),
		TotalPages: totalPages,
	}
	response.PaginatedSuccess(w, http.StatusOK, "Reservations retrieved successfully", resp, meta)
}

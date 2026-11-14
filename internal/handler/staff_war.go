package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
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

type StaffWarHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewStaffWarHandler(pool *pgxpool.Pool, cfg *config.Config) *StaffWarHandler {
	return &StaffWarHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type ConfigureWarSlotItem struct {
	DivisionID int32 `json:"division_id"`
	Quota      int32 `json:"quota"`
}

type UpdateWarSlotRequest struct {
	Quota int32 `json:"quota"`
}

type StaffVolunteerResponse struct {
	ID            int64      `json:"id"`
	StaffID       uuid.UUID  `json:"staff_id"`
	StaffName     string     `json:"staff_name"`
	StaffEmail    string     `json:"staff_email"`
	Status        string     `json:"status"`
	CheckinSource *string    `json:"checkin_source"`
	AttendedAt    *time.Time `json:"attended_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type ConfiguredWarSlotResponse struct {
	ID           int64     `json:"id"`
	DonationID   uuid.UUID `json:"donation_id"`
	DivisionID   int32     `json:"division_id"`
	DivisionName string    `json:"division_name"`
	Quota        int32     `json:"quota"`
	Remain       int32     `json:"remain"`
	ActiveClaims int32     `json:"active_claims"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type WarSlotResponse struct {
	ID           int64                    `json:"id"`
	DonationID   uuid.UUID                `json:"donation_id"`
	DivisionID   int32                    `json:"division_id"`
	DivisionName string                   `json:"division_name"`
	Quota        int32                    `json:"quota"`
	Remain       int32                    `json:"remain"`
	ActiveClaims int32                    `json:"active_claims"`
	Volunteers   []StaffVolunteerResponse `json:"volunteers"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

func (h *StaffWarHandler) ConfigureWarSlotsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	var items []ConfigureWarSlotItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body format", nil)
		return
	}

	if len(items) == 0 {
		response.BadRequest(w, r, "VAL_001", "At least one slot configuration is required", nil)
		return
	}

	seenDivisions := make(map[int32]bool)
	for _, item := range items {
		if item.DivisionID <= 0 {
			response.BadRequest(w, r, "VAL_001", "Division ID must be a positive integer", nil)
			return
		}
		if item.Quota <= 0 {
			response.BadRequest(w, r, "VAL_001", fmt.Sprintf("Quota for division %d must be greater than zero", item.DivisionID), nil)
			return
		}
		if seenDivisions[item.DivisionID] {
			response.BadRequest(w, r, "VAL_001", fmt.Sprintf("Duplicate division ID %d in request", item.DivisionID), nil)
			return
		}
		seenDivisions[item.DivisionID] = true
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].DivisionID < items[j].DivisionID
	})

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	don, err := qtx.GetDonationForShare(ctx, donationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to check donation")
		return
	}

	if don.Status != dbgen.DonationStatusDraft && don.Status != dbgen.DonationStatusOpen {
		response.BadRequest(w, r, "VAL_002", "War slots can only be configured for draft or open donations", nil)
		return
	}

	for _, item := range items {
		if _, err := qtx.GetDivisionByID(ctx, item.DivisionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				response.BadRequest(w, r, "VAL_002", fmt.Sprintf("Division %d does not exist", item.DivisionID), nil)
				return
			}
			response.InternalServerError(w, r, err, "Failed to verify division")
			return
		}

		existingSlot, err := qtx.GetWarSlotByDonationAndDivisionForUpdate(ctx, dbgen.GetWarSlotByDonationAndDivisionForUpdateParams{
			DonationID: donationID,
			DivisionID: item.DivisionID,
		})

		var activeClaims int32
		if err == nil {
			activeClaims = existingSlot.Quota - existingSlot.Remain
			if item.Quota < activeClaims {
				response.Conflict(w, r, "WAR_003", fmt.Sprintf("New quota (%d) cannot be lower than active claims (%d) for division %d", item.Quota, activeClaims, item.DivisionID))
				return
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			response.InternalServerError(w, r, err, "Failed to query existing war slot")
			return
		}

		remain := item.Quota - activeClaims

		_, err = qtx.UpsertWarSlot(ctx, dbgen.UpsertWarSlotParams{
			DonationID: donationID,
			DivisionID: item.DivisionID,
			Quota:      item.Quota,
			Remain:     remain,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to configure war slot")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit war slot configurations")
		return
	}

	slots, err := h.queries.ListWarSlotsByDonation(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to query configured slots")
		return
	}

	resp := make([]ConfiguredWarSlotResponse, 0, len(slots))
	for _, s := range slots {
		resp = append(resp, ConfiguredWarSlotResponse{
			ID:           s.ID,
			DonationID:   s.DonationID,
			DivisionID:   s.DivisionID,
			DivisionName: s.DivisionName,
			Quota:        s.Quota,
			Remain:       s.Remain,
			ActiveClaims: s.ActiveClaims,
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
		})
	}

	response.Success(w, http.StatusOK, "War slots configured successfully", resp)
}

func (h *StaffWarHandler) UpdateWarSlotHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	donationID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	slotID, err := strconv.ParseInt(r.PathValue("slot_id"), 10, 64)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid slot ID", nil)
		return
	}

	var req UpdateWarSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body format", nil)
		return
	}
	if req.Quota <= 0 {
		response.BadRequest(w, r, "VAL_001", "Quota must be greater than zero", nil)
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	don, err := qtx.GetDonationForShare(ctx, donationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to check donation")
		return
	}

	if don.Status != dbgen.DonationStatusDraft && don.Status != dbgen.DonationStatusOpen {
		response.BadRequest(w, r, "VAL_002", "War slots can only be updated for draft or open donations", nil)
		return
	}

	existingSlot, err := qtx.GetWarSlotByIDForUpdate(ctx, slotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "War slot not found for this donation")
			return
		}
		response.InternalServerError(w, r, err, "Failed to query war slot")
		return
	}
	if existingSlot.DonationID != donationID {
		response.NotFound(w, r, "War slot not found for this donation")
		return
	}

	activeClaims := existingSlot.Quota - existingSlot.Remain
	if req.Quota < activeClaims {
		response.Conflict(w, r, "WAR_003", fmt.Sprintf("New quota (%d) cannot be lower than active claims (%d) for division %d", req.Quota, activeClaims, existingSlot.DivisionID))
		return
	}

	remain := req.Quota - activeClaims
	if _, err := qtx.UpdateWarSlotQuota(ctx, dbgen.UpdateWarSlotQuotaParams{
		ID:     slotID,
		Quota:  req.Quota,
		Remain: remain,
	}); err != nil {
		response.InternalServerError(w, r, err, "Failed to update war slot")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit war slot update")
		return
	}

	slot, err := h.queries.GetConfiguredWarSlotByID(ctx, slotID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to query updated war slot")
		return
	}

	response.Success(w, http.StatusOK, "War slot updated successfully", ConfiguredWarSlotResponse{
		ID:           slot.ID,
		DonationID:   slot.DonationID,
		DivisionID:   slot.DivisionID,
		DivisionName: slot.DivisionName,
		Quota:        slot.Quota,
		Remain:       slot.Remain,
		ActiveClaims: slot.ActiveClaims,
		CreatedAt:    slot.CreatedAt,
		UpdatedAt:    slot.UpdatedAt,
	})
}

func (h *StaffWarHandler) DeleteWarSlotHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	donationID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	slotID, err := strconv.ParseInt(r.PathValue("slot_id"), 10, 64)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid slot ID", nil)
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	don, err := qtx.GetDonationForShare(ctx, donationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to check donation")
		return
	}

	if don.Status != dbgen.DonationStatusDraft && don.Status != dbgen.DonationStatusOpen {
		response.BadRequest(w, r, "VAL_002", "War slots can only be deleted for draft or open donations", nil)
		return
	}

	slot, err := qtx.GetWarSlotByIDForUpdate(ctx, slotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "War slot not found for this donation")
			return
		}
		response.InternalServerError(w, r, err, "Failed to query war slot")
		return
	}
	if slot.DonationID != donationID {
		response.NotFound(w, r, "War slot not found for this donation")
		return
	}

	activeClaims, err := qtx.CountActiveStaffAttendancesBySlot(ctx, slotID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check active reservations for war slot")
		return
	}
	if activeClaims > 0 {
		response.Conflict(w, r, "WAR_004", "Cannot delete war slot with active staff reservations")
		return
	}

	rowsAffected, err := qtx.SoftDeleteWarSlot(ctx, slotID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to delete war slot")
		return
	}
	if rowsAffected == 0 {
		response.NotFound(w, r, "War slot not found for this donation")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit war slot deletion")
		return
	}

	response.Success(w, http.StatusOK, "War slot deleted successfully", map[string]any{
		"id": slotID,
	})
}

func (h *StaffWarHandler) ListWarSlotsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	exists, err := h.queries.DonationExists(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to verify donation")
		return
	}
	if !exists {
		response.NotFound(w, r, "Donation not found")
		return
	}

	slots, err := h.queries.ListWarSlotsByDonation(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to query war slots")
		return
	}

	volRows, err := h.queries.ListActiveStaffVolunteersByDonation(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to query staff shift volunteers")
		return
	}

	volunteersBySlot := make(map[int64][]StaffVolunteerResponse)
	for _, row := range volRows {
		v := StaffVolunteerResponse{
			ID:         row.ID,
			StaffID:    row.StaffID,
			StaffName:  row.StaffName,
			StaffEmail: row.StaffEmail,
			Status:     string(row.Status),
			CreatedAt:  row.CreatedAt,
		}
		if row.CheckinSource.Valid {
			s := row.CheckinSource.String
			v.CheckinSource = &s
		}
		if row.AttendedAt.Valid {
			t := row.AttendedAt.Time
			v.AttendedAt = &t
		}
		volunteersBySlot[row.WarSlotID] = append(volunteersBySlot[row.WarSlotID], v)
	}

	resp := make([]WarSlotResponse, 0, len(slots))
	for _, s := range slots {
		vols := volunteersBySlot[s.ID]
		if vols == nil {
			vols = []StaffVolunteerResponse{}
		}
		resp = append(resp, WarSlotResponse{
			ID:           s.ID,
			DonationID:   s.DonationID,
			DivisionID:   s.DivisionID,
			DivisionName: s.DivisionName,
			Quota:        s.Quota,
			Remain:       s.Remain,
			ActiveClaims: s.ActiveClaims,
			Volunteers:   vols,
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
		})
	}

	response.Success(w, http.StatusOK, "War slots retrieved successfully", resp)
}

func (h *StaffWarHandler) ClaimStaffSlotHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsStaff {
		response.Forbidden(w, r, "Staff access required")
		return
	}

	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	slotIDStr := r.PathValue("slot_id")
	slotID, err := strconv.ParseInt(slotIDStr, 10, 64)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid slot ID", nil)
		return
	}

	staffProfile, err := h.queries.GetStaffByUserID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Forbidden(w, r, "Staff profile not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to query staff division")
		return
	}
	staffDivisionID := staffProfile.DivisionID

	bans, err := h.queries.GetActiveStaffBans(ctx, claims.UserID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to evaluate staff ban status")
		return
	}
	for _, ban := range bans {
		count, err := h.queries.CountCompletedEligibleDonationsForStaff(ctx, dbgen.CountCompletedEligibleDonationsForStaffParams{
			StaffID:    claims.UserID,
			DivisionID: staffDivisionID,
			BaselineAt: ban.BaselineAt,
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to evaluate completed staff events for ban")
			return
		}
		if count < ban.DonationCount {
			response.ForbiddenWithCode(w, r, "WAR_002", "Account is temporarily suspended from staff shifts")
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

	don, err := qtx.GetDonationForShare(ctx, donationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to query donation")
		return
	}

	if don.Status != dbgen.DonationStatusOpen || !don.EventAt.After(time.Now()) {
		response.BadRequest(w, r, "VAL_002", "Donation event is not open for shift claims", nil)
		return
	}

	slot, err := qtx.GetWarSlotByIDForUpdate(ctx, slotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "War slot not found for this donation")
			return
		}
		response.InternalServerError(w, r, err, "Failed to query war slot")
		return
	}
	if slot.DonationID != donationID {
		response.NotFound(w, r, "War slot not found for this donation")
		return
	}

	if slot.DivisionID != staffDivisionID {
		response.Forbidden(w, r, "You can only claim shifts for your own division")
		return
	}

	if slot.Remain <= 0 {
		response.Conflict(w, r, "WAR_001", "No shift slots remaining for this division")
		return
	}

	_, err = qtx.GetActiveStaffAttendance(ctx, dbgen.GetActiveStaffAttendanceParams{
		WarSlotID: slotID,
		StaffID:   claims.UserID,
	})
	if err == nil {
		response.Conflict(w, r, "ALREADY_CLAIMED", "You already have an active shift claim for this slot")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		response.InternalServerError(w, r, err, "Failed to check existing shift claim")
		return
	}

	_, err = qtx.DecrementWarSlotRemain(ctx, slotID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to decrement slot quota")
		return
	}

	attendance, err := qtx.CreateStaffAttendance(ctx, dbgen.CreateStaffAttendanceParams{
		WarSlotID: slotID,
		StaffID:   claims.UserID,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to create staff shift attendance")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit shift claim")
		return
	}

	response.Success(w, http.StatusCreated, "Shift claimed successfully", toStaffAttendanceResponse(attendance))
}

func (h *StaffWarHandler) CancelStaffSlotHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsStaff {
		response.Forbidden(w, r, "Staff access required")
		return
	}

	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	slotIDStr := r.PathValue("slot_id")
	slotID, err := strconv.ParseInt(slotIDStr, 10, 64)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid slot ID", nil)
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	don, err := qtx.GetDonationForShare(ctx, donationID)
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
		response.BadRequest(w, r, "VAL_002", "Cannot cancel shift reservation after distribution event has started", nil)
		return
	}

	slot, err := qtx.GetWarSlotByIDForUpdate(ctx, slotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "War slot not found for this donation")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lock war slot")
		return
	}
	if slot.DonationID != donationID {
		response.NotFound(w, r, "War slot not found for this donation")
		return
	}

	att, err := qtx.GetActiveStaffAttendanceForUpdate(ctx, dbgen.GetActiveStaffAttendanceForUpdateParams{
		WarSlotID: slotID,
		StaffID:   claims.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Active shift reservation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lock shift reservation")
		return
	}

	if att.Status == dbgen.AttendanceStatusAttended {
		response.BadRequest(w, r, "VAL_002", "Cannot cancel shift reservation after attendance has been recorded", nil)
		return
	}
	if att.Status != dbgen.AttendanceStatusRegistered {
		response.BadRequest(w, r, "VAL_002", "Only registered shift reservations can be cancelled", nil)
		return
	}

	isLate := false
	if don.Status != dbgen.DonationStatusCancelled {
		if now.After(don.EventAt.Add(-h.cfg.War.LateCancellationWindow)) {
			hadPriorCancel, err := qtx.HasPriorCancelledStaffAttendance(ctx, dbgen.HasPriorCancelledStaffAttendanceParams{
				DonationID: donationID,
				StaffID:    claims.UserID,
			})
			if err != nil {
				response.InternalServerError(w, r, err, "Failed to check prior cancellation history")
				return
			}

			if hadPriorCancel || now.After(att.CreatedAt.Add(h.cfg.War.StaffCancelGracePeriod)) {
				isLate = true
			}
		}
	}

	if isLate {
		_, err = qtx.UpsertStaffBanOnLateCancel(ctx, dbgen.UpsertStaffBanOnLateCancelParams{
			UserID:                  claims.UserID,
			DonationID:              pgtype.UUID{Bytes: donationID, Valid: true},
			DonationCount:           int32(h.cfg.War.StaffBanEventCount),
			SourceStaffAttendanceID: pgtype.Int8{Int64: att.ID, Valid: true},
		})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to record late cancellation penalty")
			return
		}
	}

	_, err = qtx.CancelStaffAttendance(ctx, dbgen.CancelStaffAttendanceParams{
		ID:            att.ID,
		CancelledLate: isLate,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to cancel shift reservation")
		return
	}

	_, err = qtx.IncrementWarSlotRemain(ctx, slotID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to restore slot quota")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit shift cancellation")
		return
	}

	response.Success(w, http.StatusOK, "Shift reservation cancelled successfully", map[string]any{
		"cancelled_late": isLate,
	})
}

func (h *StaffWarHandler) StaffSelfCheckInHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsStaff {
		response.Forbidden(w, r, "Staff access required")
		return
	}

	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	slotIDStr := r.PathValue("slot_id")
	slotID, err := strconv.ParseInt(slotIDStr, 10, 64)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid slot ID", nil)
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	don, err := qtx.GetDonationForShare(ctx, donationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Donation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to verify donation")
		return
	}

	if don.Status == dbgen.DonationStatusDraft || don.Status == dbgen.DonationStatusCancelled {
		response.BadRequest(w, r, "VAL_002", "Self check-in is not allowed for draft or cancelled donations", nil)
		return
	}

	now := time.Now()
	if now.Before(don.EventAt.Add(-h.cfg.War.StaffSelfCheckInBefore)) || now.After(don.EventAt.Add(h.cfg.War.StaffSelfCheckInAfter)) {
		msg := fmt.Sprintf(
			"Self check-in is only allowed between %s before and %s after the event start time",
			formatWindowDuration(h.cfg.War.StaffSelfCheckInBefore),
			formatWindowDuration(h.cfg.War.StaffSelfCheckInAfter),
		)
		response.BadRequest(w, r, "VAL_002", msg, nil)
		return
	}

	slot, err := qtx.GetWarSlotByIDForUpdate(ctx, slotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "War slot not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lock war slot")
		return
	}
	if slot.DonationID != donationID {
		response.NotFound(w, r, "War slot not found for this donation")
		return
	}

	att, err := qtx.GetLatestStaffAttendanceForCheckInForUpdate(ctx, dbgen.GetLatestStaffAttendanceForCheckInForUpdateParams{
		WarSlotID: slotID,
		StaffID:   claims.UserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Shift reservation not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to check attendance")
		return
	}

	if att.CancelledAt.Valid {
		response.BadRequest(w, r, "VAL_002", "Cannot self check-in to a cancelled reservation", nil)
		return
	}

	if att.Status == dbgen.AttendanceStatusAttended {
		response.Success(w, http.StatusOK, "Already checked in", map[string]any{
			"status": "attended",
		})
		return
	}

	if att.Status == dbgen.AttendanceStatusAbsent {
		response.BadRequest(w, r, "VAL_002", "Cannot self check-in after being marked absent by coordinator", nil)
		return
	}

	if att.Status != dbgen.AttendanceStatusRegistered {
		response.BadRequest(w, r, "VAL_002", "Self check-in requires registered status", nil)
		return
	}

	updated, err := qtx.StaffSelfCheckIn(ctx, att.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to record self check-in")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit self check-in")
		return
	}

	response.Success(w, http.StatusOK, "Self check-in completed successfully", toStaffAttendanceResponse(updated))
}

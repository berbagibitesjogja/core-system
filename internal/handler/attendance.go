package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type AttendanceHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewAttendanceHandler(pool *pgxpool.Pool, cfg *config.Config) *AttendanceHandler {
	return &AttendanceHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

func formatWindowDuration(d time.Duration) string {
	if d <= 0 {
		return "0 minutes"
	}
	if d%time.Hour == 0 {
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	}
	if d < time.Hour && d%time.Minute == 0 {
		m := int(d / time.Minute)
		if m == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		return strings.TrimSuffix(s, "0s")
	}
	return s
}

type MarkAttendanceRequest struct {
	Status string  `json:"status"`
	Notes  *string `json:"notes,omitempty"`
}

type LiftBanRequest struct {
	Note *string `json:"note,omitempty"`
}

type CreateManualBanRequest struct {
	Scope         string `json:"scope"`
	DonationCount int32  `json:"donation_count"`
	Note          string `json:"note"`
}

type HeroAttendanceResponse struct {
	ID            uuid.UUID  `json:"id"`
	DonationID    uuid.UUID  `json:"donation_id"`
	HeroID        uuid.UUID  `json:"hero_id"`
	Status        string     `json:"status"`
	CancelledLate bool       `json:"cancelled_late"`
	CancelledAt   *time.Time `json:"cancelled_at"`
	AttendedAt    *time.Time `json:"attended_at"`
	MarkedBy      *uuid.UUID `json:"marked_by"`
	Notes         *string    `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func toHeroAttendanceResponse(a dbgen.HeroAttendance) HeroAttendanceResponse {
	res := HeroAttendanceResponse{
		ID:            a.ID,
		DonationID:    a.DonationID,
		HeroID:        a.HeroID,
		Status:        string(a.Status),
		CancelledLate: a.CancelledLate,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
	if a.CancelledAt.Valid {
		t := a.CancelledAt.Time
		res.CancelledAt = &t
	}
	if a.AttendedAt.Valid {
		t := a.AttendedAt.Time
		res.AttendedAt = &t
	}
	if a.MarkedBy.Valid {
		id := uuid.UUID(a.MarkedBy.Bytes)
		res.MarkedBy = &id
	}
	if a.Notes.Valid {
		res.Notes = &a.Notes.String
	}
	return res
}

type StaffAttendanceResponse struct {
	ID            int64      `json:"id"`
	WarSlotID     int64      `json:"war_slot_id"`
	StaffID       uuid.UUID  `json:"staff_id"`
	Status        string     `json:"status"`
	CancelledLate bool       `json:"cancelled_late"`
	CancelledAt   *time.Time `json:"cancelled_at"`
	AttendedAt    *time.Time `json:"attended_at"`
	MarkedBy      *uuid.UUID `json:"marked_by"`
	CheckinSource *string    `json:"checkin_source"`
	Notes         *string    `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func toStaffAttendanceResponse(a dbgen.StaffAttendance) StaffAttendanceResponse {
	res := StaffAttendanceResponse{
		ID:            a.ID,
		WarSlotID:     a.WarSlotID,
		StaffID:       a.StaffID,
		Status:        string(a.Status),
		CancelledLate: a.CancelledLate,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
	if a.CheckinSource.Valid {
		s := a.CheckinSource.String
		res.CheckinSource = &s
	}
	if a.CancelledAt.Valid {
		t := a.CancelledAt.Time
		res.CancelledAt = &t
	}
	if a.AttendedAt.Valid {
		t := a.AttendedAt.Time
		res.AttendedAt = &t
	}
	if a.MarkedBy.Valid {
		id := uuid.UUID(a.MarkedBy.Bytes)
		res.MarkedBy = &id
	}
	if a.Notes.Valid {
		res.Notes = &a.Notes.String
	}
	return res
}

type UserBanResponse struct {
	ID                      int64      `json:"id"`
	UserID                  uuid.UUID  `json:"user_id"`
	Scope                   string     `json:"scope"`
	DonationID              *uuid.UUID `json:"donation_id"`
	DonationCount           int32      `json:"donation_count"`
	Reason                  string     `json:"reason"`
	SourceHeroAttendanceID  *uuid.UUID `json:"source_hero_attendance_id"`
	SourceStaffAttendanceID *int64     `json:"source_staff_attendance_id"`
	Note                    *string    `json:"note"`
	CreatedBy               *uuid.UUID `json:"created_by"`
	LiftedAt                *time.Time `json:"lifted_at"`
	LiftedBy                *uuid.UUID `json:"lifted_by"`
	CreatedAt               time.Time  `json:"created_at"`
}

func mapUserBanFields(
	id int64,
	userID uuid.UUID,
	scope dbgen.BanScope,
	donationID pgtype.UUID,
	donationCount int32,
	reason dbgen.BanReason,
	sourceHeroAttendanceID pgtype.UUID,
	sourceStaffAttendanceID pgtype.Int8,
	note pgtype.Text,
	createdBy pgtype.UUID,
	liftedAt pgtype.Timestamptz,
	liftedBy pgtype.UUID,
	createdAt time.Time,
) UserBanResponse {
	res := UserBanResponse{
		ID:            id,
		UserID:        userID,
		Scope:         string(scope),
		DonationCount: donationCount,
		Reason:        string(reason),
		CreatedAt:     createdAt,
	}
	if donationID.Valid {
		did := uuid.UUID(donationID.Bytes)
		res.DonationID = &did
	}
	if sourceHeroAttendanceID.Valid {
		hid := uuid.UUID(sourceHeroAttendanceID.Bytes)
		res.SourceHeroAttendanceID = &hid
	}
	if sourceStaffAttendanceID.Valid {
		sid := sourceStaffAttendanceID.Int64
		res.SourceStaffAttendanceID = &sid
	}
	if note.Valid {
		res.Note = &note.String
	}
	if createdBy.Valid {
		cid := uuid.UUID(createdBy.Bytes)
		res.CreatedBy = &cid
	}
	if liftedAt.Valid {
		t := liftedAt.Time
		res.LiftedAt = &t
	}
	if liftedBy.Valid {
		lid := uuid.UUID(liftedBy.Bytes)
		res.LiftedBy = &lid
	}
	return res
}

func liftUserBanRowToResponse(b dbgen.LiftUserBanRow) UserBanResponse {
	return mapUserBanFields(
		b.ID, b.UserID, b.Scope, b.DonationID, b.DonationCount, b.Reason,
		b.SourceHeroAttendanceID, b.SourceStaffAttendanceID, b.Note, b.CreatedBy,
		b.LiftedAt, b.LiftedBy, b.CreatedAt,
	)
}

func createManualBanRowToResponse(b dbgen.CreateManualBanRow) UserBanResponse {
	return mapUserBanFields(
		b.ID, b.UserID, b.Scope, b.DonationID, b.DonationCount, b.Reason,
		b.SourceHeroAttendanceID, b.SourceStaffAttendanceID, b.Note, b.CreatedBy,
		b.LiftedAt, b.LiftedBy, b.CreatedAt,
	)
}

type HeroAttendanceItem struct {
	ID            uuid.UUID  `json:"id"`
	DonationID    uuid.UUID  `json:"donation_id"`
	HeroID        uuid.UUID  `json:"hero_id"`
	HeroName      string     `json:"hero_name"`
	HeroEmail     string     `json:"hero_email"`
	HeroPhone     *string    `json:"hero_phone"`
	Status        string     `json:"status"`
	CancelledLate bool       `json:"cancelled_late"`
	CancelledAt   *time.Time `json:"cancelled_at"`
	AttendedAt    *time.Time `json:"attended_at"`
	MarkedBy      *uuid.UUID `json:"marked_by"`
	Notes         *string    `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type StaffAttendanceItem struct {
	ID            int64      `json:"id"`
	WarSlotID     int64      `json:"war_slot_id"`
	StaffID       uuid.UUID  `json:"staff_id"`
	StaffName     string     `json:"staff_name"`
	StaffEmail    string     `json:"staff_email"`
	StaffPhone    *string    `json:"staff_phone"`
	DivisionID    int32      `json:"division_id"`
	DivisionName  string     `json:"division_name"`
	Status        string     `json:"status"`
	CancelledLate bool       `json:"cancelled_late"`
	CancelledAt   *time.Time `json:"cancelled_at"`
	AttendedAt    *time.Time `json:"attended_at"`
	MarkedBy      *uuid.UUID `json:"marked_by"`
	CheckinSource *string    `json:"checkin_source"`
	Notes         *string    `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (h *AttendanceHandler) ListHeroAttendancesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	donationIDStr := r.PathValue("id")
	donationID, err := uuid.Parse(donationIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid donation ID", nil)
		return
	}

	donExists, err := h.queries.DonationExists(ctx, donationID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to verify donation")
		return
	}
	if !donExists {
		response.NotFound(w, r, "Donation not found")
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
	switch statusFilter {
	case "", "unmarked", "registered", "attended", "absent", "cancelled":
	default:
		response.BadRequest(w, r, "VAL_001", "Invalid status filter; must be one of unmarked, registered, attended, absent, cancelled", nil)
		return
	}

	unmarkedOnly := statusFilter == "unmarked"
	var statusParam dbgen.NullAttendanceStatus
	if statusFilter != "" && !unmarkedOnly {
		statusParam = dbgen.NullAttendanceStatus{
			AttendanceStatus: dbgen.AttendanceStatus(statusFilter),
			Valid:            true,
		}
	}

	totalItems32, err := h.queries.CountHeroAttendancesByDonation(ctx, dbgen.CountHeroAttendancesByDonationParams{
		DonationID:   donationID,
		UnmarkedOnly: unmarkedOnly,
		Status:       statusParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count attendances")
		return
	}
	totalItems := int(totalItems32)

	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(limit)))
	}

	rows, err := h.queries.ListHeroAttendancesByDonation(ctx, dbgen.ListHeroAttendancesByDonationParams{
		DonationID:   donationID,
		UnmarkedOnly: unmarkedOnly,
		Status:       statusParam,
		Offset:       int32(offset),
		Limit:        int32(limit),
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to query attendances")
		return
	}

	items := make([]HeroAttendanceItem, 0, len(rows))
	for _, row := range rows {
		item := HeroAttendanceItem{
			ID:            row.ID,
			DonationID:    row.DonationID,
			HeroID:        row.HeroID,
			HeroName:      row.HeroName,
			HeroEmail:     row.HeroEmail,
			Status:        string(row.Status),
			CancelledLate: row.CancelledLate,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
		if row.CancelledAt.Valid {
			t := row.CancelledAt.Time
			item.CancelledAt = &t
		}
		if row.AttendedAt.Valid {
			t := row.AttendedAt.Time
			item.AttendedAt = &t
		}
		if row.MarkedBy.Valid {
			u := uuid.UUID(row.MarkedBy.Bytes)
			item.MarkedBy = &u
		}
		if row.Notes.Valid {
			s := row.Notes.String
			item.Notes = &s
		}
		if row.HeroPhone.Valid {
			s := row.HeroPhone.String
			item.HeroPhone = &s
		}
		items = append(items, item)
	}

	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      totalItems,
		TotalPages: totalPages,
	}
	response.PaginatedSuccess(w, http.StatusOK, "Hero attendances retrieved successfully", items, meta)
}

func (h *AttendanceHandler) MarkHeroAttendanceHandler(w http.ResponseWriter, r *http.Request) {
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

	attendanceIDStr := r.PathValue("attendance_id")
	attendanceID, err := uuid.Parse(attendanceIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid attendance ID", nil)
		return
	}

	var req MarkAttendanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body format", nil)
		return
	}

	newStatus := strings.ToLower(strings.TrimSpace(req.Status))
	if newStatus != "attended" && newStatus != "absent" {
		response.BadRequest(w, r, "VAL_001", "Status must be either 'attended' or 'absent'", nil)
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
		response.InternalServerError(w, r, err, "Failed to check donation")
		return
	}

	if don.Status == dbgen.DonationStatusDraft || don.Status == dbgen.DonationStatusCancelled {
		response.BadRequest(w, r, "VAL_002", "Attendance can only be marked for open, closed, or completed donations", nil)
		return
	}

	now := time.Now()
	if newStatus == "absent" && now.Before(don.EventAt) {
		response.BadRequest(w, r, "CANNOT_MARK_ABSENT_BEFORE_EVENT_START", "Cannot mark hero absent before the distribution event starts", nil)
		return
	}
	if newStatus == "attended" && now.Before(don.EventAt.Add(-h.cfg.War.HeroAttendanceWindowBefore)) {
		msg := fmt.Sprintf("Attendance check-in window opens %s prior to event time", formatWindowDuration(h.cfg.War.HeroAttendanceWindowBefore))
		response.BadRequest(w, r, "ATTENDANCE_WINDOW_NOT_OPEN", msg, nil)
		return
	}

	att, err := qtx.GetHeroAttendanceForMarkForUpdate(ctx, dbgen.GetHeroAttendanceForMarkForUpdateParams{
		ID:         attendanceID,
		DonationID: donationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Hero attendance record not found for this donation")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lock attendance record")
		return
	}

	if att.Status == dbgen.AttendanceStatusCancelled {
		response.BadRequest(w, r, "VAL_002", "Cannot mark attendance for cancelled reservations", nil)
		return
	}

	notesVal := pgtype.Text{}
	if req.Notes != nil {
		notesVal = pgtype.Text{String: *req.Notes, Valid: true}
	}

	updated, err := qtx.MarkHeroAttendance(ctx, dbgen.MarkHeroAttendanceParams{
		ID:       attendanceID,
		Column2:  dbgen.AttendanceStatus(newStatus),
		MarkedBy: pgtype.UUID{Bytes: claims.UserID, Valid: true},
		Notes:    notesVal,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to mark attendance")
		return
	}

	if newStatus == "absent" {
		if att.Status != dbgen.AttendanceStatusAbsent {
			_, err = qtx.UpsertHeroBanOnAbsent(ctx, dbgen.UpsertHeroBanOnAbsentParams{
				UserID:                 att.HeroID,
				DonationID:             pgtype.UUID{Bytes: donationID, Valid: true},
				DonationCount:          int32(h.cfg.War.HeroBanEventCount),
				SourceHeroAttendanceID: pgtype.UUID{Bytes: attendanceID, Valid: true},
			})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				response.InternalServerError(w, r, err, "Failed to record absence penalty")
				return
			}
		}
	} else if newStatus == "attended" {
		err = qtx.AutoLiftHeroBan(ctx, pgtype.UUID{Bytes: attendanceID, Valid: true})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to auto-lift attendance penalty")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit attendance update")
		return
	}

	response.Success(w, http.StatusOK, "Attendance marked successfully", toHeroAttendanceResponse(updated))
}

func (h *AttendanceHandler) ListStaffAttendancesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	slotExists, err := h.queries.ActiveWarSlotExistsForDonation(ctx, dbgen.ActiveWarSlotExistsForDonationParams{
		ID:         slotID,
		DonationID: donationID,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to verify war slot")
		return
	}
	if !slotExists {
		response.NotFound(w, r, "War slot not found for this donation")
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
	switch statusFilter {
	case "", "unmarked", "registered", "attended", "absent", "cancelled":
	default:
		response.BadRequest(w, r, "VAL_001", "Invalid status filter; must be one of unmarked, registered, attended, absent, cancelled", nil)
		return
	}

	unmarkedOnly := statusFilter == "unmarked"
	var statusParam dbgen.NullAttendanceStatus
	if statusFilter != "" && !unmarkedOnly {
		statusParam = dbgen.NullAttendanceStatus{
			AttendanceStatus: dbgen.AttendanceStatus(statusFilter),
			Valid:            true,
		}
	}

	totalItems32, err := h.queries.CountStaffAttendancesBySlot(ctx, dbgen.CountStaffAttendancesBySlotParams{
		WarSlotID:    slotID,
		UnmarkedOnly: unmarkedOnly,
		Status:       statusParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count staff attendances")
		return
	}
	totalItems := int(totalItems32)

	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(limit)))
	}

	rows, err := h.queries.ListStaffAttendancesBySlot(ctx, dbgen.ListStaffAttendancesBySlotParams{
		WarSlotID:    slotID,
		UnmarkedOnly: unmarkedOnly,
		Status:       statusParam,
		Offset:       int32(offset),
		Limit:        int32(limit),
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to query staff attendances")
		return
	}

	items := make([]StaffAttendanceItem, 0, len(rows))
	for _, row := range rows {
		item := StaffAttendanceItem{
			ID:            row.ID,
			WarSlotID:     row.WarSlotID,
			StaffID:       row.StaffID,
			StaffName:     row.StaffName,
			StaffEmail:    row.StaffEmail,
			DivisionID:    row.DivisionID,
			DivisionName:  row.DivisionName,
			Status:        string(row.Status),
			CancelledLate: row.CancelledLate,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
		if row.CancelledAt.Valid {
			t := row.CancelledAt.Time
			item.CancelledAt = &t
		}
		if row.AttendedAt.Valid {
			t := row.AttendedAt.Time
			item.AttendedAt = &t
		}
		if row.MarkedBy.Valid {
			u := uuid.UUID(row.MarkedBy.Bytes)
			item.MarkedBy = &u
		}
		if row.CheckinSource.Valid {
			s := row.CheckinSource.String
			item.CheckinSource = &s
		}
		if row.Notes.Valid {
			s := row.Notes.String
			item.Notes = &s
		}
		if row.StaffPhone.Valid {
			s := row.StaffPhone.String
			item.StaffPhone = &s
		}
		items = append(items, item)
	}

	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      totalItems,
		TotalPages: totalPages,
	}
	response.PaginatedSuccess(w, http.StatusOK, "Staff attendances retrieved successfully", items, meta)
}

func (h *AttendanceHandler) MarkStaffAttendanceHandler(w http.ResponseWriter, r *http.Request) {
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

	staffAttendanceIDStr := r.PathValue("staff_attendance_id")
	staffAttendanceID, err := strconv.ParseInt(staffAttendanceIDStr, 10, 64)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid staff attendance ID", nil)
		return
	}

	var req MarkAttendanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body format", nil)
		return
	}

	newStatus := strings.ToLower(strings.TrimSpace(req.Status))
	if newStatus != "attended" && newStatus != "absent" {
		response.BadRequest(w, r, "VAL_001", "Status must be either 'attended' or 'absent'", nil)
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
		response.BadRequest(w, r, "VAL_002", "Attendance can only be marked for open, closed, or completed donations", nil)
		return
	}

	now := time.Now()
	if newStatus == "absent" && now.Before(don.EventAt) {
		response.BadRequest(w, r, "CANNOT_MARK_ABSENT_BEFORE_EVENT_START", "Cannot mark staff absent before the distribution event starts", nil)
		return
	}
	if newStatus == "attended" && now.Before(don.EventAt.Add(-h.cfg.War.StaffAttendanceWindowBefore)) {
		msg := fmt.Sprintf("Attendance check-in window opens %s prior to event time", formatWindowDuration(h.cfg.War.StaffAttendanceWindowBefore))
		response.BadRequest(w, r, "ATTENDANCE_WINDOW_NOT_OPEN", msg, nil)
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
		response.NotFound(w, r, "War slot does not belong to this donation")
		return
	}

	att, err := qtx.GetStaffAttendanceForMarkForUpdate(ctx, dbgen.GetStaffAttendanceForMarkForUpdateParams{
		ID:        staffAttendanceID,
		WarSlotID: slotID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Staff attendance record not found for this shift slot")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lock attendance record")
		return
	}

	if att.Status == dbgen.AttendanceStatusCancelled {
		response.BadRequest(w, r, "VAL_002", "Cannot mark attendance for cancelled shift reservations", nil)
		return
	}

	notesVal := pgtype.Text{}
	if req.Notes != nil {
		notesVal = pgtype.Text{String: *req.Notes, Valid: true}
	}

	updated, err := qtx.MarkStaffAttendance(ctx, dbgen.MarkStaffAttendanceParams{
		ID:       staffAttendanceID,
		Column2:  dbgen.AttendanceStatus(newStatus),
		MarkedBy: pgtype.UUID{Bytes: claims.UserID, Valid: true},
		Notes:    notesVal,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to mark staff attendance")
		return
	}

	if newStatus == "absent" {
		if att.Status != dbgen.AttendanceStatusAbsent {
			_, err = qtx.UpsertStaffBanOnAbsent(ctx, dbgen.UpsertStaffBanOnAbsentParams{
				UserID:                  att.StaffID,
				DonationID:              pgtype.UUID{Bytes: donationID, Valid: true},
				DonationCount:           int32(h.cfg.War.StaffBanEventCount),
				SourceStaffAttendanceID: pgtype.Int8{Int64: staffAttendanceID, Valid: true},
			})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				response.InternalServerError(w, r, err, "Failed to record absence penalty")
				return
			}
		}
	} else if newStatus == "attended" {
		err = qtx.AutoLiftStaffBan(ctx, pgtype.Int8{Int64: staffAttendanceID, Valid: true})
		if err != nil {
			response.InternalServerError(w, r, err, "Failed to auto-lift staff attendance penalty")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit staff attendance update")
		return
	}

	response.Success(w, http.StatusOK, "Staff attendance marked successfully", toStaffAttendanceResponse(updated))
}

func (h *AttendanceHandler) ListUserBansHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userIDStr := r.PathValue("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid user ID", nil)
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

	scopeFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	switch scopeFilter {
	case "", "hero_war", "staff_war", "all":
	default:
		response.BadRequest(w, r, "VAL_001", "Invalid scope filter; must be 'hero_war', 'staff_war', or 'all'", nil)
		return
	}

	activeOnly := false
	if activeOnlyStr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("active_only"))); activeOnlyStr != "" {
		parsed, err := strconv.ParseBool(activeOnlyStr)
		if err != nil {
			response.BadRequest(w, r, "VAL_001", "Invalid active_only filter; must be 'true' or 'false'", nil)
			return
		}
		activeOnly = parsed
	}

	userExists, err := h.queries.UserExists(ctx, userID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to verify user")
		return
	}
	if !userExists {
		response.NotFound(w, r, "User not found")
		return
	}

	var scopeParam dbgen.NullBanScope
	if scopeFilter != "" {
		scopeParam = dbgen.NullBanScope{
			BanScope: dbgen.BanScope(scopeFilter),
			Valid:    true,
		}
	}

	totalItems32, err := h.queries.CountUserBans(ctx, dbgen.CountUserBansParams{
		UserID:     userID,
		Scope:      scopeParam,
		ActiveOnly: activeOnly,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count user bans")
		return
	}
	totalItems := int(totalItems32)

	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(limit)))
	}

	bans, err := h.queries.ListUserBans(ctx, dbgen.ListUserBansParams{
		UserID:     userID,
		Scope:      scopeParam,
		ActiveOnly: activeOnly,
		Offset:     int32(offset),
		Limit:      int32(limit),
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to list user bans")
		return
	}

	banResponses := make([]UserBanResponse, 0, len(bans))
	for _, b := range bans {
		banResponses = append(banResponses, mapUserBanFields(
			b.ID, b.UserID, b.Scope, b.DonationID, b.DonationCount, b.Reason,
			b.SourceHeroAttendanceID, b.SourceStaffAttendanceID, b.Note, b.CreatedBy,
			b.LiftedAt, b.LiftedBy, b.CreatedAt,
		))
	}

	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      totalItems,
		TotalPages: totalPages,
	}
	response.PaginatedSuccess(w, http.StatusOK, "User bans retrieved successfully", banResponses, meta)
}

func (h *AttendanceHandler) LiftBanHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsStaff {
		response.Forbidden(w, r, "Staff access required")
		return
	}

	banIDStr := r.PathValue("id")
	banID, err := strconv.ParseInt(banIDStr, 10, 64)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid ban ID", nil)
		return
	}

	var req LiftBanRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			response.BadRequest(w, r, "VAL_001", "Invalid request body format", nil)
			return
		}
	}

	existing, err := h.queries.GetUserBanByID(ctx, banID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "User ban record not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve user ban")
		return
	}
	if existing.LiftedAt.Valid {
		response.Conflict(w, r, "BAN_ALREADY_LIFTED", "User ban has already been lifted")
		return
	}

	noteVal := pgtype.Text{}
	if req.Note != nil && strings.TrimSpace(*req.Note) != "" {
		noteVal = pgtype.Text{String: strings.TrimSpace(*req.Note), Valid: true}
	} else {
		noteVal = pgtype.Text{String: "manual pardon by coordinator", Valid: true}
	}

	lifted, err := h.queries.LiftUserBan(ctx, dbgen.LiftUserBanParams{
		ID:       banID,
		LiftedBy: pgtype.UUID{Bytes: claims.UserID, Valid: true},
		Note:     noteVal,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Conflict(w, r, "BAN_ALREADY_LIFTED", "User ban has already been lifted")
			return
		}
		response.InternalServerError(w, r, err, "Failed to lift user ban")
		return
	}

	response.Success(w, http.StatusOK, "User ban pardoned successfully", liftUserBanRowToResponse(lifted))
}

func (h *AttendanceHandler) CreateManualBanHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.GetClaims(ctx)
	if !ok || claims == nil || !claims.IsStaff {
		response.Forbidden(w, r, "Staff access required")
		return
	}

	targetUserIDStr := r.PathValue("id")
	targetUserID, err := uuid.Parse(targetUserIDStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_002", "Invalid user ID", nil)
		return
	}

	if claims.UserID == targetUserID {
		response.BadRequest(w, r, "VAL_001", "You cannot ban yourself", nil)
		return
	}

	var req CreateManualBanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body format", nil)
		return
	}

	if req.DonationCount < 1 || req.DonationCount > 10 {
		response.BadRequest(w, r, "VAL_001", "donation_count must be between 1 and 10", nil)
		return
	}

	note := strings.TrimSpace(req.Note)
	if note == "" {
		response.BadRequest(w, r, "VAL_001", "note is required for manual bans", nil)
		return
	}

	scopeStr := strings.ToLower(strings.TrimSpace(req.Scope))
	var scope dbgen.BanScope
	switch scopeStr {
	case "hero_war":
		scope = dbgen.BanScopeHeroWar
	case "staff_war":
		scope = dbgen.BanScopeStaffWar
	case "all":
		scope = dbgen.BanScopeAll
	default:
		response.BadRequest(w, r, "VAL_001", "scope must be 'hero_war', 'staff_war', or 'all'", nil)
		return
	}

	profiles, err := h.queries.GetUserBanTargetProfiles(ctx, targetUserID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check user profiles")
		return
	}

	if !profiles.UserExists {
		response.NotFound(w, r, "Target user not found")
		return
	}

	if scope == dbgen.BanScopeHeroWar && !profiles.IsHero {
		response.BadRequest(w, r, "VAL_002", "Target user does not have a hero profile", nil)
		return
	}
	if scope == dbgen.BanScopeStaffWar && !profiles.IsStaff {
		response.BadRequest(w, r, "VAL_002", "Target user does not have a staff profile", nil)
		return
	}
	if scope == dbgen.BanScopeAll && (!profiles.IsHero || !profiles.IsStaff) {
		response.BadRequest(w, r, "VAL_002", "Scope 'all' requires the target user to have both a hero and a staff profile", nil)
		return
	}

	hasActiveManualBan, err := h.queries.CheckActiveManualBan(ctx, dbgen.CheckActiveManualBanParams{
		UserID: targetUserID,
		Scope:  scope,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to check active manual bans")
		return
	}
	if hasActiveManualBan {
		response.Conflict(w, r, "MANUAL_BAN_ALREADY_ACTIVE", "Target user already has an active manual ban covering this scope")
		return
	}

	createdBan, err := h.queries.CreateManualBan(ctx, dbgen.CreateManualBanParams{
		UserID:        targetUserID,
		Scope:         scope,
		DonationCount: req.DonationCount,
		Note:          pgtype.Text{String: note, Valid: true},
		CreatedBy:     pgtype.UUID{Bytes: claims.UserID, Valid: true},
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to create manual ban")
		return
	}

	response.Success(w, http.StatusCreated, "Manual ban created successfully", createManualBanRowToResponse(createdBan))
}

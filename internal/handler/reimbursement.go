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

var allowedReimbursementStatuses = map[string]bool{
	"pending":   true,
	"completed": true,
	"rejected":  true,
}

type ReimbursementHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewReimbursementHandler(pool *pgxpool.Pool, cfg *config.Config) *ReimbursementHandler {
	return &ReimbursementHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type CreateReimbursementRequest struct {
	Amount  int64    `json:"amount"`
	Method  string   `json:"method"`
	Target  string   `json:"target"`
	Notes   *string  `json:"notes"`
	FileIDs []string `json:"file_ids"`
}

type ReimbursementResponse struct {
	ID              string   `json:"id"`
	UserID          string   `json:"user_id"`
	Amount          int64    `json:"amount"`
	Method          string   `json:"method"`
	Target          string   `json:"target"`
	Notes           *string  `json:"notes"`
	Status          string   `json:"status"`
	PaymentProof    *string  `json:"payment_proof"`
	PaidBy          *string  `json:"paid_by"`
	PaidAt          *string  `json:"paid_at"`
	RejectionReason *string  `json:"rejection_reason"`
	FileIDs         []string `json:"file_ids"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

func toReimbursementResponse(item dbgen.Reimbursement, fileIDs []string) ReimbursementResponse {
	var notes *string
	if item.Notes.Valid {
		str := item.Notes.String
		notes = &str
	}

	var paymentProof *string
	if item.PaymentProof.Valid {
		str := uuid.UUID(item.PaymentProof.Bytes).String()
		paymentProof = &str
	}

	var paidBy *string
	if item.PaidBy.Valid {
		str := uuid.UUID(item.PaidBy.Bytes).String()
		paidBy = &str
	}

	var paidAt *string
	if item.PaidAt.Valid {
		str := item.PaidAt.Time.Format(time.RFC3339)
		paidAt = &str
	}

	var rejectionReason *string
	if item.RejectionReason.Valid {
		str := item.RejectionReason.String
		rejectionReason = &str
	}

	if fileIDs == nil {
		fileIDs = []string{}
	}

	return ReimbursementResponse{
		ID:              item.ID.String(),
		UserID:          item.UserID.String(),
		Amount:          item.Amount,
		Method:          item.Method,
		Target:          item.Target,
		Notes:           notes,
		Status:          item.Status,
		PaymentProof:    paymentProof,
		PaidBy:          paidBy,
		PaidAt:          paidAt,
		RejectionReason: rejectionReason,
		FileIDs:         fileIDs,
		CreatedAt:       item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       item.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *ReimbursementHandler) CreateReimbursementHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetClaims(r.Context())
	if !ok || claims == nil {
		response.Unauthorized(w, r, "Authentication required")
		return
	}

	var req CreateReimbursementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	fieldErrors := make(map[string]string)
	if req.Amount <= 0 {
		fieldErrors["amount"] = "Amount must be greater than zero"
	}

	req.Method = strings.TrimSpace(req.Method)
	if req.Method == "" {
		fieldErrors["method"] = "Payment method is required"
	} else if len(req.Method) > 50 {
		fieldErrors["method"] = "Payment method cannot exceed 50 characters"
	}

	req.Target = strings.TrimSpace(req.Target)
	if req.Target == "" {
		fieldErrors["target"] = "Target account or number is required"
	} else if len(req.Target) > 100 {
		fieldErrors["target"] = "Target cannot exceed 100 characters"
	}

	parsedFileIDs := make([]uuid.UUID, 0, len(req.FileIDs))
	for _, fidStr := range req.FileIDs {
		fid, err := uuid.Parse(fidStr)
		if err != nil {
			fieldErrors["file_ids"] = "Invalid receipt file ID format"
			break
		}
		parsedFileIDs = append(parsedFileIDs, fid)
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	var notesParam pgtype.Text
	if req.Notes != nil {
		trimmed := strings.TrimSpace(*req.Notes)
		if trimmed != "" {
			notesParam = pgtype.Text{String: trimmed, Valid: true}
		}
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.queries.WithTx(tx)

	item, err := qtx.CreateReimbursement(r.Context(), dbgen.CreateReimbursementParams{
		UserID: claims.UserID,
		Amount: req.Amount,
		Method: req.Method,
		Target: req.Target,
		Notes:  notesParam,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create reimbursement")
		return
	}

	createdFileIDs := make([]string, 0, len(parsedFileIDs))
	for _, fid := range parsedFileIDs {
		_, err := qtx.AddReimbursementFile(r.Context(), dbgen.AddReimbursementFileParams{
			ReimbursementID: item.ID,
			FileID:          fid,
		})
		if err != nil {
			if response.HandleDBError(w, r, err) {
				return
			}
			response.InternalServerError(w, r, err, "Failed to link receipt file")
			return
		}
		createdFileIDs = append(createdFileIDs, fid.String())
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit transaction")
		return
	}

	response.Success(w, http.StatusCreated, "Reimbursement created successfully", toReimbursementResponse(item, createdFileIDs))
}

func (h *ReimbursementHandler) ListReimbursementsHandler(w http.ResponseWriter, r *http.Request) {
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

	var userParam pgtype.UUID
	if uStr := strings.TrimSpace(query.Get("user_id")); uStr != "" {
		if uVal, err := uuid.Parse(uStr); err == nil {
			userParam = pgtype.UUID{Bytes: uVal, Valid: true}
		} else {
			response.BadRequest(w, r, "VAL_001", "Invalid user_id filter format", nil)
			return
		}
	}

	var statusParam pgtype.Text
	if s := strings.TrimSpace(strings.ToLower(query.Get("status"))); s != "" {
		if !allowedReimbursementStatuses[s] {
			response.BadRequest(w, r, "VAL_001", "Invalid status filter. Allowed: pending, completed, rejected", nil)
			return
		}
		statusParam = pgtype.Text{String: s, Valid: true}
	}

	var searchParam pgtype.Text
	if s := strings.TrimSpace(query.Get("search")); s != "" {
		searchParam = pgtype.Text{String: s, Valid: true}
	}

	total, err := h.queries.CountReimbursements(r.Context(), dbgen.CountReimbursementsParams{
		UserID: userParam,
		Status: statusParam,
		Search: searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count reimbursements")
		return
	}

	offset := int32((page - 1) * limit)
	items, err := h.queries.ListReimbursements(r.Context(), dbgen.ListReimbursementsParams{
		Limit:  int32(limit),
		Offset: offset,
		UserID: userParam,
		Status: statusParam,
		Search: searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to list reimbursements")
		return
	}

	data := make([]ReimbursementResponse, 0, len(items))
	for _, item := range items {
		files, _ := h.queries.ListReimbursementFilesByReimbursementID(r.Context(), item.ID)
		fileIDs := make([]string, 0, len(files))
		for _, f := range files {
			fileIDs = append(fileIDs, f.FileID.String())
		}
		data = append(data, toReimbursementResponse(item, fileIDs))
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(total),
		TotalPages: totalPages,
	}

	response.PaginatedSuccess(w, http.StatusOK, "Reimbursements retrieved successfully", data, meta)
}

func (h *ReimbursementHandler) GetReimbursementByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	reimbursementID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid reimbursement ID format", nil)
		return
	}

	item, err := h.queries.GetReimbursementByID(r.Context(), reimbursementID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Reimbursement not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve reimbursement")
		return
	}

	files, err := h.queries.ListReimbursementFilesByReimbursementID(r.Context(), item.ID)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve attached files")
		return
	}

	fileIDs := make([]string, 0, len(files))
	for _, f := range files {
		fileIDs = append(fileIDs, f.FileID.String())
	}

	response.Success(w, http.StatusOK, "Reimbursement retrieved successfully", toReimbursementResponse(item, fileIDs))
}

func (h *ReimbursementHandler) UpdateReimbursementHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	reimbursementID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid reimbursement ID format", nil)
		return
	}

	existing, err := h.queries.GetReimbursementByID(r.Context(), reimbursementID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Reimbursement not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve reimbursement")
		return
	}

	var rawMap map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMap); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	updateParams := dbgen.UpdateReimbursementParams{
		ID:              existing.ID,
		Amount:          existing.Amount,
		Method:          existing.Method,
		Target:          existing.Target,
		Notes:           existing.Notes,
		Status:          existing.Status,
		PaymentProof:    existing.PaymentProof,
		PaidBy:          existing.PaidBy,
		PaidAt:          existing.PaidAt,
		RejectionReason: existing.RejectionReason,
	}

	fieldErrors := make(map[string]string)

	if rawAmount, exists := rawMap["amount"]; exists {
		var amt int64
		if err := json.Unmarshal(rawAmount, &amt); err != nil || amt <= 0 {
			fieldErrors["amount"] = "Amount must be greater than zero"
		} else {
			updateParams.Amount = amt
		}
	}

	if rawMethod, exists := rawMap["method"]; exists {
		var m string
		if err := json.Unmarshal(rawMethod, &m); err != nil {
			fieldErrors["method"] = "Method must be a valid string"
		} else {
			trimmed := strings.TrimSpace(m)
			if trimmed == "" {
				fieldErrors["method"] = "Payment method cannot be empty"
			} else if len(trimmed) > 50 {
				fieldErrors["method"] = "Payment method cannot exceed 50 characters"
			} else {
				updateParams.Method = trimmed
			}
		}
	}

	if rawTarget, exists := rawMap["target"]; exists {
		var tgt string
		if err := json.Unmarshal(rawTarget, &tgt); err != nil {
			fieldErrors["target"] = "Target must be a valid string"
		} else {
			trimmed := strings.TrimSpace(tgt)
			if trimmed == "" {
				fieldErrors["target"] = "Target cannot be empty"
			} else if len(trimmed) > 100 {
				fieldErrors["target"] = "Target cannot exceed 100 characters"
			} else {
				updateParams.Target = trimmed
			}
		}
	}

	if rawNotes, exists := rawMap["notes"]; exists {
		var n *string
		if err := json.Unmarshal(rawNotes, &n); err != nil {
			fieldErrors["notes"] = "Notes must be a valid string or null"
		} else if n != nil {
			trimmed := strings.TrimSpace(*n)
			if trimmed != "" {
				updateParams.Notes = pgtype.Text{String: trimmed, Valid: true}
			} else {
				updateParams.Notes = pgtype.Text{Valid: false}
			}
		} else {
			updateParams.Notes = pgtype.Text{Valid: false}
		}
	}

	if rawStatus, exists := rawMap["status"]; exists {
		var st string
		if err := json.Unmarshal(rawStatus, &st); err != nil {
			fieldErrors["status"] = "Status must be a valid string"
		} else {
			trimmed := strings.TrimSpace(strings.ToLower(st))
			if !allowedReimbursementStatuses[trimmed] {
				fieldErrors["status"] = "Invalid status. Allowed: pending, completed, rejected"
			} else {
				updateParams.Status = trimmed
			}
		}
	}

	if rawProof, exists := rawMap["payment_proof"]; exists {
		var p *string
		if err := json.Unmarshal(rawProof, &p); err != nil {
			fieldErrors["payment_proof"] = "Payment proof must be a valid UUID string or null"
		} else if p != nil {
			pID, err := uuid.Parse(*p)
			if err != nil {
				fieldErrors["payment_proof"] = "Invalid payment proof UUID format"
			} else {
				updateParams.PaymentProof = pgtype.UUID{Bytes: pID, Valid: true}
			}
		} else {
			updateParams.PaymentProof = pgtype.UUID{Valid: false}
		}
	}

	if rawPaidBy, exists := rawMap["paid_by"]; exists {
		var pb *string
		if err := json.Unmarshal(rawPaidBy, &pb); err != nil {
			fieldErrors["paid_by"] = "Paid by must be a valid UUID string or null"
		} else if pb != nil {
			pbID, err := uuid.Parse(*pb)
			if err != nil {
				fieldErrors["paid_by"] = "Invalid paid_by UUID format"
			} else {
				updateParams.PaidBy = pgtype.UUID{Bytes: pbID, Valid: true}
			}
		} else {
			updateParams.PaidBy = pgtype.UUID{Valid: false}
		}
	}

	if rawRejection, exists := rawMap["rejection_reason"]; exists {
		var r *string
		if err := json.Unmarshal(rawRejection, &r); err != nil {
			fieldErrors["rejection_reason"] = "Rejection reason must be a valid string or null"
		} else if r != nil {
			trimmed := strings.TrimSpace(*r)
			if trimmed != "" {
				updateParams.RejectionReason = pgtype.Text{String: trimmed, Valid: true}
			} else {
				updateParams.RejectionReason = pgtype.Text{Valid: false}
			}
		} else {
			updateParams.RejectionReason = pgtype.Text{Valid: false}
		}
	}

	var newFileIDs []uuid.UUID
	filesUpdated := false
	if rawFiles, exists := rawMap["file_ids"]; exists {
		var fStrs []string
		if err := json.Unmarshal(rawFiles, &fStrs); err != nil {
			fieldErrors["file_ids"] = "File IDs must be an array of UUID strings"
		} else {
			filesUpdated = true
			for _, fStr := range fStrs {
				fID, err := uuid.Parse(fStr)
				if err != nil {
					fieldErrors["file_ids"] = "Invalid receipt file ID format"
					break
				}
				newFileIDs = append(newFileIDs, fID)
			}
		}
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.queries.WithTx(tx)

	updated, err := qtx.UpdateReimbursement(r.Context(), updateParams)
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to update reimbursement")
		return
	}

	if filesUpdated {
		if err := qtx.DeleteReimbursementFilesByReimbursementID(r.Context(), updated.ID); err != nil {
			response.InternalServerError(w, r, err, "Failed to reset attached receipt files")
			return
		}
		for _, fid := range newFileIDs {
			_, err := qtx.AddReimbursementFile(r.Context(), dbgen.AddReimbursementFileParams{
				ReimbursementID: updated.ID,
				FileID:          fid,
			})
			if err != nil {
				if response.HandleDBError(w, r, err) {
					return
				}
				response.InternalServerError(w, r, err, "Failed to link receipt file")
				return
			}
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, r, err, "Failed to commit transaction")
		return
	}

	files, _ := h.queries.ListReimbursementFilesByReimbursementID(r.Context(), updated.ID)
	resultFileIDs := make([]string, 0, len(files))
	for _, f := range files {
		resultFileIDs = append(resultFileIDs, f.FileID.String())
	}

	response.Success(w, http.StatusOK, "Reimbursement updated successfully", toReimbursementResponse(updated, resultFileIDs))
}

func (h *ReimbursementHandler) DeleteReimbursementHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	reimbursementID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid reimbursement ID format", nil)
		return
	}

	deleted, err := h.queries.SoftDeleteReimbursement(r.Context(), reimbursementID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Reimbursement not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to delete reimbursement")
		return
	}

	response.Success(w, http.StatusOK, "Reimbursement deleted successfully", map[string]string{
		"id": deleted.ID.String(),
	})
}

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
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FoodItemHandler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queries *dbgen.Queries
}

func NewFoodItemHandler(pool *pgxpool.Pool, cfg *config.Config) *FoodItemHandler {
	return &FoodItemHandler{
		pool:    pool,
		cfg:     cfg,
		queries: dbgen.New(pool),
	}
}

type CreateFoodItemRequest struct {
	Name string `json:"name"`
}

type UpdateFoodItemRequest struct {
	Name string `json:"name"`
}

type FoodItemResponse struct {
	ID        int32  `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type FoodItemListItemResponse struct {
	ID   int32  `json:"id"`
	Name string `json:"name"`
}

func toFoodItemResponse(item dbgen.FoodItem) FoodItemResponse {
	return FoodItemResponse{
		ID:        item.ID,
		Name:      item.Name,
		CreatedAt: item.CreatedAt.Format(time.RFC3339),
		UpdatedAt: item.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *FoodItemHandler) ListFoodItemsHandler(w http.ResponseWriter, r *http.Request) {
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

	var searchParam pgtype.Text
	if s := strings.TrimSpace(query.Get("search")); s != "" {
		searchParam = pgtype.Text{String: s, Valid: true}
	}

	total, err := h.queries.CountFoodItems(r.Context(), searchParam)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to count food items")
		return
	}

	offset := int32((page - 1) * limit)
	items, err := h.queries.ListFoodItems(r.Context(), dbgen.ListFoodItemsParams{
		Limit:  int32(limit),
		Offset: offset,
		Search: searchParam,
	})
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to list food items")
		return
	}

	data := make([]FoodItemListItemResponse, 0, len(items))
	for _, item := range items {
		data = append(data, FoodItemListItemResponse{
			ID:   item.ID,
			Name: item.Name,
		})
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	meta := response.ResponseMeta{
		Page:       page,
		Limit:      limit,
		Total:      int(total),
		TotalPages: totalPages,
	}

	response.PaginatedSuccess(w, http.StatusOK, "Food items retrieved successfully", data, meta)
}

func (h *FoodItemHandler) GetFoodItemByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		response.BadRequest(w, r, "VAL_001", "Invalid food item ID format", nil)
		return
	}

	item, err := h.queries.GetFoodItemByID(r.Context(), int32(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Food item not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve food item")
		return
	}

	response.Success(w, http.StatusOK, "Food item retrieved successfully", toFoodItemResponse(item))
}

func (h *FoodItemHandler) CreateFoodItemHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateFoodItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	fieldErrors := make(map[string]string)

	if req.Name == "" {
		fieldErrors["name"] = "Food item name is required"
	} else if len(req.Name) > 100 {
		fieldErrors["name"] = "Food item name cannot exceed 100 characters"
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	item, err := h.queries.CreateFoodItem(r.Context(), req.Name)
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to create food item")
		return
	}

	response.Success(w, http.StatusCreated, "Food item created successfully", toFoodItemResponse(item))
}

func (h *FoodItemHandler) UpdateFoodItemHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		response.BadRequest(w, r, "VAL_001", "Invalid food item ID format", nil)
		return
	}

	_, err = h.queries.GetFoodItemByID(r.Context(), int32(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Food item not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to retrieve food item")
		return
	}

	var req UpdateFoodItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, r, "VAL_001", "Invalid request body", nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	fieldErrors := make(map[string]string)

	if req.Name == "" {
		fieldErrors["name"] = "Food item name cannot be empty"
	} else if len(req.Name) > 100 {
		fieldErrors["name"] = "Food item name cannot exceed 100 characters"
	}

	if len(fieldErrors) > 0 {
		response.BadRequest(w, r, "VAL_001", "Validation failed", fieldErrors)
		return
	}

	updated, err := h.queries.UpdateFoodItem(r.Context(), dbgen.UpdateFoodItemParams{
		ID:   int32(id),
		Name: req.Name,
	})
	if err != nil {
		if response.HandleDBError(w, r, err) {
			return
		}
		response.InternalServerError(w, r, err, "Failed to update food item")
		return
	}

	response.Success(w, http.StatusOK, "Food item updated successfully", toFoodItemResponse(updated))
}

func (h *FoodItemHandler) DeleteFoodItemHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		response.BadRequest(w, r, "VAL_001", "Invalid food item ID format", nil)
		return
	}

	deletedID, err := h.queries.DeleteFoodItem(r.Context(), int32(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, r, "Food item not found")
			return
		}
		response.InternalServerError(w, r, err, "Failed to delete food item")
		return
	}

	response.Success(w, http.StatusOK, "Food item deleted successfully", map[string]int32{
		"id": deletedID,
	})
}

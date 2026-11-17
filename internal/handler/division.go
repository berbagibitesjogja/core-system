package handler

import (
	"net/http"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DivisionHandler struct {
	queries *dbgen.Queries
}

func NewDivisionHandler(pool *pgxpool.Pool, _ *config.Config) *DivisionHandler {
	return &DivisionHandler{
		queries: dbgen.New(pool),
	}
}

type DivisionListItemResponse struct {
	ID   int32  `json:"id"`
	Name string `json:"name"`
}

func (h *DivisionHandler) ListDivisionsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListDivisions(r.Context())
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to retrieve divisions")
		return
	}

	data := make([]DivisionListItemResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, DivisionListItemResponse{
			ID:   row.ID,
			Name: row.Name,
		})
	}

	response.Success(w, http.StatusOK, "Divisions retrieved successfully", data)
}

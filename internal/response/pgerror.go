package response

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// HandleDBError inspects database driver errors (e.g. pgconn.PgError) and maps
// known constraint violations to user-friendly 4xx responses.
// It also attaches the raw database error to the request context so structured
// logging (slog) records the exact driver error (e.g. SQLSTATE and constraint name).
// Returns true if the error was recognized and handled; false otherwise.
func HandleDBError(w http.ResponseWriter, r *http.Request, err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	if r != nil && err != nil {
		SetError(r.Context(), err)
	}

	switch pgErr.Code {
	case "23505": // unique_violation
		fieldErrors := make(map[string]string)
		switch {
		case strings.Contains(pgErr.ConstraintName, "phone"):
			fieldErrors["phone_number"] = "Phone number is already registered"
		case strings.Contains(pgErr.ConstraintName, "email"):
			fieldErrors["email"] = "Email address is already registered"
		default:
			fieldErrors["general"] = "A record with this identifier already exists"
		}
		ErrorWithRequest(w, r, http.StatusBadRequest, "VAL_002", "Validation failed", fieldErrors)
		return true

	case "23503": // foreign_key_violation
		fieldErrors := make(map[string]string)
		if strings.Contains(pgErr.ConstraintName, "division") {
			fieldErrors["division_id"] = "Selected division does not exist"
		} else {
			fieldErrors["general"] = "Referenced resource does not exist"
		}
		ErrorWithRequest(w, r, http.StatusBadRequest, "VAL_002", "Validation failed", fieldErrors)
		return true

	case "22001": // string_data_right_truncation
		ErrorWithRequest(w, r, http.StatusBadRequest, "VAL_001", "Input field exceeds maximum allowed character length", nil)
		return true
	}

	return false
}

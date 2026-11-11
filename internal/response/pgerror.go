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
		case strings.Contains(pgErr.ConstraintName, "beneficiary_categories_active_email_domain") || strings.Contains(pgErr.ConstraintName, "email_domain"):
			fieldErrors["email_domain"] = "Email domain is already registered"
		case strings.Contains(pgErr.ConstraintName, "phone"):
			fieldErrors["phone_number"] = "Phone number is already registered"
		case strings.Contains(pgErr.ConstraintName, "email"):
			fieldErrors["email"] = "Email address is already registered"
		case strings.Contains(pgErr.ConstraintName, "beneficiary_categories"):
			fieldErrors["name"] = "Beneficiary category already exists"
		case strings.Contains(pgErr.ConstraintName, "food_items"):
			fieldErrors["name"] = "Food item already exists"
		case strings.Contains(pgErr.ConstraintName, "uq_reimbursement_files_pair"):
			fieldErrors["file_id"] = "Receipt file is already attached to this reimbursement"
		case strings.Contains(pgErr.ConstraintName, "name"):
			fieldErrors["name"] = "Sponsor name is already registered"
		default:
			fieldErrors["general"] = "A record with this identifier already exists"
		}
		ErrorWithRequest(w, r, http.StatusBadRequest, "VAL_002", "Validation failed", fieldErrors)
		return true

	case "23503": // foreign_key_violation
		fieldErrors := make(map[string]string)
		switch {
		case strings.Contains(pgErr.ConstraintName, "division"):
			fieldErrors["division_id"] = "Selected division does not exist"
		case strings.Contains(pgErr.ConstraintName, "logo"):
			fieldErrors["logo"] = "Referenced logo file does not exist"
		case strings.Contains(pgErr.ConstraintName, "payment_proof"):
			fieldErrors["payment_proof"] = "Referenced payment proof file does not exist"
		case strings.Contains(pgErr.ConstraintName, "reimbursement_files_file_id"):
			fieldErrors["file_id"] = "Referenced receipt file does not exist"
		case strings.Contains(pgErr.ConstraintName, "paid_by"):
			fieldErrors["paid_by"] = "Referenced staff member does not exist"
		case strings.Contains(pgErr.ConstraintName, "user_id"):
			fieldErrors["user_id"] = "Referenced user does not exist"
		default:
			fieldErrors["general"] = "Referenced resource does not exist"
		}
		ErrorWithRequest(w, r, http.StatusBadRequest, "VAL_002", "Validation failed", fieldErrors)
		return true

	case "23514": // check_violation
		fieldErrors := make(map[string]string)
		switch {
		case strings.Contains(pgErr.ConstraintName, "chk_email_domain_university_only"):
			fieldErrors["email_domain"] = "Email domain can only be set for university variant"
		case strings.Contains(pgErr.ConstraintName, "chk_email_domain_lowercase"):
			fieldErrors["email_domain"] = "Email domain must be lowercase"
		case strings.Contains(pgErr.ConstraintName, "variant"):
			fieldErrors["variant"] = "Invalid variant"
		default:
			fieldErrors["general"] = "Check constraint violation"
		}
		ErrorWithRequest(w, r, http.StatusBadRequest, "VAL_002", "Validation failed", fieldErrors)
		return true

	case "22001": // string_data_right_truncation
		ErrorWithRequest(w, r, http.StatusBadRequest, "VAL_001", "Input field exceeds maximum allowed character length", nil)
		return true
	}

	return false
}

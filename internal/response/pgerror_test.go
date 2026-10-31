package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestHandleDBError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantHandled  bool
		wantStatus   int
		wantCode     string
		wantErrorKey string
		wantErrorVal string
	}{
		{
			name: "duplicate phone number (23505)",
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "idx_users_active_phone",
			},
			wantHandled:  true,
			wantStatus:   http.StatusBadRequest,
			wantCode:     "VAL_002",
			wantErrorKey: "phone_number",
			wantErrorVal: "Phone number is already registered",
		},
		{
			name: "duplicate email (23505)",
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "idx_users_active_email",
			},
			wantHandled:  true,
			wantStatus:   http.StatusBadRequest,
			wantCode:     "VAL_002",
			wantErrorKey: "email",
			wantErrorVal: "Email address is already registered",
		},
		{
			name: "foreign key violation on division (23503)",
			err: &pgconn.PgError{
				Code:           "23503",
				ConstraintName: "users_division_id_fkey",
			},
			wantHandled:  true,
			wantStatus:   http.StatusBadRequest,
			wantCode:     "VAL_002",
			wantErrorKey: "division_id",
			wantErrorVal: "Selected division does not exist",
		},
		{
			name: "string truncation (22001)",
			err: &pgconn.PgError{
				Code: "22001",
			},
			wantHandled: true,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "VAL_001",
		},
		{
			name:        "non-pg error returns false",
			err:         errors.New("generic disk failure"),
			wantHandled: false,
		},
		{
			name: "unrelated pg error returns false",
			err: &pgconn.PgError{
				Code: "57P01", // admin_shutdown
			},
			wantHandled: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/test", nil)

			handled := HandleDBError(rec, req, tc.err)
			if handled != tc.wantHandled {
				t.Fatalf("expected handled=%v, got %v", tc.wantHandled, handled)
			}

			if !tc.wantHandled {
				return
			}

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}

			var resp Response
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Code != tc.wantCode {
				t.Errorf("expected code %s, got %s", tc.wantCode, resp.Code)
			}

			if tc.wantErrorKey != "" {
				errMap, ok := resp.Errors.(map[string]any)
				if !ok {
					t.Fatalf("expected errors to be a map, got %T", resp.Errors)
				}
				val, exists := errMap[tc.wantErrorKey]
				if !exists || val != tc.wantErrorVal {
					t.Errorf("expected error %s=%s, got %v", tc.wantErrorKey, tc.wantErrorVal, val)
				}
			}
		})
	}
}

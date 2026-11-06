package response

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDContext(t *testing.T) {
	t.Run("round trip request id in context", func(t *testing.T) {
		ctx := WithRequestID(context.Background(), "ref_a1b2c3d4")
		if got := GetRequestID(ctx); got != "ref_a1b2c3d4" {
			t.Fatalf("expected ref_a1b2c3d4, got %q", got)
		}
	})

	t.Run("empty string when not set", func(t *testing.T) {
		if got := GetRequestID(context.TODO()); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("empty string when context is nil", func(t *testing.T) {
		var nilCtx context.Context
		if got := GetRequestID(nilCtx); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})
}

func TestInternalServerError(t *testing.T) {
	t.Run("includes ref_id from request context", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx, _ := WithErrorContext(req.Context())
		ctx = WithRequestID(ctx, "ref_test1234")
		req = req.WithContext(ctx)

		testErr := errors.New("database connection refused")
		InternalServerError(rec, req, testErr, "Failed to connect to database")

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", rec.Code)
		}

		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Status != "error" {
			t.Fatalf("expected status 'error', got %q", resp.Status)
		}
		if resp.Code != "INTERNAL_ERROR" {
			t.Fatalf("expected code 'INTERNAL_ERROR', got %q", resp.Code)
		}
		if resp.RefID != "ref_test1234" {
			t.Fatalf("expected ref_id 'ref_test1234', got %q", resp.RefID)
		}
		if resp.Message != "Failed to connect to database" {
			t.Fatalf("expected message 'Failed to connect to database', got %q", resp.Message)
		}

		if capturedErr := GetError(req.Context()); capturedErr != testErr {
			t.Fatalf("expected captured error %v, got %v", testErr, capturedErr)
		}
	})

	t.Run("safely handles nil request", func(t *testing.T) {
		rec := httptest.NewRecorder()
		var nilReq *http.Request
		InternalServerError(rec, nilReq, nil, "")

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", rec.Code)
		}

		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.RefID != "" {
			t.Fatalf("expected empty ref_id, got %q", resp.RefID)
		}
		if resp.Message != "An unexpected error occurred" {
			t.Fatalf("expected default message, got %q", resp.Message)
		}
	})
}

func TestErrorWithRequest(t *testing.T) {
	t.Run("populates ref_id for client error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req = req.WithContext(WithRequestID(req.Context(), "ref_client99"))

		ErrorWithRequest(rec, req, http.StatusBadRequest, "VAL_001", "Validation failed", map[string]string{
			"email": "invalid email format",
		})

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}

		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.RefID != "ref_client99" {
			t.Fatalf("expected ref_id 'ref_client99', got %q", resp.RefID)
		}
		if resp.Code != "VAL_001" {
			t.Fatalf("expected code 'VAL_001', got %q", resp.Code)
		}
	})
}

func TestRefIDOmittedWhenEmpty(t *testing.T) {
	t.Run("omits ref_id in json when not present", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Error(rec, http.StatusNotFound, "NOT_FOUND", "Resource not found", nil)

		bodyStr := rec.Body.String()
		if strings.Contains(bodyStr, `"ref_id"`) {
			t.Fatalf("expected ref_id to be omitted, got %s", bodyStr)
		}
	})
}

func TestConvenienceHelpersWithRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(WithRequestID(req.Context(), "ref_helper_test"))

	t.Run("BadRequest includes ref_id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		BadRequest(rec, req, "VAL_001", "Bad input", nil)

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.RefID != "ref_helper_test" || rec.Code != http.StatusBadRequest {
			t.Fatalf("unexpected response: code=%d ref_id=%s", rec.Code, resp.RefID)
		}
	})

	t.Run("Unauthorized includes ref_id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Unauthorized(rec, req, "Invalid credentials")

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.RefID != "ref_helper_test" || rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected response: code=%d ref_id=%s", rec.Code, resp.RefID)
		}
	})

	t.Run("Forbidden includes ref_id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Forbidden(rec, req, "Access denied")

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.RefID != "ref_helper_test" || rec.Code != http.StatusForbidden {
			t.Fatalf("unexpected response: code=%d ref_id=%s", rec.Code, resp.RefID)
		}
	})

	t.Run("NotFound includes ref_id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		NotFound(rec, req, "Not found")

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.RefID != "ref_helper_test" || rec.Code != http.StatusNotFound {
			t.Fatalf("unexpected response: code=%d ref_id=%s", rec.Code, resp.RefID)
		}
	})

	t.Run("TooManyRequests includes ref_id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		TooManyRequests(rec, req, "")

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.RefID != "ref_helper_test" || rec.Code != http.StatusTooManyRequests {
			t.Fatalf("unexpected response: code=%d ref_id=%s", rec.Code, resp.RefID)
		}
	})
}

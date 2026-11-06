package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/berbagibitesjogja/core-system/internal/response"
)

func TestLoggerWithRequestID(t *testing.T) {
	t.Run("logs include ref_id when RequestID middleware precedes Logger", func(t *testing.T) {
		var logBuf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		originalDefault := slog.Default()
		slog.SetDefault(testLogger)
		defer slog.SetDefault(originalDefault)

		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		chain := RequestID(Logger(testHandler))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
		rec := httptest.NewRecorder()

		chain.ServeHTTP(rec, req)

		responseHeaderID := rec.Header().Get(RequestIDHeader)
		if responseHeaderID == "" {
			t.Fatal("expected X-Request-ID response header")
		}

		logOutput := logBuf.String()
		if !strings.Contains(logOutput, responseHeaderID) {
			t.Fatalf("expected log output to contain ref_id %q, got: %s", responseHeaderID, logOutput)
		}

		var logEntry map[string]any
		if err := json.Unmarshal(logBuf.Bytes(), &logEntry); err != nil {
			t.Fatalf("failed to parse log json: %v", err)
		}

		if logEntry["ref_id"] != responseHeaderID {
			t.Fatalf("expected ref_id in log %q, got %v", responseHeaderID, logEntry["ref_id"])
		}
	})

	t.Run("logs raw error when SetError is in context", func(t *testing.T) {
		var logBuf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		originalDefault := slog.Default()
		slog.SetDefault(testLogger)
		defer slog.SetDefault(originalDefault)

		rawDBErr := errors.New("ERROR: duplicate key value violates unique constraint \"idx_users_active_phone\" (SQLSTATE 23505)")

		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response.SetError(r.Context(), rawDBErr)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":"error","code":"VAL_002","message":"Validation failed"}`))
		})

		chain := RequestID(Logger(testHandler))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil)
		rec := httptest.NewRecorder()

		chain.ServeHTTP(rec, req)

		var logEntry map[string]any
		if err := json.Unmarshal(logBuf.Bytes(), &logEntry); err != nil {
			t.Fatalf("failed to parse log json: %v", err)
		}

		if logEntry["error"] != rawDBErr.Error() {
			t.Fatalf("expected error attribute %q, got %v", rawDBErr.Error(), logEntry["error"])
		}
		if logEntry["ref_id"] == nil || logEntry["ref_id"] == "" {
			t.Fatal("expected ref_id in log")
		}
	})

	t.Run("omits error and does not dump response body on pure validation errors", func(t *testing.T) {
		var logBuf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		originalDefault := slog.Default()
		slog.SetDefault(testLogger)
		defer slog.SetDefault(originalDefault)

		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":"error","code":"VAL_001","message":"Validation failed"}`))
		})

		chain := RequestID(Logger(testHandler))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil)
		rec := httptest.NewRecorder()

		chain.ServeHTTP(rec, req)

		var logEntry map[string]any
		if err := json.Unmarshal(logBuf.Bytes(), &logEntry); err != nil {
			t.Fatalf("failed to parse log json: %v", err)
		}

		if _, exists := logEntry["error"]; exists {
			t.Fatalf("expected no 'error' attribute in log, got %v", logEntry["error"])
		}
		if logEntry["ref_id"] == nil || logEntry["ref_id"] == "" {
			t.Fatal("expected ref_id in log")
		}
	})
}

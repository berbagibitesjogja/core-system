package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
}

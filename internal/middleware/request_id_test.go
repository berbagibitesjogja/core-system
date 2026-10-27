package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateRefID(t *testing.T) {
	t.Run("format and length", func(t *testing.T) {
		id := GenerateRefID()
		if !strings.HasPrefix(id, "ref_") {
			t.Fatalf("expected prefix 'ref_', got %q", id)
		}
		if len(id) != 12 {
			t.Fatalf("expected length 12, got %d (%q)", len(id), id)
		}
	})

	t.Run("uniqueness", func(t *testing.T) {
		seen := make(map[string]struct{})
		for range 1000 {
			id := GenerateRefID()
			if _, exists := seen[id]; exists {
				t.Fatalf("duplicate ref_id generated: %q", id)
			}
			seen[id] = struct{}{}
		}
	})
}

func TestWithAndGetRequestID(t *testing.T) {
	t.Run("round trip context value", func(t *testing.T) {
		ctx := context.Background()
		expected := "ref_1234abcd"
		ctx = WithRequestID(ctx, expected)

		got := GetRequestID(ctx)
		if got != expected {
			t.Fatalf("expected %q, got %q", expected, got)
		}
	})

	t.Run("empty when not set", func(t *testing.T) {
		if got := GetRequestID(context.TODO()); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("empty when context is nil", func(t *testing.T) {
		var nilCtx context.Context
		if got := GetRequestID(nilCtx); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})
}

func TestRequestIDMiddleware(t *testing.T) {
	tests := []struct {
		name              string
		incomingHeader    string
		expectSameHeader  bool
		expectValidPrefix bool
	}{
		{
			name:              "generates new ref_id when header is empty",
			incomingHeader:    "",
			expectSameHeader:  false,
			expectValidPrefix: true,
		},
		{
			name:              "preserves incoming valid header",
			incomingHeader:    "custom-upstream-trace-123",
			expectSameHeader:  true,
			expectValidPrefix: false,
		},
		{
			name:              "replaces oversized incoming header with generated ref_id",
			incomingHeader:    strings.Repeat("a", 100),
			expectSameHeader:  false,
			expectValidPrefix: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedContextID string

			handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedContextID = GetRequestID(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tc.incomingHeader != "" {
				req.Header.Set(RequestIDHeader, tc.incomingHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			responseHeader := rec.Header().Get(RequestIDHeader)
			if responseHeader == "" {
				t.Fatal("expected X-Request-ID response header to be set")
			}
			if responseHeader != capturedContextID {
				t.Fatalf("response header %q does not match context id %q", responseHeader, capturedContextID)
			}

			if tc.expectSameHeader && responseHeader != tc.incomingHeader {
				t.Fatalf("expected header %q, got %q", tc.incomingHeader, responseHeader)
			}

			if tc.expectValidPrefix && (!strings.HasPrefix(responseHeader, "ref_") || len(responseHeader) != 12) {
				t.Fatalf("expected generated ref_id format 'ref_xxxxxxxx', got %q", responseHeader)
			}
		})
	}
}

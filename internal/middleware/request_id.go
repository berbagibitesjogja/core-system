package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/response"
)

const (
	RequestIDHeader  = "X-Request-ID"
	maxIncomingIDLen = 64
)

func GenerateRefID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		fallback := make([]byte, 4)
		nano := uint32(time.Now().UnixNano())
		fallback[0] = byte(nano)
		fallback[1] = byte(nano >> 8)
		fallback[2] = byte(nano >> 16)
		fallback[3] = byte(nano >> 24)
		return "ref_" + hex.EncodeToString(fallback)
	}
	return "ref_" + hex.EncodeToString(b)
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return response.WithRequestID(ctx, id)
}

func GetRequestID(ctx context.Context) string {
	return response.GetRequestID(ctx)
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if reqID == "" || len(reqID) > maxIncomingIDLen {
			reqID = GenerateRefID()
		}

		w.Header().Set(RequestIDHeader, reqID)
		ctx := WithRequestID(r.Context(), reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

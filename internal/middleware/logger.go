package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/response"
)

type responseWriterInterceptor struct {
	http.ResponseWriter
	statusCode int
	written    bool
	body       bytes.Buffer
}

func (rw *responseWriterInterceptor) WriteHeader(statusCode int) {
	if rw.written {
		return
	}
	rw.statusCode = statusCode
	rw.written = true
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriterInterceptor) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	if rw.statusCode >= 400 {
		rw.body.Write(b)
	}
	return rw.ResponseWriter.Write(b)
}

func SetError(ctx context.Context, err error) {
	response.SetError(ctx, err)
}

func GetError(ctx context.Context) error {
	return response.GetError(ctx)
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriterInterceptor{ResponseWriter: w, statusCode: http.StatusOK}
		ctx, _ := response.WithErrorContext(r.Context())

		next.ServeHTTP(rw, r.WithContext(ctx))

		duration := time.Since(start)
		level := slog.LevelInfo
		if rw.statusCode >= 500 {
			level = slog.LevelError
		} else if rw.statusCode >= 400 {
			level = slog.LevelWarn
		}

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", duration,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		}

		if refID := response.GetRequestID(ctx); refID != "" {
			attrs = append(attrs, "ref_id", refID)
		}

		if err := response.GetError(ctx); err != nil {
			attrs = append(attrs, "error", err.Error())
		} else if rw.statusCode >= 400 && rw.body.Len() > 0 {
			attrs = append(attrs, "error", rw.body.String())
		}

		slog.Log(ctx, level, "HTTP request handled", attrs...)
	})
}

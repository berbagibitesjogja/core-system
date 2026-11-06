package response

import (
	"context"
	"encoding/json"
	"net/http"
)

type contextKey string

const (
	errorHolderKey contextKey = "response_error_holder"
	requestIDKey   contextKey = "response_request_id"
)

type errorHolder struct {
	err error
}

func SetError(ctx context.Context, err error) {
	if holder, ok := ctx.Value(errorHolderKey).(*errorHolder); ok && holder != nil {
		holder.err = err
	}
}

func GetError(ctx context.Context) error {
	if holder, ok := ctx.Value(errorHolderKey).(*errorHolder); ok && holder != nil {
		return holder.err
	}
	return nil
}

func WithErrorContext(ctx context.Context) (context.Context, *errorHolder) {
	holder := &errorHolder{}
	return context.WithValue(ctx, errorHolderKey, holder), holder
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

type Response struct {
	Status  string        `json:"status"`
	Code    string        `json:"code,omitempty"`
	Message string        `json:"message,omitempty"`
	RefID   string        `json:"ref_id,omitempty"`
	Data    any           `json:"data,omitempty"`
	Meta    *ResponseMeta `json:"meta,omitempty"`
	Errors  any           `json:"errors,omitempty"`
}

type ResponseMeta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

func JSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func Success(w http.ResponseWriter, statusCode int, message string, data any) {
	JSON(w, statusCode, Response{
		Status:  "success",
		Message: message,
		Data:    data,
	})
}

func PaginatedSuccess(w http.ResponseWriter, statusCode int, message string, data any, meta ResponseMeta) {
	JSON(w, statusCode, Response{
		Status:  "success",
		Message: message,
		Data:    data,
		Meta:    &meta,
	})
}

func Error(w http.ResponseWriter, statusCode int, code, message string, errDetails any) {
	JSON(w, statusCode, Response{
		Status:  "error",
		Code:    code,
		Message: message,
		Errors:  errDetails,
	})
}

func ErrorWithRequest(w http.ResponseWriter, r *http.Request, statusCode int, code, message string, errDetails any) {
	var refID string
	if r != nil {
		refID = GetRequestID(r.Context())
	}
	JSON(w, statusCode, Response{
		Status:  "error",
		Code:    code,
		Message: message,
		RefID:   refID,
		Errors:  errDetails,
	})
}

func BadRequest(w http.ResponseWriter, r *http.Request, code, message string, errDetails any) {
	ErrorWithRequest(w, r, http.StatusBadRequest, code, message, errDetails)
}

func Unauthorized(w http.ResponseWriter, r *http.Request, message string) {
	ErrorWithRequest(w, r, http.StatusUnauthorized, "UNAUTHORIZED", message, nil)
}

func Forbidden(w http.ResponseWriter, r *http.Request, message string) {
	ErrorWithRequest(w, r, http.StatusForbidden, "FORBIDDEN", message, nil)
}

func NotFound(w http.ResponseWriter, r *http.Request, message string) {
	ErrorWithRequest(w, r, http.StatusNotFound, "NOT_FOUND", message, nil)
}

func TooManyRequests(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "Too many requests, please try again later"
	}
	ErrorWithRequest(w, r, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", message, nil)
}

func InternalServerError(w http.ResponseWriter, r *http.Request, err error, message string) {
	var refID string
	if r != nil {
		if err != nil {
			SetError(r.Context(), err)
		}
		refID = GetRequestID(r.Context())
	}
	if message == "" {
		message = "An unexpected error occurred"
	}
	JSON(w, http.StatusInternalServerError, Response{
		Status:  "error",
		Code:    "INTERNAL_ERROR",
		Message: message,
		RefID:   refID,
	})
}

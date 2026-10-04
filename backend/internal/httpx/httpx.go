// Package httpx holds small HTTP helpers shared by all handlers: the JSON error
// envelope from SPEC-00 and an error-returning handler type.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// Error is an API error rendered as {"error":{"code","message"}}.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newErr(status int, code, format string, a ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, a...)}
}

func Unauthorized(format string, a ...any) *Error {
	return newErr(http.StatusUnauthorized, "unauthorized", format, a...)
}
func Forbidden(format string, a ...any) *Error {
	return newErr(http.StatusForbidden, "forbidden", format, a...)
}
func NotFound(format string, a ...any) *Error {
	return newErr(http.StatusNotFound, "not_found", format, a...)
}
func BadRequest(format string, a ...any) *Error {
	return newErr(http.StatusBadRequest, "invalid_request", format, a...)
}
func Conflict(format string, a ...any) *Error {
	return newErr(http.StatusConflict, "conflict", format, a...)
}
func TooManyRequests(format string, a ...any) *Error {
	return newErr(http.StatusTooManyRequests, "rate_limited", format, a...)
}
func Upstream(format string, a ...any) *Error {
	return newErr(http.StatusBadGateway, "upstream_error", format, a...)
}

// Handler is an http.Handler whose function may return an error.
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, err)
	}
}

// WriteError renders err; anything that is not an *Error becomes a 500 without leaking details.
func WriteError(w http.ResponseWriter, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		slog.Error("unhandled error", "err", err)
		apiErr = newErr(http.StatusInternalServerError, "internal", "erro interno")
	}
	JSON(w, apiErr.Status, map[string]any{"error": apiErr})
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Decode reads a JSON body (max 1 MiB) into v.
func Decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return BadRequest("corpo da requisição inválido")
	}
	return nil
}

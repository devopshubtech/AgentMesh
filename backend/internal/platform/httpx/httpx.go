// Package httpx contains HTTP helpers: JSON encoding, typed errors and
// error-returning handlers.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// MaxBodyBytes caps JSON request bodies.
const MaxBodyBytes = 1 << 20

// Error is an API error rendered as {"error":{...}}.
type Error struct {
	Status     int               `json:"-"`
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Fields     map[string]string `json:"fields,omitempty"`
	RetryAfter int               `json:"-"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func newErr(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func BadRequest(msg string) *Error                 { return newErr(http.StatusBadRequest, "invalid_request", msg) }
func Unauthorized(msg string) *Error               { return newErr(http.StatusUnauthorized, "unauthenticated", msg) }
func Forbidden(msg string) *Error                  { return newErr(http.StatusForbidden, "forbidden", msg) }
func NotFound(what string) *Error                  { return newErr(http.StatusNotFound, "not_found", what+" not found") }
func Conflict(msg string) *Error                   { return newErr(http.StatusConflict, "conflict", msg) }
func InvalidState(msg string) *Error               { return newErr(http.StatusConflict, "invalid_state", msg) }
func WithCode(status int, code, msg string) *Error { return newErr(status, code, msg) }

// RateLimited returns a 429 error with a Retry-After hint in seconds.
func RateLimited(retryAfter int) *Error {
	e := newErr(http.StatusTooManyRequests, "rate_limited", "too many requests, slow down")
	e.RetryAfter = retryAfter
	return e
}

// Validation collects per-field validation messages.
type Validation map[string]string

// Err returns nil when there are no failures.
func (v Validation) Err() error {
	if len(v) == 0 {
		return nil
	}
	return &Error{Status: http.StatusBadRequest, Code: "validation_failed", Message: "request validation failed", Fields: v}
}

// HandlerFunc is an http handler that may return an error.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// H adapts an error-returning handler into http.HandlerFunc.
func H(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

// WriteError renders err. Unknown errors are logged and reported as 500.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		slog.ErrorContext(r.Context(), "request failed", "err", err, "path", r.URL.Path, "request_id", RequestID(r.Context()))
		apiErr = newErr(http.StatusInternalServerError, "internal", "internal server error")
	}
	if apiErr.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(apiErr.RetryAfter))
	}
	body := struct {
		Error struct {
			*Error
			RequestID string `json:"request_id"`
		} `json:"error"`
	}{}
	body.Error.Error = apiErr
	body.Error.RequestID = RequestID(r.Context())
	WriteJSON(w, apiErr.Status, body)
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

// DecodeJSON strictly decodes a bounded JSON body into dst.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return newErr(http.StatusUnsupportedMediaType, "invalid_request", "content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return newErr(http.StatusRequestEntityTooLarge, "invalid_request", "request body too large")
		case errors.Is(err, io.EOF):
			return BadRequest("request body is empty")
		default:
			return BadRequest("malformed JSON: " + err.Error())
		}
	}
	if dec.More() {
		return BadRequest("request body must contain a single JSON object")
	}
	return nil
}

// List is the standard paginated list envelope.
type List[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// ParseLimit reads ?limit= within [1,200], defaulting to 50.
func ParseLimit(r *http.Request) (int, error) {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return 50, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 200 {
		return 0, BadRequest("limit must be between 1 and 200")
	}
	return n, nil
}

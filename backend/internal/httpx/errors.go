package httpx

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"leadintel/backend/internal/apperr"
)

// Error is an HTTP-level error rendered as the standard error envelope.
type Error struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

var (
	ErrInvalidJSON     = &Error{Status: http.StatusBadRequest, Code: "invalid_json", Message: "Request body is not valid JSON."}
	ErrPayloadTooLarge = &Error{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: "Request body is too large."}
	ErrNotFoundRoute   = &Error{Status: http.StatusNotFound, Code: "not_found", Message: "Resource not found."}
	ErrInvalidRequest  = &Error{Status: http.StatusBadRequest, Code: "validation_failed", Message: "Request validation failed."}
	ErrUnauthorized    = &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "Missing or invalid admin token."}
	errInternal        = &Error{Status: http.StatusInternalServerError, Code: "internal", Message: "Internal server error."}
)

type envelope struct {
	Error envelopeBody `json:"error"`
}

type envelopeBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// WriteError is the single place where errors are mapped to HTTP responses.
// Unknown errors become a generic 500; their details only reach the log.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	e := toHTTPError(err)
	if e.Status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "request failed",
			"request_id", middleware.GetReqID(r.Context()),
			"method", r.Method, "path", r.URL.Path, "err", err)
	}
	WriteJSON(w, e.Status, envelope{Error: envelopeBody{Code: e.Code, Message: e.Message, Fields: e.Fields}})
}

func toHTTPError(err error) *Error {
	var he *Error
	if errors.As(err, &he) {
		return he
	}
	var ce *apperr.CodedError
	if errors.As(err, &ce) {
		return &Error{Status: statusFor(ce.Kind), Code: ce.Code, Message: ce.Message, Fields: ce.Fields}
	}
	var ve *apperr.ValidationError
	if errors.As(err, &ve) {
		return &Error{Status: http.StatusBadRequest, Code: "validation_failed", Message: "Request validation failed.", Fields: ve.Fields}
	}
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		return &Error{Status: http.StatusNotFound, Code: "not_found", Message: capitalize(err.Error()) + "."}
	case errors.Is(err, apperr.ErrInvalidState):
		return &Error{Status: http.StatusConflict, Code: "invalid_state", Message: capitalize(err.Error()) + "."}
	}
	return errInternal
}

func statusFor(kind error) int {
	switch {
	case errors.Is(kind, apperr.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(kind, apperr.ErrInvalidState), errors.Is(kind, apperr.ErrConflict):
		return http.StatusConflict
	case errors.Is(kind, apperr.ErrUnprocessable):
		return http.StatusUnprocessableEntity
	case errors.Is(kind, apperr.ErrUnavailable):
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

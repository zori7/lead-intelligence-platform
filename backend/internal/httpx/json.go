// Package httpx holds HTTP plumbing shared by all handlers: JSON encoding and
// decoding, the error envelope, and middleware.
package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// MaxBodyBytes caps every request body.
const MaxBodyBytes = 64 << 10

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json response", "err", err)
	}
}

// DecodeJSON strictly decodes a single JSON object from the request body:
// the body is size-limited, unknown fields are rejected, and trailing data is
// an error. It also returns the raw body for checks the typed value cannot
// express (e.g. telling an explicit null from an absent field).
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		return nil, mapDecodeError(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return nil, mapDecodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, mapDecodeError(err)
		}
		return nil, withMessage(ErrInvalidJSON, "Request body must contain a single JSON object.")
	}
	return raw, nil
}

func mapDecodeError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return ErrPayloadTooLarge
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		e := &Error{Status: http.StatusBadRequest, Code: "validation_failed", Message: "A field has the wrong type."}
		// The field name is unknown when the error comes from a custom
		// UnmarshalJSON (e.g. optional PATCH fields).
		if typeErr.Field != "" {
			e.Message = "Request validation failed."
			e.Fields = map[string]string{typeErr.Field: "has the wrong type"}
		}
		return e
	}
	if errors.Is(err, io.EOF) {
		return withMessage(ErrInvalidJSON, "Request body is empty.")
	}
	// Unknown fields, syntax errors, etc. The decoder's message is safe to expose
	// (it names the offending field or position, never server internals).
	return withMessage(ErrInvalidJSON, "Invalid JSON: "+err.Error()+".")
}

func withMessage(e *Error, msg string) *Error {
	c := *e
	c.Message = msg
	return &c
}

// Package apperr defines transport-agnostic error kinds shared by domain
// packages. Domain packages wrap these kinds in their own sentinels
// (e.g. lead.ErrNotFound) and httpx maps the kinds to HTTP responses in one place.
package apperr

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

var (
	// ErrNotFound means the requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalidState means the entity exists but the operation is not allowed in its current state.
	ErrInvalidState = errors.New("invalid state")
	// ErrConflict means the request conflicts with the system's configuration or state
	// in a way the caller can fix (e.g. AI not configured yet).
	ErrConflict = errors.New("conflict")
	// ErrUnprocessable means the request is well-formed but refers to something that
	// does not exist outside this service (e.g. a model the LLM server does not list).
	ErrUnprocessable = errors.New("unprocessable")
	// ErrUnavailable means a dependency needed to complete the request is unreachable.
	ErrUnavailable = errors.New("unavailable")
)

// CodedError is a domain error with a stable machine-readable code and a
// user-safe message. Kind is one of the kinds above and decides the HTTP status.
type CodedError struct {
	Kind    error
	Code    string
	Message string
	Fields  map[string]string
}

func (e *CodedError) Error() string { return e.Code + ": " + e.Message }
func (e *CodedError) Unwrap() error { return e.Kind }

// ValidationError carries per-field, user-safe messages.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := slices.Sorted(maps.Keys(e.Fields))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+e.Fields[k])
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// Fields accumulates field errors; Err returns nil when nothing was added.
type Fields map[string]string

// Add records msg for field unless the field already has an error,
// so the first (most fundamental) problem wins.
func (f Fields) Add(field, msg string) {
	if _, ok := f[field]; !ok {
		f[field] = msg
	}
}

func (f Fields) Err() error {
	if len(f) == 0 {
		return nil
	}
	return &ValidationError{Fields: f}
}

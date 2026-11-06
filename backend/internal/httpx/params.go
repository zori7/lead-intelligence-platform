package httpx

import (
	"errors"
	"net/http"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
)

// ParamError is the ErrorHandlerFunc for the generated router. It maps
// parameter binding failures to the standard error envelope.
//
// A malformed path ID means the resource cannot exist, so it is a 404 rather
// than a validation error.
func ParamError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *api.InvalidParamFormatError
	if errors.As(err, &invalid) {
		if invalid.ParamName == "id" {
			WriteError(w, r, ErrNotFoundRoute)
			return
		}
		WriteError(w, r, &apperr.ValidationError{Fields: map[string]string{invalid.ParamName: "has the wrong type"}})
		return
	}
	var required *api.RequiredParamError
	if errors.As(err, &required) {
		WriteError(w, r, &apperr.ValidationError{Fields: map[string]string{required.ParamName: "is required"}})
		return
	}
	var tooMany *api.TooManyValuesForParamError
	if errors.As(err, &tooMany) {
		WriteError(w, r, &apperr.ValidationError{Fields: map[string]string{tooMany.ParamName: "must be given once"}})
		return
	}
	WriteError(w, r, withMessage(ErrInvalidRequest, "Invalid request parameters."))
}

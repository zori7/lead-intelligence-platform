// Package apitest checks HTTP responses against the embedded OpenAPI spec.
// It is used by contract tests only.
package apitest

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"leadintel/backend/internal/api"
)

// CheckResponse fails the test unless rec's status is documented for
// method + specPath and its body validates against the documented schema
// (which forbids missing and extra fields).
func CheckResponse(t *testing.T, method, specPath string, rec *httptest.ResponseRecorder) {
	t.Helper()
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	path := spec.Paths.Find(specPath)
	if path == nil {
		t.Fatalf("spec has no path %s", specPath)
	}
	op := path.GetOperation(method)
	if op == nil {
		t.Fatalf("spec has no %s %s", method, specPath)
	}
	resp := op.Responses.Status(rec.Code)
	if resp == nil {
		t.Fatalf("status %d is not documented for %s %s (body: %s)", rec.Code, method, specPath, rec.Body)
	}
	media := resp.Value.Content.Get("application/json")
	if media == nil {
		if rec.Body.Len() != 0 {
			t.Fatalf("status %d documents no body, got %s", rec.Code, rec.Body)
		}
		return
	}
	var body any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body)
	}
	if err := media.Schema.Value.VisitJSON(body, openapi3.MultiErrors()); err != nil {
		t.Fatalf("response does not match spec for %d:\n%v\nbody: %s", rec.Code, err, rec.Body)
	}
}

// CheckSchema validates v (marshalled to JSON) against a named component schema.
func CheckSchema(t *testing.T, schema string, v any) {
	t.Helper()
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	ref := spec.Components.Schemas[schema]
	if ref == nil {
		t.Fatalf("spec has no schema %s", schema)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if err := ref.Value.VisitJSON(body, openapi3.MultiErrors()); err != nil {
		t.Fatalf("value does not match schema %s:\n%v\njson: %s", schema, err, raw)
	}
}

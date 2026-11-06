package api

import (
	"context"
	"testing"
)

// TestSpecIsValid lints api/openapi.yaml (via the embedded copy): structure,
// references, defaults and examples must be valid OpenAPI 3.0.
func TestSpecIsValid(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatalf("load embedded spec: %v", err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("invalid OpenAPI spec: %v", err)
	}
}

package analysis

import (
	"fmt"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
)

// The history limit is read from the embedded OpenAPI spec, so the server and
// the generated frontend schemas share one definition.
var historyLimit struct{ min, max, def int }

func init() {
	spec, err := api.GetSwagger()
	if err != nil {
		panic(fmt.Sprintf("analysis: load embedded OpenAPI spec: %v", err))
	}
	path := spec.Paths.Find("/leads/{id}/analyses")
	if path == nil || path.Get == nil {
		panic("analysis: OpenAPI operation GET /leads/{id}/analyses not found")
	}
	p := path.Get.Parameters.GetByInAndName("query", "limit")
	if p == nil || p.Schema == nil || p.Schema.Value.Min == nil || p.Schema.Value.Max == nil {
		panic("analysis: OpenAPI GET /leads/{id}/analyses needs a bounded limit parameter")
	}
	s := p.Schema.Value
	def, ok := s.Default.(float64)
	if !ok {
		panic("analysis: OpenAPI limit parameter needs a default")
	}
	historyLimit.min, historyLimit.max, historyLimit.def = int(*s.Min), int(*s.Max), int(def)
}

func parseLimit(v *int) (int, error) {
	if v == nil {
		return historyLimit.def, nil
	}
	if *v < historyLimit.min || *v > historyLimit.max {
		return 0, &apperr.ValidationError{Fields: map[string]string{
			"limit": fmt.Sprintf("must be between %d and %d", historyLimit.min, historyLimit.max),
		}}
	}
	return *v, nil
}

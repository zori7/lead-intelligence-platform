package lead

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
)

// Validation rules are read from the embedded OpenAPI spec (api/openapi.yaml),
// the same document the frontend's zod schemas are generated from, so a limit
// changed in the spec applies to the server and the browser alike.
//
// Loading fails fast: a field missing from the spec panics at startup (and in
// every test run) instead of silently skipping validation.

// fieldRule is the validation contract of one request field.
type fieldRule struct {
	nullable   bool
	maxLen     int // characters (code points); 0 = unbounded
	pattern    *regexp.Regexp
	patternMsg string
	enum       []string
}

// leadRules holds the rules for every editable lead field.
type leadRules struct {
	companyName, website, contactName, contactEmail, contactTitle,
	industry, companySize, notes, status fieldRule
}

type listRules struct {
	qMaxLen                          int
	limitMin, limitMax, limitDefault int
	offsetMin                        int
	sort                             fieldRule
	status                           fieldRule
}

var (
	createRules leadRules
	// patchNullable records which PATCH fields accept null.
	patchNullable map[string]bool
	listParams    listRules
)

func init() {
	spec, err := api.GetSwagger()
	if err != nil {
		panic(fmt.Sprintf("lead: load embedded OpenAPI spec: %v", err))
	}
	createRules = mustLeadRules(spec, "LeadCreate")

	patch := mustObjectRules(spec, "LeadPatch")
	patchNullable = make(map[string]bool, len(patch))
	for field, r := range patch {
		patchNullable[field] = r.nullable
	}

	listParams = mustListRules(spec)
}

func mustLeadRules(spec *openapi3.T, schema string) leadRules {
	m := mustObjectRules(spec, schema)
	get := func(field string) fieldRule {
		r, ok := m[field]
		if !ok {
			panic(fmt.Sprintf("lead: OpenAPI schema %s has no property %q", schema, field))
		}
		return r
	}
	return leadRules{
		companyName:  get("company_name"),
		website:      get("website"),
		contactName:  get("contact_name"),
		contactEmail: get("contact_email"),
		contactTitle: get("contact_title"),
		industry:     get("industry"),
		companySize:  get("company_size"),
		notes:        get("notes"),
		status:       get("status"),
	}
}

func mustObjectRules(spec *openapi3.T, schema string) map[string]fieldRule {
	ref := spec.Components.Schemas[schema]
	if ref == nil || ref.Value == nil {
		panic(fmt.Sprintf("lead: OpenAPI schema %s not found", schema))
	}
	rules := make(map[string]fieldRule, len(ref.Value.Properties))
	for field, prop := range ref.Value.Properties {
		rules[field] = ruleFrom(prop.Value)
	}
	return rules
}

func mustListRules(spec *openapi3.T) listRules {
	path := spec.Paths.Find("/leads")
	if path == nil || path.Get == nil {
		panic("lead: OpenAPI operation GET /leads not found")
	}
	var lr listRules
	seen := map[string]bool{}
	for _, p := range path.Get.Parameters {
		s := p.Value.Schema.Value
		seen[p.Value.Name] = true
		switch p.Value.Name {
		case "q":
			lr.qMaxLen = ruleFrom(s).maxLen
		case "limit":
			lr.limitMin, lr.limitMax = int(*s.Min), int(*s.Max)
			lr.limitDefault = int(s.Default.(float64))
		case "offset":
			lr.offsetMin = int(*s.Min)
		case "sort":
			lr.sort = ruleFrom(s)
		case "status":
			lr.status = ruleFrom(s)
		}
	}
	for _, name := range []string{"q", "limit", "offset", "sort", "status"} {
		if !seen[name] {
			panic(fmt.Sprintf("lead: OpenAPI GET /leads has no %q parameter", name))
		}
	}
	return lr
}

func ruleFrom(s *openapi3.Schema) fieldRule {
	r := fieldRule{nullable: s.Nullable}
	if s.MaxLength != nil {
		r.maxLen = int(*s.MaxLength)
	}
	if s.Pattern != "" {
		r.pattern = regexp.MustCompile(s.Pattern)
		r.patternMsg = "has an invalid format"
		if msg, ok := s.Extensions["x-pattern-message"].(string); ok {
			r.patternMsg = msg
		}
	}
	// Nullable enums are expressed as `nullable: true` + `allOf: [$ref Enum]`.
	enumSrc := s
	if len(s.Enum) == 0 && len(s.AllOf) == 1 && s.AllOf[0].Value != nil {
		enumSrc = s.AllOf[0].Value
	}
	for _, e := range enumSrc.Enum {
		r.enum = append(r.enum, fmt.Sprint(e))
	}
	return r
}

// text trims an optional string, maps "" to nil, and enforces the length and pattern.
func (r fieldRule) text(fe apperr.Fields, field string, v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	if r.maxLen > 0 && utf8.RuneCountInString(s) > r.maxLen {
		fe.Add(field, fmt.Sprintf("must be at most %d characters", r.maxLen))
	}
	if r.pattern != nil && !r.pattern.MatchString(s) {
		fe.Add(field, r.patternMsg)
	}
	return &s
}

// required is text for a mandatory field: blank input is an error.
func (r fieldRule) required(fe apperr.Fields, field, v string) string {
	s := r.text(fe, field, &v)
	if s == nil {
		fe.Add(field, "is required")
		return ""
	}
	return *s
}

// oneOf checks v against the field's enum.
func (r fieldRule) oneOf(fe apperr.Fields, field, v string) {
	if !slices.Contains(r.enum, v) {
		fe.Add(field, "must be one of: "+strings.Join(r.enum, ", "))
	}
}

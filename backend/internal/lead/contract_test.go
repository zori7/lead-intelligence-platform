package lead

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	"leadintel/backend/internal/api"
)

// TestResponsesMatchSpec drives every lead operation through the real router
// and checks each response against api/openapi.yaml: the status code must be
// documented for that operation and the body must validate against its schema
// (which forbids missing and extra fields). This keeps the hand-written Lead
// struct and error envelope in lockstep with the contract the frontend is
// generated from.
func TestResponsesMatchSpec(t *testing.T) {
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}

	full := fullLead()
	id := full.ID.String()
	unknown := uuid.NewString()

	tests := []struct {
		name, method, url, body string
		specPath                string
	}{
		{"list", "GET", "/api/leads", "", "/leads"},
		{"list invalid", "GET", "/api/leads?limit=0", "", "/leads"},
		{"create", "POST", "/api/leads", `{"company_name":"Acme"}`, "/leads"},
		{"create invalid", "POST", "/api/leads", `{"company_name":""}`, "/leads"},
		{"create too large", "POST", "/api/leads", `{"notes":"` + strings.Repeat("x", 70<<10) + `"}`, "/leads"},
		{"get", "GET", "/api/leads/" + id, "", "/leads/{id}"},
		{"get unknown", "GET", "/api/leads/" + unknown, "", "/leads/{id}"},
		{"patch", "PATCH", "/api/leads/" + id, `{"status":"won"}`, "/leads/{id}"},
		{"patch invalid", "PATCH", "/api/leads/" + id, `{"status":null}`, "/leads/{id}"},
		{"patch unknown", "PATCH", "/api/leads/" + unknown, `{}`, "/leads/{id}"},
		{"delete", "DELETE", "/api/leads/" + id, "", "/leads/{id}"},
		{"delete unknown", "DELETE", "/api/leads/" + unknown, "", "/leads/{id}"},
	}

	// Run each case against both a fully populated lead and an all-nulls lead.
	for _, l := range []*Lead{full, minimalLead(full.ID)} {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec, _ := do(t, &fakeRepo{lead: l}, tt.method, tt.url, tt.body)

				op := spec.Paths.Find(tt.specPath).GetOperation(tt.method)
				if op == nil {
					t.Fatalf("spec has no %s %s", tt.method, tt.specPath)
				}
				resp := op.Responses.Status(rec.Code)
				if resp == nil {
					t.Fatalf("status %d is not documented for %s %s (body: %s)", rec.Code, tt.method, tt.specPath, rec.Body)
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
			})
		}
	}
}

func fullLead() *Lead {
	s := func(v string) *string { return &v }
	size, tier, score := api.Size51To200, api.TierHot, int16(87)
	now := time.Now().UTC()
	return &Lead{
		ID: uuid.New(), CompanyName: "Acme", Website: s("https://acme.io"),
		ContactName: s("Ann Lee"), ContactEmail: s("ann@acme.io"), ContactTitle: s("CTO"),
		Industry: s("SaaS"), CompanySize: &size, Notes: s("Warm intro"), Status: api.StatusQualified,
		AIScore: &score, AITier: &tier, LastAnalyzedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
}

func minimalLead(id uuid.UUID) *Lead {
	now := time.Now().UTC()
	return &Lead{ID: id, CompanyName: "Acme", Status: api.StatusNew, CreatedAt: now, UpdatedAt: now}
}

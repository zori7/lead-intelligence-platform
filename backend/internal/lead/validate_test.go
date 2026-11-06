package lead

import (
	"errors"
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
)

func ptrTo[T any](v T) *T                 { return &v }
func val[T any](v T) nullable.Nullable[T] { return nullable.NewNullableWithValue(v) }
func null[T any]() nullable.Nullable[T]   { return nullable.NewNullNullable[T]() }

func fieldErrors(t *testing.T, err error) map[string]string {
	t.Helper()
	if err == nil {
		return nil
	}
	var ve *apperr.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	return ve.Fields
}

func TestValidateCreate(t *testing.T) {
	tests := []struct {
		name      string
		req       api.LeadCreate
		wantField string // "" means valid
	}{
		{"minimal", api.LeadCreate{CompanyName: "Acme"}, ""},
		{"full", api.LeadCreate{
			CompanyName: "Acme", Website: val("https://acme.io"), ContactName: val("Ann"),
			ContactEmail: val("ann@acme.io"), ContactTitle: val("CTO"), Industry: val("SaaS"),
			CompanySize: val(api.Size51To200), Notes: val("Warm intro"), Status: ptrTo(api.StatusQualified),
		}, ""},
		{"explicit nulls", api.LeadCreate{CompanyName: "A", Website: null[string](), CompanySize: null[api.CompanySize]()}, ""},
		{"missing company", api.LeadCreate{}, "company_name"},
		{"blank company", api.LeadCreate{CompanyName: "   "}, "company_name"},
		{"company too long", api.LeadCreate{CompanyName: strings.Repeat("a", 201)}, "company_name"},
		{"company at limit (multibyte)", api.LeadCreate{CompanyName: strings.Repeat("ä", 200)}, ""},
		{"website without scheme", api.LeadCreate{CompanyName: "A", Website: val("acme.io")}, "website"},
		{"website ftp", api.LeadCreate{CompanyName: "A", Website: val("ftp://acme.io")}, "website"},
		{"website too long", api.LeadCreate{CompanyName: "A", Website: val("https://a.io/" + strings.Repeat("x", 300))}, "website"},
		{"bad email", api.LeadCreate{CompanyName: "A", ContactEmail: val("ann@")}, "contact_email"},
		{"bad company size", api.LeadCreate{CompanyName: "A", CompanySize: val(api.CompanySize("huge"))}, "company_size"},
		{"bad status", api.LeadCreate{CompanyName: "A", Status: ptrTo(api.LeadStatus("archived"))}, "status"},
		{"notes too long", api.LeadCreate{CompanyName: "A", Notes: val(strings.Repeat("n", 5001))}, "notes"},
		{"industry too long", api.LeadCreate{CompanyName: "A", Industry: val(strings.Repeat("i", 101))}, "industry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateCreate(tt.req)
			fields := fieldErrors(t, err)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if _, ok := fields[tt.wantField]; !ok {
				t.Fatalf("expected error on %q, got %v", tt.wantField, fields)
			}
		})
	}
}

func TestValidateCreateMessagesComeFromSpec(t *testing.T) {
	_, err := ValidateCreate(api.LeadCreate{CompanyName: "A", ContactEmail: val("nope"), Website: val("nope")})
	fields := fieldErrors(t, err)
	if fields["contact_email"] != "must be a valid email address" || fields["website"] != "must be a valid http(s) URL" {
		t.Fatalf("x-pattern-message not applied: %v", fields)
	}
}

func TestValidateCreateNormalizes(t *testing.T) {
	f, err := ValidateCreate(api.LeadCreate{CompanyName: "  Acme  ", Industry: val("  "), Website: val(" https://acme.io ")})
	if err != nil {
		t.Fatal(err)
	}
	if f.CompanyName != "Acme" {
		t.Errorf("company_name not trimmed: %q", f.CompanyName)
	}
	if f.Industry != nil {
		t.Errorf("blank industry should become nil, got %q", *f.Industry)
	}
	if f.Website == nil || *f.Website != "https://acme.io" {
		t.Errorf("website not trimmed: %v", f.Website)
	}
	if f.Status != api.StatusNew {
		t.Errorf("default status = %q, want new", f.Status)
	}
}

func TestValidatePatch(t *testing.T) {
	tests := []struct {
		name        string
		req         api.LeadPatch
		raw         string
		wantField   string
		wantColumns []string
	}{
		{"empty", api.LeadPatch{}, `{}`, "", nil},
		{"status only", api.LeadPatch{Status: ptrTo(api.StatusWon)}, `{"status":"won"}`, "", []string{"status"}},
		{"clear website", api.LeadPatch{Website: null[string]()}, `{"website":null}`, "", []string{"website"}},
		{"null status", api.LeadPatch{}, `{"status":null}`, "status", nil},
		{"null company", api.LeadPatch{}, `{"company_name":null}`, "company_name", nil},
		{"invalid status", api.LeadPatch{Status: ptrTo(api.LeadStatus("x"))}, `{"status":"x"}`, "status", nil},
		{"blank company", api.LeadPatch{CompanyName: ptrTo(" ")}, `{"company_name":" "}`, "company_name", nil},
		{"bad email", api.LeadPatch{ContactEmail: val("nope")}, `{"contact_email":"nope"}`, "contact_email", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes, err := ValidatePatch(tt.req, []byte(tt.raw))
			fields := fieldErrors(t, err)
			if tt.wantField != "" {
				if _, ok := fields[tt.wantField]; !ok {
					t.Fatalf("expected error on %q, got %v", tt.wantField, fields)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var cols []string
			for _, c := range changes {
				cols = append(cols, c.Column)
			}
			if strings.Join(cols, ",") != strings.Join(tt.wantColumns, ",") {
				t.Fatalf("columns = %v, want %v", cols, tt.wantColumns)
			}
		})
	}
}

func TestParseListParams(t *testing.T) {
	tests := []struct {
		name      string
		params    api.ListLeadsParams
		wantField string
	}{
		{"defaults", api.ListLeadsParams{}, ""},
		{"all set", api.ListLeadsParams{
			Status: ptrTo(api.StatusWon), Q: ptrTo("acme"), Sort: ptrTo(api.SortScoreDesc),
			Limit: ptrTo(100), Offset: ptrTo(10),
		}, ""},
		{"bad status", api.ListLeadsParams{Status: ptrTo(api.LeadStatus("archived"))}, "status"},
		{"bad sort", api.ListLeadsParams{Sort: ptrTo(api.LeadSort("name"))}, "sort"},
		{"limit zero", api.ListLeadsParams{Limit: ptrTo(0)}, "limit"},
		{"limit too big", api.ListLeadsParams{Limit: ptrTo(101)}, "limit"},
		{"negative offset", api.ListLeadsParams{Offset: ptrTo(-1)}, "offset"},
		{"query too long", api.ListLeadsParams{Q: ptrTo(strings.Repeat("q", 201))}, "q"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseListParams(tt.params)
			fields := fieldErrors(t, err)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.name == "defaults" && (p.Limit != 50 || p.Sort != api.SortCreatedDesc) {
					t.Fatalf("bad defaults: %+v", p)
				}
				return
			}
			if _, ok := fields[tt.wantField]; !ok {
				t.Fatalf("expected error on %q, got %v", tt.wantField, fields)
			}
		})
	}
}

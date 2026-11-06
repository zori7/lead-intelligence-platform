package lead

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"leadintel/backend/internal/apperr"
)

func ptr[T any](v T) *T { return &v }

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
		req       CreateRequest
		wantField string // "" means valid
	}{
		{"minimal", CreateRequest{CompanyName: "Acme"}, ""},
		{"full", CreateRequest{
			CompanyName: "Acme", Website: ptr("https://acme.io"), ContactName: ptr("Ann"),
			ContactEmail: ptr("ann@acme.io"), ContactTitle: ptr("CTO"), Industry: ptr("SaaS"),
			CompanySize: ptr(CompanySize("51-200")), Notes: ptr("Warm intro"), Status: ptr(StatusQualified),
		}, ""},
		{"missing company", CreateRequest{}, "company_name"},
		{"blank company", CreateRequest{CompanyName: "   "}, "company_name"},
		{"company too long", CreateRequest{CompanyName: strings.Repeat("a", 201)}, "company_name"},
		{"company at limit (multibyte)", CreateRequest{CompanyName: strings.Repeat("ä", 200)}, ""},
		{"website without scheme", CreateRequest{CompanyName: "A", Website: ptr("acme.io")}, "website"},
		{"website ftp", CreateRequest{CompanyName: "A", Website: ptr("ftp://acme.io")}, "website"},
		{"website too long", CreateRequest{CompanyName: "A", Website: ptr("https://a.io/" + strings.Repeat("x", 300))}, "website"},
		{"bad email", CreateRequest{CompanyName: "A", ContactEmail: ptr("ann@")}, "contact_email"},
		{"bad company size", CreateRequest{CompanyName: "A", CompanySize: ptr(CompanySize("huge"))}, "company_size"},
		{"bad status", CreateRequest{CompanyName: "A", Status: ptr(Status("archived"))}, "status"},
		{"notes too long", CreateRequest{CompanyName: "A", Notes: ptr(strings.Repeat("n", 5001))}, "notes"},
		{"industry too long", CreateRequest{CompanyName: "A", Industry: ptr(strings.Repeat("i", 101))}, "industry"},
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

func TestValidateCreateNormalizes(t *testing.T) {
	f, err := ValidateCreate(CreateRequest{CompanyName: "  Acme  ", Industry: ptr("  "), Website: ptr(" https://acme.io ")})
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
	if f.Status != StatusNew {
		t.Errorf("default status = %q, want new", f.Status)
	}
}

func TestValidatePatch(t *testing.T) {
	tests := []struct {
		name        string
		req         PatchRequest
		wantField   string
		wantColumns []string
	}{
		{"empty", PatchRequest{}, "", nil},
		{"status only", PatchRequest{Status: Optional[Status]{Set: true, Value: ptr(StatusWon)}}, "", []string{"status"}},
		{"clear website", PatchRequest{Website: Optional[string]{Set: true}}, "", []string{"website"}},
		{"null status", PatchRequest{Status: Optional[Status]{Set: true}}, "status", nil},
		{"invalid status", PatchRequest{Status: Optional[Status]{Set: true, Value: ptr(Status("x"))}}, "status", nil},
		{"null company", PatchRequest{CompanyName: Optional[string]{Set: true}}, "company_name", nil},
		{"bad email", PatchRequest{ContactEmail: Optional[string]{Set: true, Value: ptr("nope")}}, "contact_email", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes, err := ValidatePatch(tt.req)
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
		query     string
		wantField string
	}{
		{"", ""},
		{"status=won&q=acme&sort=score_desc&limit=100&offset=10", ""},
		{"status=archived", "status"},
		{"sort=name", "sort"},
		{"limit=0", "limit"},
		{"limit=101", "limit"},
		{"limit=abc", "limit"},
		{"offset=-1", "offset"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			q, _ := url.ParseQuery(tt.query)
			p, err := ParseListParams(q)
			fields := fieldErrors(t, err)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.query == "" && (p.Limit != 50 || p.Sort != SortCreatedDesc) {
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

package lead

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/oapi-codegen/nullable"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
)

// ValidateCreate normalizes and validates a create request. Optional text
// fields are trimmed and empty strings become null. Limits come from the spec
// (see rules.go).
func ValidateCreate(req api.LeadCreate) (Fields, error) {
	fe := apperr.Fields{}
	r := createRules
	f := Fields{
		CompanyName:  r.companyName.required(fe, "company_name", req.CompanyName),
		Website:      r.website.text(fe, "website", ptr(req.Website)),
		ContactName:  r.contactName.text(fe, "contact_name", ptr(req.ContactName)),
		ContactEmail: r.contactEmail.text(fe, "contact_email", ptr(req.ContactEmail)),
		ContactTitle: r.contactTitle.text(fe, "contact_title", ptr(req.ContactTitle)),
		Industry:     r.industry.text(fe, "industry", ptr(req.Industry)),
		CompanySize:  ptr(req.CompanySize),
		Notes:        r.notes.text(fe, "notes", ptr(req.Notes)),
		Status:       api.StatusNew,
	}
	if f.CompanySize != nil {
		r.companySize.oneOf(fe, "company_size", string(*f.CompanySize))
	}
	if req.Status != nil {
		f.Status = *req.Status
		r.status.oneOf(fe, "status", string(f.Status))
	}
	return f, fe.Err()
}

// ValidatePatch normalizes and validates a partial update and returns the
// column assignments to apply; only fields present in the request appear.
// raw is the request body, used to reject null on fields the spec marks
// non-nullable (the generated type cannot tell null from absent for those).
func ValidatePatch(req api.LeadPatch, raw []byte) ([]Change, error) {
	fe := apperr.Fields{}
	rejectNulls(fe, raw)

	var changes []Change
	set := func(column string, v any) { changes = append(changes, Change{Column: column, Value: v}) }
	r := createRules

	if req.CompanyName != nil {
		set("company_name", r.companyName.required(fe, "company_name", *req.CompanyName))
	}
	if req.Website.IsSpecified() {
		set("website", r.website.text(fe, "website", ptr(req.Website)))
	}
	if req.ContactName.IsSpecified() {
		set("contact_name", r.contactName.text(fe, "contact_name", ptr(req.ContactName)))
	}
	if req.ContactEmail.IsSpecified() {
		set("contact_email", r.contactEmail.text(fe, "contact_email", ptr(req.ContactEmail)))
	}
	if req.ContactTitle.IsSpecified() {
		set("contact_title", r.contactTitle.text(fe, "contact_title", ptr(req.ContactTitle)))
	}
	if req.Industry.IsSpecified() {
		set("industry", r.industry.text(fe, "industry", ptr(req.Industry)))
	}
	if req.CompanySize.IsSpecified() {
		size := ptr(req.CompanySize)
		if size != nil {
			r.companySize.oneOf(fe, "company_size", string(*size))
		}
		set("company_size", size)
	}
	if req.Notes.IsSpecified() {
		set("notes", r.notes.text(fe, "notes", ptr(req.Notes)))
	}
	if req.Status != nil {
		r.status.oneOf(fe, "status", string(*req.Status))
		set("status", *req.Status)
	}

	if err := fe.Err(); err != nil {
		return nil, err
	}
	return changes, nil
}

// ParseListParams validates the query parameters of GET /api/leads (already
// type-converted by the generated router) against the spec's limits.
func ParseListParams(p api.ListLeadsParams) (ListParams, error) {
	fe := apperr.Fields{}
	lr := listParams
	out := ListParams{Sort: api.SortCreatedDesc, Limit: lr.limitDefault}

	if p.Status != nil {
		lr.status.oneOf(fe, "status", string(*p.Status))
		out.Status = p.Status
	}
	if q := (fieldRule{maxLen: lr.qMaxLen}).text(fe, "q", p.Q); q != nil {
		out.Query = *q
	}
	if p.Sort != nil {
		lr.sort.oneOf(fe, "sort", string(*p.Sort))
		out.Sort = *p.Sort
	}
	if p.Limit != nil {
		if *p.Limit < lr.limitMin || *p.Limit > lr.limitMax {
			fe.Add("limit", fmt.Sprintf("must be an integer between %d and %d", lr.limitMin, lr.limitMax))
		}
		out.Limit = *p.Limit
	}
	if p.Offset != nil {
		if *p.Offset < lr.offsetMin {
			fe.Add("offset", fmt.Sprintf("must be an integer of at least %d", lr.offsetMin))
		}
		out.Offset = *p.Offset
	}
	return out, fe.Err()
}

// rejectNulls flags explicit nulls on PATCH fields that the spec marks non-nullable.
func rejectNulls(fe apperr.Fields, raw []byte) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return // the typed decode already accepted the body; nothing more to check
	}
	for field, v := range m {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) && !patchNullable[field] {
			fe.Add(field, "must not be null")
		}
	}
}

// ptr converts a Nullable to a pointer: absent and null both become nil.
func ptr[T any](n nullable.Nullable[T]) *T {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v := n.MustGet()
	return &v
}

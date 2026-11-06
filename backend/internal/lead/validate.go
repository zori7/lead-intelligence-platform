package lead

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"leadintel/backend/internal/apperr"
)

// Field limits (characters). Mirrored by DB CHECK constraints and the frontend zod schema.
const (
	maxCompanyName  = 200
	maxWebsite      = 300
	maxContactName  = 200
	maxContactEmail = 320
	maxContactTitle = 200
	maxIndustry     = 100
	maxNotes        = 5000

	defaultListLimit = 50
	maxListLimit     = 100
)

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// ValidateCreate normalizes and validates a create request.
// Optional text fields are trimmed and empty strings become null.
func ValidateCreate(req CreateRequest) (Fields, error) {
	fe := apperr.Fields{}
	f := Fields{
		CompanyName:  companyName(fe, &req.CompanyName),
		Website:      website(fe, req.Website),
		ContactName:  text(fe, "contact_name", req.ContactName, maxContactName),
		ContactEmail: email(fe, req.ContactEmail),
		ContactTitle: text(fe, "contact_title", req.ContactTitle, maxContactTitle),
		Industry:     text(fe, "industry", req.Industry, maxIndustry),
		CompanySize:  companySize(fe, req.CompanySize),
		Notes:        text(fe, "notes", req.Notes, maxNotes),
		Status:       StatusNew,
	}
	if req.Status != nil {
		f.Status = status(fe, *req.Status)
	}
	return f, fe.Err()
}

// Change is a single validated column assignment for a partial update.
type Change struct {
	Column string
	Value  any
}

// ValidatePatch normalizes and validates a partial update and returns the
// column assignments to apply. Only fields present in the request appear.
func ValidatePatch(p PatchRequest) ([]Change, error) {
	fe := apperr.Fields{}
	var changes []Change
	set := func(column string, v any) { changes = append(changes, Change{Column: column, Value: v}) }

	if p.CompanyName.Set {
		set("company_name", companyName(fe, p.CompanyName.Value))
	}
	if p.Website.Set {
		set("website", website(fe, p.Website.Value))
	}
	if p.ContactName.Set {
		set("contact_name", text(fe, "contact_name", p.ContactName.Value, maxContactName))
	}
	if p.ContactEmail.Set {
		set("contact_email", email(fe, p.ContactEmail.Value))
	}
	if p.ContactTitle.Set {
		set("contact_title", text(fe, "contact_title", p.ContactTitle.Value, maxContactTitle))
	}
	if p.Industry.Set {
		set("industry", text(fe, "industry", p.Industry.Value, maxIndustry))
	}
	if p.CompanySize.Set {
		set("company_size", companySize(fe, p.CompanySize.Value))
	}
	if p.Notes.Set {
		set("notes", text(fe, "notes", p.Notes.Value, maxNotes))
	}
	if p.Status.Set {
		if p.Status.Value == nil {
			fe.Add("status", "must not be null")
		} else {
			set("status", status(fe, *p.Status.Value))
		}
	}

	if err := fe.Err(); err != nil {
		return nil, err
	}
	return changes, nil
}

// ParseListParams validates the query string of GET /api/leads.
func ParseListParams(q url.Values) (ListParams, error) {
	fe := apperr.Fields{}
	p := ListParams{
		Query: strings.TrimSpace(q.Get("q")),
		Sort:  SortCreatedDesc,
		Limit: defaultListLimit,
	}

	if s := q.Get("status"); s != "" {
		st := status(fe, Status(s))
		p.Status = &st
	}
	if s := q.Get("sort"); s != "" {
		switch Sort(s) {
		case SortCreatedDesc, SortScoreDesc:
			p.Sort = Sort(s)
		default:
			fe.Add("sort", fmt.Sprintf("must be one of: %s, %s", SortCreatedDesc, SortScoreDesc))
		}
	}
	if utf8.RuneCountInString(p.Query) > maxCompanyName {
		fe.Add("q", fmt.Sprintf("must be at most %d characters", maxCompanyName))
	}
	p.Limit = intParam(fe, q, "limit", defaultListLimit, 1, maxListLimit)
	p.Offset = intParam(fe, q, "offset", 0, 0, 1<<31-1)

	return p, fe.Err()
}

func companyName(fe apperr.Fields, v *string) string {
	s := text(fe, "company_name", v, maxCompanyName)
	if s == nil {
		fe.Add("company_name", "is required")
		return ""
	}
	return *s
}

func website(fe apperr.Fields, v *string) *string {
	s := text(fe, "website", v, maxWebsite)
	if s == nil {
		return nil
	}
	u, err := url.Parse(*s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fe.Add("website", "must be a valid http(s) URL")
	}
	return s
}

func email(fe apperr.Fields, v *string) *string {
	s := text(fe, "contact_email", v, maxContactEmail)
	if s != nil && !emailRe.MatchString(*s) {
		fe.Add("contact_email", "must be a valid email address")
	}
	return s
}

func companySize(fe apperr.Fields, v *CompanySize) *CompanySize {
	if v == nil || *v == "" {
		return nil
	}
	if !slices.Contains(CompanySizes, *v) {
		fe.Add("company_size", "must be one of: "+join(CompanySizes))
	}
	return v
}

func status(fe apperr.Fields, v Status) Status {
	if !slices.Contains(Statuses, v) {
		fe.Add("status", "must be one of: "+join(Statuses))
	}
	return v
}

// text trims an optional string, maps "" to nil, and enforces a character limit.
func text(fe apperr.Fields, field string, v *string, maxLen int) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	if utf8.RuneCountInString(s) > maxLen {
		fe.Add(field, fmt.Sprintf("must be at most %d characters", maxLen))
	}
	return &s
}

func intParam(fe apperr.Fields, q url.Values, key string, def, minV, maxV int) int {
	raw := q.Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minV || n > maxV {
		fe.Add(key, fmt.Sprintf("must be an integer between %d and %d", minV, maxV))
		return def
	}
	return n
}

func join[T ~string](vs []T) string {
	ss := make([]string, len(vs))
	for i, v := range vs {
		ss[i] = string(v)
	}
	return strings.Join(ss, ", ")
}

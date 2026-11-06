// Package lead implements lead management: model, validation, persistence and HTTP handlers.
package lead

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"leadintel/backend/internal/apperr"
)

// ErrNotFound is returned when a lead does not exist.
var ErrNotFound = fmt.Errorf("lead %w", apperr.ErrNotFound)

type Status string

const (
	StatusNew       Status = "new"
	StatusContacted Status = "contacted"
	StatusQualified Status = "qualified"
	StatusProposal  Status = "proposal"
	StatusWon       Status = "won"
	StatusLost      Status = "lost"
)

var Statuses = []Status{StatusNew, StatusContacted, StatusQualified, StatusProposal, StatusWon, StatusLost}

type CompanySize string

var CompanySizes = []CompanySize{"1-10", "11-50", "51-200", "201-1000", "1000+"}

type Tier string

type Lead struct {
	ID             uuid.UUID    `json:"id"               db:"id"`
	CompanyName    string       `json:"company_name"     db:"company_name"`
	Website        *string      `json:"website"          db:"website"`
	ContactName    *string      `json:"contact_name"     db:"contact_name"`
	ContactEmail   *string      `json:"contact_email"    db:"contact_email"`
	ContactTitle   *string      `json:"contact_title"    db:"contact_title"`
	Industry       *string      `json:"industry"         db:"industry"`
	CompanySize    *CompanySize `json:"company_size"     db:"company_size"`
	Notes          *string      `json:"notes"            db:"notes"`
	Status         Status       `json:"status"           db:"status"`
	AIScore        *int16       `json:"ai_score"         db:"ai_score"`
	AITier         *Tier        `json:"ai_tier"          db:"ai_tier"`
	LastAnalyzedAt *time.Time   `json:"last_analyzed_at" db:"last_analyzed_at"`
	CreatedAt      time.Time    `json:"created_at"       db:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"       db:"updated_at"`
}

// Fields is the set of user-editable lead fields. It is the validated form
// of both create and update requests.
type Fields struct {
	CompanyName  string
	Website      *string
	ContactName  *string
	ContactEmail *string
	ContactTitle *string
	Industry     *string
	CompanySize  *CompanySize
	Notes        *string
	Status       Status
}

// CreateRequest is the body of POST /api/leads.
type CreateRequest struct {
	CompanyName  string       `json:"company_name"`
	Website      *string      `json:"website"`
	ContactName  *string      `json:"contact_name"`
	ContactEmail *string      `json:"contact_email"`
	ContactTitle *string      `json:"contact_title"`
	Industry     *string      `json:"industry"`
	CompanySize  *CompanySize `json:"company_size"`
	Notes        *string      `json:"notes"`
	Status       *Status      `json:"status"`
}

// PatchRequest is the body of PATCH /api/leads/{id}. Each field distinguishes
// "absent" (leave unchanged) from "null" (clear) and a value (set).
type PatchRequest struct {
	CompanyName  Optional[string]      `json:"company_name"`
	Website      Optional[string]      `json:"website"`
	ContactName  Optional[string]      `json:"contact_name"`
	ContactEmail Optional[string]      `json:"contact_email"`
	ContactTitle Optional[string]      `json:"contact_title"`
	Industry     Optional[string]      `json:"industry"`
	CompanySize  Optional[CompanySize] `json:"company_size"`
	Notes        Optional[string]      `json:"notes"`
	Status       Optional[Status]      `json:"status"`
}

// Optional is a JSON field that records whether it was present in the input.
// Value is nil when the field was explicitly null.
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(b, []byte("null")) {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// ListParams are the validated query parameters of GET /api/leads.
type ListParams struct {
	Status *Status
	Query  string
	Sort   Sort
	Limit  int
	Offset int
}

type Sort string

const (
	SortCreatedDesc Sort = "created_desc"
	SortScoreDesc   Sort = "score_desc"
)

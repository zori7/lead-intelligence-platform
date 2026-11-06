// Package lead implements lead management: model, validation, persistence and HTTP handlers.
package lead

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
)

// ErrNotFound is returned when a lead does not exist.
var ErrNotFound = fmt.Errorf("lead %w", apperr.ErrNotFound)

// Enum types come from the OpenAPI spec (generated in internal/api).
type (
	Status      = api.LeadStatus
	CompanySize = api.CompanySize
	Tier        = api.Tier
	Sort        = api.LeadSort
)

// Lead is the persisted lead. Its JSON form is the `Lead` schema of the API;
// contract_test.go verifies the two stay identical.
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

// Fields is the validated, normalized set of editable fields for a new lead.
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

// Change is a single validated column assignment for a partial update.
type Change struct {
	Column string
	Value  any
}

// ListParams are the validated query parameters of GET /api/leads.
type ListParams struct {
	Status *Status
	Query  string
	Sort   Sort
	Limit  int
	Offset int
}

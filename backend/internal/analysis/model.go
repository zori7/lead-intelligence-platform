// Package analysis implements AI analysis jobs: creation and dedupe, the
// worker pool that dispatches jobs to the AI service, and the REST handlers
// (SPEC.md §4.2, §6.2, §6.4).
package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apperr"
	"leadintel/backend/internal/llmsettings"
)

var (
	// ErrNotFound is returned when an analysis job does not exist.
	ErrNotFound = fmt.Errorf("analysis %w", apperr.ErrNotFound)
	// ErrNotCancellable is returned when cancelling a job that has already finished.
	ErrNotCancellable = &apperr.CodedError{Kind: apperr.ErrInvalidState, Code: "invalid_state",
		Message: "The analysis has already finished."}
)

type (
	Status    = api.JobStatus
	StepName  = api.StepName
	StepState = api.StepState
)

// Steps lists the workflow steps in order (SPEC.md §9.1).
var Steps = []StepName{
	api.StepValidateInput,
	api.StepFetchWebsite,
	api.StepExtractCompany,
	api.StepAnalyzeFit,
	api.StepAnalyzeIntent,
	api.StepScoreLead,
	api.StepGenerateRecommendations,
	api.StepAggregate,
}

// Step is one entry of the job's step list.
type Step struct {
	Name  StepName  `json:"name"`
	State StepState `json:"state"`
}

// JobError is the user-facing failure of a job.
type JobError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// Job is an analysis job. Its JSON form is the `AnalysisJob` schema;
// contract_test.go verifies the two stay identical.
type Job struct {
	ID         uuid.UUID       `json:"id"`
	LeadID     uuid.UUID       `json:"lead_id"`
	Status     Status          `json:"status"`
	Steps      []Step          `json:"steps"`
	Progress   int             `json:"progress"`
	Result     json.RawMessage `json:"result"`
	Error      *JobError       `json:"error"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`

	// Internal fields, not part of the API.
	Input            LeadInput            `json:"-"`
	DispatchAttempts int                  `json:"-"`
	ModelConfig      llmsettings.Resolved `json:"-"`
}

// MarshalJSON keeps `result` null (not absent) when there is no result.
func (j Job) MarshalJSON() ([]byte, error) {
	type plain Job
	p := plain(j)
	if len(p.Result) == 0 {
		p.Result = json.RawMessage("null")
	}
	return json.Marshal(p)
}

// LeadInput is the lead snapshot taken when the job is created
// (analysis_jobs.input_snapshot). contact_email is intentionally absent:
// it is never sent to the AI service (SPEC.md §12).
type LeadInput struct {
	CompanyName  string `json:"company_name"`
	Website      string `json:"website,omitempty"`
	ContactName  string `json:"contact_name,omitempty"`
	ContactTitle string `json:"contact_title,omitempty"`
	Industry     string `json:"industry,omitempty"`
	CompanySize  string `json:"company_size,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

// ---- AI service boundary ----------------------------------------------------------

// Request is one dispatch of a job to the AI service.
type Request struct {
	JobID  uuid.UUID
	Lead   LeadInput
	Models llmsettings.Resolved
}

// Progress reports a step's state and the overall percentage.
type Progress struct {
	Step    StepName
	State   StepState
	Percent int
}

// Failure is a domain failure reported by the AI service.
type Failure struct {
	Code      string
	Message   string
	Retryable bool
}

// Event is one message on the analysis stream: exactly one field is set.
type Event struct {
	Progress  *Progress
	Completed *api.AnalysisResult
	Failed    *Failure
}

// Analyzer runs an analysis on the AI service and calls handle for every
// event, in order. It is implemented by aiclient and faked in tests.
//
// A nil error means the stream ended normally. Transport failures are
// returned wrapped in ErrAIUnavailable (worth retrying before the first event)
// or ErrAIProtocol; context errors are returned as-is.
type Analyzer interface {
	Analyze(ctx context.Context, req Request, handle func(Event) error) error
}

var (
	// ErrAIUnavailable means the AI service could not be reached or is overloaded.
	ErrAIUnavailable = errors.New("ai service unavailable")
	// ErrAIProtocol means the AI service broke the stream contract or failed internally.
	ErrAIProtocol = errors.New("ai service protocol error")
)

// Error codes written by Go itself (SPEC.md §11.1).
const (
	CodeAIUnavailable = "ai_unavailable"
	CodeTimeout       = "timeout"
	CodeInterrupted   = "interrupted"
	CodeInternal      = "internal"
)

// User-safe messages for the error codes Go writes (SPEC.md §11.1).
const (
	msgAIUnavailable = "Analysis service unavailable. Retry."
	msgTimeout       = "Analysis took too long. Retry."
	msgInterrupted   = "Analysis was interrupted. Retry."
	msgInternal      = "Analysis failed unexpectedly. Retry."
)

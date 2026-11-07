package analysis

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/httpx"
	"leadintel/backend/internal/lead"
	"leadintel/backend/internal/llmsettings"
)

// Repository is the job persistence the handler depends on (implemented by *Store).
type Repository interface {
	Create(ctx context.Context, leadID uuid.UUID, input LeadInput) (Job, bool, error)
	Get(ctx context.Context, id uuid.UUID) (Job, error)
	ListByLead(ctx context.Context, leadID uuid.UUID, limit int) ([]Job, error)
}

// LeadGetter loads a lead (implemented by lead.Store).
type LeadGetter interface {
	Get(ctx context.Context, id uuid.UUID) (lead.Lead, error)
}

// Waker is notified when a job is queued (implemented by *Worker).
type Waker interface{ Wake() }

// Handler implements the analysis operations of the generated api.ServerInterface.
type Handler struct {
	api.Unimplemented
	jobs     Repository
	leads    LeadGetter
	settings SettingsSource
	waker    Waker
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(jobs Repository, leads LeadGetter, settings SettingsSource, waker Waker) *Handler {
	return &Handler{jobs: jobs, leads: leads, settings: settings, waker: waker}
}

type listResponse struct {
	Items []Job `json:"items"`
}

func (h *Handler) TriggerAnalysis(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	l, err := h.leads.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !settings.Configured() {
		httpx.WriteError(w, r, llmsettings.ErrNotConfigured)
		return
	}
	job, created, err := h.jobs.Create(r.Context(), id, snapshot(l))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !created {
		httpx.WriteJSON(w, http.StatusOK, job)
		return
	}
	slog.InfoContext(r.Context(), "analysis queued", "job_id", job.ID, "lead_id", id)
	h.waker.Wake()
	httpx.WriteJSON(w, http.StatusAccepted, job)
}

func (h *Handler) ListLeadAnalyses(w http.ResponseWriter, r *http.Request, id uuid.UUID, params api.ListLeadAnalysesParams) {
	limit, err := parseLimit(params.Limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if _, err := h.leads.Get(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	jobs, err := h.jobs.ListByLead(r.Context(), id, limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{Items: jobs})
}

func (h *Handler) GetAnalysis(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	job, err := h.jobs.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

// snapshot copies the fields the AI service needs. contact_email is left out
// on purpose: it never leaves the Go service (SPEC.md §12).
func snapshot(l lead.Lead) LeadInput {
	s := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	in := LeadInput{
		CompanyName:  l.CompanyName,
		Website:      s(l.Website),
		ContactName:  s(l.ContactName),
		ContactTitle: s(l.ContactTitle),
		Industry:     s(l.Industry),
		Notes:        s(l.Notes),
	}
	if l.CompanySize != nil {
		in.CompanySize = string(*l.CompanySize)
	}
	return in
}

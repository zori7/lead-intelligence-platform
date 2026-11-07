package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
	Cancel(ctx context.Context, id uuid.UUID) (Job, error)
	CancelActive(ctx context.Context, leadID uuid.UUID) (Job, bool, error)
}

// LeadGetter loads a lead (implemented by lead.Store).
type LeadGetter interface {
	Get(ctx context.Context, id uuid.UUID) (lead.Lead, error)
}

// Runner runs queued jobs (implemented by *Worker): Wake signals a new job,
// Cancel aborts a job's AI run after its state has been changed in the database.
type Runner interface {
	Wake()
	Cancel(jobID uuid.UUID)
}

// Events provides live job events (implemented by *Broker).
type Events interface {
	Subscribe(jobID uuid.UUID) (<-chan StreamEvent, func())
	Publisher
}

// pingInterval keeps idle event streams alive through proxies (SPEC.md §7.4).
const pingInterval = 15 * time.Second

// Handler implements the analysis operations of the generated api.ServerInterface.
type Handler struct {
	api.Unimplemented
	jobs     Repository
	leads    LeadGetter
	settings SettingsSource
	runner   Runner
	events   Events
	ping     time.Duration
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(jobs Repository, leads LeadGetter, settings SettingsSource, runner Runner, events Events) *Handler {
	return &Handler{jobs: jobs, leads: leads, settings: settings, runner: runner, events: events, ping: pingInterval}
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
	h.runner.Wake()
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

// CancelAnalysis cancels a queued or running job. The database transition
// decides the race with the worker finishing the job: whichever conditional
// UPDATE commits first wins, and the loser changes nothing.
func (h *Handler) CancelAnalysis(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	job, err := h.jobs.Cancel(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.stopped(r.Context(), job)
	httpx.WriteJSON(w, http.StatusOK, job)
}

// CancelActive cancels the lead's active job, if any. The lead handler calls
// it before deleting a lead, so the AI run stops instead of finishing for a
// lead that no longer exists.
func (h *Handler) CancelActive(ctx context.Context, leadID uuid.UUID) error {
	job, ok, err := h.jobs.CancelActive(ctx, leadID)
	if err != nil || !ok {
		return err
	}
	h.stopped(ctx, job)
	return nil
}

// stopped aborts the run of a job that was just cancelled and tells its subscribers.
func (h *Handler) stopped(ctx context.Context, job Job) {
	h.runner.Cancel(job.ID)
	h.events.Publish(job.ID, StreamEvent{Name: EventCancelled, Data: job})
	slog.InfoContext(ctx, "analysis cancelled", "job_id", job.ID, "lead_id", job.LeadID)
}

// StreamAnalysisEvents streams a job's progress as Server-Sent Events
// (SPEC.md §7.4): a snapshot from the database first, then live events from
// the broker until a terminal event, the client leaving, or the broker
// dropping the subscriber (the client then reconnects and gets a new snapshot).
func (h *Handler) StreamAnalysisEvents(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	// Subscribe before reading the snapshot, so no event can fall in between.
	events, unsubscribe := h.events.Subscribe(id)
	defer unsubscribe()

	job, err := h.jobs.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	rc := http.NewResponseController(w)
	// A server-wide WriteTimeout would cut long streams; this one has no deadline.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		slog.WarnContext(r.Context(), "clear sse write deadline", "err", err)
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sse := sseWriter{w: w, rc: rc}
	if err := sse.event(EventSnapshot, job); err != nil {
		return
	}
	if name := terminalEvent(job.Status); name != "" {
		_ = sse.event(name, job)
		return
	}

	ping := time.NewTicker(h.ping)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := sse.event(ev.Name, ev.Data); err != nil {
				return
			}
			if ev.Name == EventCompleted || ev.Name == EventFailed || ev.Name == EventCancelled {
				return
			}
		case <-ping.C:
			if err := sse.comment("ping"); err != nil {
				return
			}
		}
	}
}

// sseWriter writes Server-Sent Events and flushes each one immediately.
type sseWriter struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (s sseWriter) event(name string, data any) error {
	body, err := json.Marshal(data) // single line: compact JSON has no newlines
	if err != nil {
		return fmt.Errorf("encode %s event: %w", name, err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, body); err != nil {
		return err
	}
	return s.rc.Flush()
}

func (s sseWriter) comment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	return s.rc.Flush()
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

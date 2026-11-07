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
}

// LeadGetter loads a lead (implemented by lead.Store).
type LeadGetter interface {
	Get(ctx context.Context, id uuid.UUID) (lead.Lead, error)
}

// Waker is notified when a job is queued (implemented by *Worker).
type Waker interface{ Wake() }

// Subscriber provides live job events (implemented by *Broker).
type Subscriber interface {
	Subscribe(jobID uuid.UUID) (<-chan StreamEvent, func())
}

// pingInterval keeps idle event streams alive through proxies (SPEC.md §7.4).
const pingInterval = 15 * time.Second

// Handler implements the analysis operations of the generated api.ServerInterface.
type Handler struct {
	api.Unimplemented
	jobs     Repository
	leads    LeadGetter
	settings SettingsSource
	waker    Waker
	events   Subscriber
	ping     time.Duration
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(jobs Repository, leads LeadGetter, settings SettingsSource, waker Waker, events Subscriber) *Handler {
	return &Handler{jobs: jobs, leads: leads, settings: settings, waker: waker, events: events, ping: pingInterval}
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

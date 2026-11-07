package analysis

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/llmsettings"
)

// JobStore is the persistence the worker depends on (implemented by *Store).
type JobStore interface {
	ClaimNext(ctx context.Context, models llmsettings.Resolved) (Job, bool, error)
	AddDispatchAttempt(ctx context.Context, id uuid.UUID) error
	UpdateProgress(ctx context.Context, id uuid.UUID, steps []Step, progress int) (bool, error)
	Complete(ctx context.Context, id uuid.UUID, steps []Step, result api.AnalysisResult) (bool, error)
	Fail(ctx context.Context, id uuid.UUID, steps []Step, e JobError) (bool, error)
}

// SettingsSource provides the current LLM settings (implemented by llmsettings.Store).
type SettingsSource interface {
	Get(ctx context.Context) (llmsettings.Settings, error)
}

// WorkerConfig tunes the worker pool (SPEC.md §13.1).
type WorkerConfig struct {
	Workers          int
	Timeout          time.Duration // per-job deadline, propagated to the AI service
	DispatchAttempts int           // attempts while the AI service is unavailable before the first event
	PollInterval     time.Duration // fallback when a wake signal is missed
	// Backoff is the wait after failed dispatch attempt n (1-based).
	Backoff func(attempt int) time.Duration
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.Workers <= 0 {
		c.Workers = 1
	}
	if c.DispatchAttempts <= 0 {
		c.DispatchAttempts = 1
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.Backoff == nil {
		c.Backoff = func(n int) time.Duration { return time.Second << (n - 1) } // 1s, 2s, 4s
	}
	return c
}

// terminalWriteTimeout bounds the final status write, which must happen even
// after the job's own deadline has passed.
const terminalWriteTimeout = 5 * time.Second

// Worker claims queued jobs from the database and runs them on the AI service.
// The analysis_jobs table is the queue: claims use FOR UPDATE SKIP LOCKED, so
// any number of workers (and API instances) can run safely.
type Worker struct {
	store    JobStore
	settings SettingsSource
	ai       Analyzer
	cfg      WorkerConfig
	wake     chan struct{}
}

func NewWorker(store JobStore, settings SettingsSource, ai Analyzer, cfg WorkerConfig) *Worker {
	return &Worker{store: store, settings: settings, ai: ai, cfg: cfg.withDefaults(), wake: make(chan struct{}, 1)}
}

// Wake signals that a job may be waiting. It never blocks.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run starts the pool and blocks until ctx is cancelled and all workers have stopped.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range w.cfg.Workers {
		wg.Go(func() { w.loop(ctx) })
	}
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		for ctx.Err() == nil && w.processNext(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// processNext claims and runs one job. It returns false when nothing was claimed.
func (w *Worker) processNext(ctx context.Context) bool {
	settings, err := w.settings.Get(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "load llm settings", "err", err)
		}
		return false
	}
	models, ok := llmsettings.Resolve(settings)
	if !ok {
		return false // triggering is blocked until configured; nothing to run
	}
	job, ok, err := w.store.ClaimNext(ctx, models)
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "claim analysis", "err", err)
		}
		return false
	}
	if !ok {
		return false
	}
	// Another job may be queued behind this one: let an idle worker look.
	w.Wake()
	w.run(ctx, job)
	return true
}

// run dispatches one claimed job and records its outcome. Every path ends in
// a terminal state: completed or failed by the AI service, or failed here.
func (w *Worker) run(ctx context.Context, job Job) {
	log := slog.With("job_id", job.ID, "lead_id", job.LeadID)
	jobCtx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()

	steps := newStepTracker(job.Steps)
	var received, terminal bool

	terminalCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(ctx), terminalWriteTimeout)
	}

	handle := func(ev Event) error {
		received = true
		switch {
		case ev.Progress != nil:
			steps.set(ev.Progress.Step, ev.Progress.State)
			if _, err := w.store.UpdateProgress(jobCtx, job.ID, steps.list(), ev.Progress.Percent); err != nil {
				return err
			}
		case ev.Completed != nil:
			terminal = true
			tctx, cancel := terminalCtx()
			defer cancel()
			if _, err := w.store.Complete(tctx, job.ID, steps.finish(true), *ev.Completed); err != nil {
				return err
			}
			log.Info("analysis succeeded", "score", ev.Completed.Score.Value)
		case ev.Failed != nil:
			terminal = true
			tctx, cancel := terminalCtx()
			defer cancel()
			f := ev.Failed
			if _, err := w.store.Fail(tctx, job.ID, steps.finish(false), JobError{Code: f.Code, Message: f.Message, Retryable: f.Retryable}); err != nil {
				return err
			}
			log.Info("analysis failed", "code", f.Code, "retryable", f.Retryable)
		}
		return nil
	}

	req := Request{JobID: job.ID, Lead: job.Input, Models: job.ModelConfig}
	var err error
	for attempt := 1; ; attempt++ {
		if aerr := w.store.AddDispatchAttempt(jobCtx, job.ID); aerr != nil {
			log.Warn("count dispatch attempt", "err", aerr)
		}
		err = w.ai.Analyze(jobCtx, req, handle)
		// Retry only before the first event: afterwards LLM spend has happened.
		if err == nil || received || !errors.Is(err, ErrAIUnavailable) || attempt >= w.cfg.DispatchAttempts {
			break
		}
		log.Warn("ai service unavailable, retrying", "attempt", attempt, "err", err)
		if !sleep(jobCtx, w.cfg.Backoff(attempt)) {
			break
		}
	}
	if terminal {
		return
	}

	jobErr := classify(ctx, jobCtx, err)
	log.Warn("analysis failed", "code", jobErr.Code, "err", err)
	tctx, cancel := terminalCtx()
	defer cancel()
	if _, ferr := w.store.Fail(tctx, job.ID, steps.finish(false), jobErr); ferr != nil {
		log.Error("record analysis failure", "err", ferr)
	}
}

// classify maps an unfinished dispatch to a user-facing error (SPEC.md §11.1).
func classify(parent, jobCtx context.Context, err error) JobError {
	switch {
	case parent.Err() != nil:
		return JobError{Code: CodeInterrupted, Message: "Analysis was interrupted. Retry.", Retryable: true}
	case errors.Is(jobCtx.Err(), context.DeadlineExceeded):
		return JobError{Code: CodeTimeout, Message: "Analysis took too long. Retry.", Retryable: true}
	case errors.Is(err, ErrAIUnavailable):
		return JobError{Code: CodeAIUnavailable, Message: "Analysis service unavailable. Retry.", Retryable: true}
	default: // stream ended without a terminal event, protocol error, or store error
		return JobError{Code: CodeInternal, Message: "Analysis failed unexpectedly. Retry.", Retryable: true}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// stepTracker holds a job's step states while it runs.
type stepTracker struct{ steps []Step }

func newStepTracker(steps []Step) *stepTracker {
	if len(steps) == 0 {
		steps = initialSteps()
	}
	return &stepTracker{steps: append([]Step(nil), steps...)}
}

func (t *stepTracker) set(name StepName, state StepState) {
	for i := range t.steps {
		if t.steps[i].Name == name {
			t.steps[i].State = state
			return
		}
	}
}

func (t *stepTracker) list() []Step { return append([]Step(nil), t.steps...) }

// finish settles steps that never reported a final state: steps not reached
// are skipped; a step still running failed (or, on success, was skipped).
func (t *stepTracker) finish(success bool) []Step {
	for i := range t.steps {
		switch t.steps[i].State {
		case api.StepPending:
			t.steps[i].State = api.StepSkipped
		case api.StepRunning:
			if success {
				t.steps[i].State = api.StepSkipped
			} else {
				t.steps[i].State = api.StepFailed
			}
		}
	}
	return t.list()
}

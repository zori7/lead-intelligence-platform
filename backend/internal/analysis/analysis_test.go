package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apitest"
	"leadintel/backend/internal/httpx"
	"leadintel/backend/internal/lead"
	"leadintel/backend/internal/llmsettings"
)

// ---- Fakes ------------------------------------------------------------------------

func configured() llmsettings.Settings {
	m := "base"
	return llmsettings.Settings{Default: llmsettings.Setting{Model: &m}}
}

type fakeSettings struct{ s llmsettings.Settings }

func (f fakeSettings) Get(context.Context) (llmsettings.Settings, error) { return f.s, nil }

// memStore is an in-memory JobStore + Repository with the same conditional
// transition rules as the SQL store.
type memStore struct {
	mu       sync.Mutex
	jobs     map[uuid.UUID]*Job
	attempts int
}

func newMemStore(jobs ...Job) *memStore {
	s := &memStore{jobs: map[uuid.UUID]*Job{}}
	for i := range jobs {
		j := jobs[i]
		s.jobs[j.ID] = &j
	}
	return s
}

func (s *memStore) ClaimNext(_ context.Context, models llmsettings.Resolved) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.Status == api.JobQueued {
			j.Status, j.ModelConfig = api.JobRunning, models
			return *j, true, nil
		}
	}
	return Job{}, false, nil
}

func (s *memStore) AddDispatchAttempt(context.Context, uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	return nil
}

func (s *memStore) UpdateProgress(_ context.Context, id uuid.UUID, steps []Step, p int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j.Status != api.JobRunning {
		return false, nil
	}
	j.Steps, j.Progress = steps, p
	return true, nil
}

func (s *memStore) Complete(_ context.Context, id uuid.UUID, steps []Step, r api.AnalysisResult) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j.Status != api.JobRunning {
		return Job{}, false, nil
	}
	body, _ := json.Marshal(r)
	j.Status, j.Steps, j.Progress, j.Result = api.JobSucceeded, steps, 100, body
	return *j, true, nil
}

func (s *memStore) Fail(_ context.Context, id uuid.UUID, steps []Step, e JobError) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j.Status != api.JobRunning && j.Status != api.JobQueued {
		return Job{}, false, nil
	}
	j.Status, j.Steps, j.Error = api.JobFailed, steps, &e
	return *j, true, nil
}

func (s *memStore) get(id uuid.UUID) Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.jobs[id]
}

// scriptAnalyzer replays a script per dispatch attempt.
type scriptAnalyzer struct {
	mu      sync.Mutex
	scripts []func(ctx context.Context, handle func(Event) error) error
	calls   int
	req     Request
}

func (a *scriptAnalyzer) Analyze(ctx context.Context, req Request, handle func(Event) error) error {
	a.mu.Lock()
	i := min(a.calls, len(a.scripts)-1)
	a.calls++
	a.req = req
	a.mu.Unlock()
	return a.scripts[i](ctx, handle)
}

func progress(step StepName, state StepState, pct int) Event {
	return Event{Progress: &Progress{Step: step, State: state, Percent: pct}}
}

func sampleResult() *api.AnalysisResult {
	r := &api.AnalysisResult{
		Summary: "Good lead.",
		Company: api.CompanyInsights{Name: "Acme", Industry: "SaaS", SizeEstimate: "51-200", BusinessModel: "SaaS",
			Description: "Software.", KeyNeeds: []string{"Growth"}, Confidence: 0.8},
		Fit:             nullable.NewNullableWithValue(api.FitAnalysis{Score: 80, Rationale: "Fits.", MatchedCriteria: []string{"B2B"}, Gaps: []string{}}),
		Intent:          nullable.NewNullNullable[api.IntentAnalysis](),
		Score:           api.LeadScore{Value: 80, Tier: api.TierHot, Confidence: 0.48},
		Recommendations: []api.Recommendation{{Action: "Call", Rationale: "Now", Priority: api.PriorityHigh}},
		Meta: api.ResultMeta{
			NodeModels: map[string]string{"extract_company": "base"}, Partial: true, Warnings: []string{"intent unavailable"},
			Website: api.WebsiteFetch{Status: api.WebsiteNotProvided, FinalUrl: nullable.NewNullNullable[string]()},
		},
	}
	r.Score.Breakdown.Fit = nullable.NewNullableWithValue(80)
	r.Score.Breakdown.Intent = nullable.NewNullNullable[int]()
	return r
}

func queuedJob() Job {
	return Job{ID: uuid.New(), LeadID: uuid.New(), Status: api.JobQueued, Steps: initialSteps(),
		Input: LeadInput{CompanyName: "Acme", Industry: "SaaS"}, CreatedAt: time.Now().UTC()}
}

// ---- Worker ---------------------------------------------------------------------------

func runOne(t *testing.T, store *memStore, ai Analyzer, cfg WorkerConfig) {
	t.Helper()
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	cfg.Backoff = func(int) time.Duration { return time.Millisecond }
	w := NewWorker(store, fakeSettings{configured()}, ai, NewBroker(), cfg)
	if !w.processNext(context.Background()) {
		t.Fatal("no job was claimed")
	}
}

func stepStates(steps []Step) map[StepName]StepState {
	out := map[StepName]StepState{}
	for _, s := range steps {
		out[s.Name] = s.State
	}
	return out
}

func TestWorker(t *testing.T) {
	unavailable := func(context.Context, func(Event) error) error {
		return errors.Join(ErrAIUnavailable, errors.New("connection refused"))
	}
	succeed := func(_ context.Context, handle func(Event) error) error {
		for _, ev := range []Event{
			progress(api.StepValidateInput, api.StepRunning, 0),
			progress(api.StepValidateInput, api.StepDone, 12),
			{Completed: sampleResult()},
		} {
			if err := handle(ev); err != nil {
				return err
			}
		}
		return nil
	}

	tests := []struct {
		name         string
		scripts      []func(context.Context, func(Event) error) error
		timeout      time.Duration
		wantStatus   api.JobStatus
		wantCode     string
		wantCalls    int
		wantRetry    bool
		wantStepDone StepName
	}{
		{
			name:       "success",
			scripts:    []func(context.Context, func(Event) error) error{succeed},
			wantStatus: api.JobSucceeded, wantCalls: 1, wantStepDone: api.StepValidateInput,
		},
		{
			name: "domain failure",
			scripts: []func(context.Context, func(Event) error) error{func(_ context.Context, h func(Event) error) error {
				_ = h(progress(api.StepValidateInput, api.StepFailed, 12))
				return h(Event{Failed: &Failure{Code: "insufficient_data", Message: "Add details.", Retryable: false}})
			}},
			wantStatus: api.JobFailed, wantCode: "insufficient_data", wantCalls: 1,
		},
		{
			name:       "unavailable then success is retried",
			scripts:    []func(context.Context, func(Event) error) error{unavailable, succeed},
			wantStatus: api.JobSucceeded, wantCalls: 2,
		},
		{
			name:       "unavailable on every attempt",
			scripts:    []func(context.Context, func(Event) error) error{unavailable},
			wantStatus: api.JobFailed, wantCode: CodeAIUnavailable, wantCalls: 3, wantRetry: true,
		},
		{
			name: "broken after first event is not retried",
			scripts: []func(context.Context, func(Event) error) error{func(_ context.Context, h func(Event) error) error {
				_ = h(progress(api.StepValidateInput, api.StepRunning, 0))
				return errors.Join(ErrAIUnavailable, errors.New("stream reset"))
			}},
			wantStatus: api.JobFailed, wantCode: CodeAIUnavailable, wantCalls: 1, wantRetry: true,
		},
		{
			name: "stream ends without terminal event",
			scripts: []func(context.Context, func(Event) error) error{func(_ context.Context, h func(Event) error) error {
				return h(progress(api.StepValidateInput, api.StepDone, 12))
			}},
			wantStatus: api.JobFailed, wantCode: CodeInternal, wantCalls: 1, wantRetry: true,
		},
		{
			name: "deadline",
			scripts: []func(context.Context, func(Event) error) error{func(ctx context.Context, h func(Event) error) error {
				_ = h(progress(api.StepExtractCompany, api.StepRunning, 25))
				<-ctx.Done()
				return ctx.Err()
			}},
			timeout:    50 * time.Millisecond,
			wantStatus: api.JobFailed, wantCode: CodeTimeout, wantCalls: 1, wantRetry: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := queuedJob()
			store := newMemStore(job)
			ai := &scriptAnalyzer{scripts: tt.scripts}
			runOne(t, store, ai, WorkerConfig{DispatchAttempts: 3, Timeout: tt.timeout})

			got := store.get(job.ID)
			if got.Status != tt.wantStatus {
				t.Fatalf("status %s, want %s (error %+v)", got.Status, tt.wantStatus, got.Error)
			}
			if ai.calls != tt.wantCalls || store.attempts != tt.wantCalls {
				t.Fatalf("calls %d / attempts %d, want %d", ai.calls, store.attempts, tt.wantCalls)
			}
			if tt.wantCode != "" && (got.Error == nil || got.Error.Code != tt.wantCode || got.Error.Retryable != tt.wantRetry) {
				t.Fatalf("error %+v, want code %s retryable %v", got.Error, tt.wantCode, tt.wantRetry)
			}
			// Every step ends in a final state.
			for name, state := range stepStates(got.Steps) {
				if state == api.StepPending || state == api.StepRunning {
					t.Fatalf("step %s left %s", name, state)
				}
			}
			if tt.wantStepDone != "" && stepStates(got.Steps)[tt.wantStepDone] != api.StepDone {
				t.Fatalf("step %s not done: %v", tt.wantStepDone, got.Steps)
			}
			if ai.req.Lead.CompanyName != "Acme" || len(ai.req.Models) != len(llmsettings.Nodes) {
				t.Fatalf("request not built from snapshot and settings: %+v", ai.req)
			}
		})
	}
}

func TestWorkerNotConfiguredClaimsNothing(t *testing.T) {
	store := newMemStore(queuedJob())
	w := NewWorker(store, fakeSettings{}, &scriptAnalyzer{}, NewBroker(), WorkerConfig{})
	if w.processNext(context.Background()) {
		t.Fatal("claimed a job while AI is not configured")
	}
}

func TestWorkerShutdownMarksInterrupted(t *testing.T) {
	job := queuedJob()
	store := newMemStore(job)
	started := make(chan struct{})
	ai := &scriptAnalyzer{scripts: []func(context.Context, func(Event) error) error{
		func(ctx context.Context, h func(Event) error) error {
			_ = h(progress(api.StepValidateInput, api.StepRunning, 0))
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}}
	w := NewWorker(store, fakeSettings{configured()}, ai, NewBroker(), WorkerConfig{Timeout: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	w.Wake()
	<-started
	cancel()
	<-done
	if got := store.get(job.ID); got.Error == nil || got.Error.Code != CodeInterrupted {
		t.Fatalf("got %+v, want interrupted", got.Error)
	}
}

// ---- Handlers and contract ----------------------------------------------------------

type fakeLeads struct{ l lead.Lead }

func (f fakeLeads) Get(_ context.Context, id uuid.UUID) (lead.Lead, error) {
	if id != f.l.ID {
		return lead.Lead{}, lead.ErrNotFound
	}
	return f.l, nil
}

type fakeJobs struct {
	job     Job
	created bool
	input   LeadInput
}

func (f *fakeJobs) Create(_ context.Context, leadID uuid.UUID, in LeadInput) (Job, bool, error) {
	f.input = in
	f.job.LeadID = leadID
	return f.job, f.created, nil
}

func (f *fakeJobs) Get(_ context.Context, id uuid.UUID) (Job, error) {
	if id != f.job.ID {
		return Job{}, ErrNotFound
	}
	return f.job, nil
}

func (f *fakeJobs) ListByLead(context.Context, uuid.UUID, int) ([]Job, error) {
	return []Job{f.job}, nil
}

type countWaker struct{ n int }

func (c *countWaker) Wake() { c.n++ }

func newRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		api.HandlerWithOptions(h, api.ChiServerOptions{BaseRouter: r, ErrorHandlerFunc: httpx.ParamError})
	})
	return r
}

func sampleLead() lead.Lead {
	email, site := "ann@acme.example", "https://acme.example"
	now := time.Now().UTC()
	return lead.Lead{ID: uuid.New(), CompanyName: "Acme", Website: &site, ContactEmail: &email,
		Status: api.StatusNew, CreatedAt: now, UpdatedAt: now}
}

func succeededJob(leadID uuid.UUID) Job {
	j := queuedJob()
	j.LeadID, j.Status, j.Progress = leadID, api.JobSucceeded, 100
	j.Result, _ = json.Marshal(sampleResult())
	now := time.Now().UTC()
	j.StartedAt, j.FinishedAt = &now, &now
	for i := range j.Steps {
		j.Steps[i].State = api.StepDone
	}
	return j
}

func TestTrigger(t *testing.T) {
	l := sampleLead()
	tests := []struct {
		name       string
		leadID     uuid.UUID
		settings   llmsettings.Settings
		created    bool
		wantStatus int
		wantWake   int
	}{
		{"queued", l.ID, configured(), true, http.StatusAccepted, 1},
		{"already active", l.ID, configured(), false, http.StatusOK, 0},
		{"not configured", l.ID, llmsettings.Settings{}, true, http.StatusConflict, 0},
		{"unknown lead", uuid.New(), configured(), true, http.StatusNotFound, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := &fakeJobs{job: queuedJob(), created: tt.created}
			waker := &countWaker{}
			h := NewHandler(jobs, fakeLeads{l}, fakeSettings{tt.settings}, waker, NewBroker())
			rec := httptest.NewRecorder()
			newRouter(h).ServeHTTP(rec, httptest.NewRequest("POST", "/api/leads/"+tt.leadID.String()+"/analyses", nil))

			if rec.Code != tt.wantStatus || waker.n != tt.wantWake {
				t.Fatalf("got %d (wakes %d), want %d (wakes %d): %s", rec.Code, waker.n, tt.wantStatus, tt.wantWake, rec.Body)
			}
			apitest.CheckResponse(t, "POST", "/leads/{id}/analyses", rec)
			if tt.wantStatus == http.StatusAccepted && jobs.input.Website != "https://acme.example" {
				t.Fatalf("snapshot %+v", jobs.input)
			}
		})
	}
}

func TestSnapshotOmitsContactEmail(t *testing.T) {
	raw, _ := json.Marshal(snapshot(sampleLead()))
	if json.Valid(raw) && containsKey(raw, "contact_email") {
		t.Fatalf("snapshot leaks contact_email: %s", raw)
	}
}

func containsKey(raw []byte, key string) bool {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	_, ok := m[key]
	return ok
}

func TestResponsesMatchSpec(t *testing.T) {
	l := sampleLead()
	failed := queuedJob()
	failed.Status, failed.Error = api.JobFailed, &JobError{Code: "provider_error", Message: "Retry.", Retryable: true}

	for name, job := range map[string]Job{"queued": queuedJob(), "succeeded": succeededJob(l.ID), "failed": failed} {
		t.Run(name, func(t *testing.T) {
			h := NewHandler(&fakeJobs{job: job}, fakeLeads{l}, fakeSettings{configured()}, &countWaker{}, NewBroker())
			for _, tc := range []struct{ method, url, specPath string }{
				{"GET", "/api/analyses/" + job.ID.String(), "/analyses/{id}"},
				{"GET", "/api/analyses/" + uuid.NewString(), "/analyses/{id}"},
				{"GET", "/api/leads/" + l.ID.String() + "/analyses", "/leads/{id}/analyses"},
				{"GET", "/api/leads/" + l.ID.String() + "/analyses?limit=99", "/leads/{id}/analyses"},
				{"GET", "/api/leads/" + uuid.NewString() + "/analyses", "/leads/{id}/analyses"},
			} {
				rec := httptest.NewRecorder()
				newRouter(h).ServeHTTP(rec, httptest.NewRequest(tc.method, tc.url, nil))
				apitest.CheckResponse(t, tc.method, tc.specPath, rec)
			}
		})
	}
}

func TestLeadDetailWithLatestAnalysisMatchesSpec(t *testing.T) {
	l := sampleLead()
	job := succeededJob(l.ID)
	h := lead.NewHandler(singleLead{l}, func(context.Context, uuid.UUID) (any, error) { return &job, nil })
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		api.HandlerWithOptions(h, api.ChiServerOptions{BaseRouter: r, ErrorHandlerFunc: httpx.ParamError})
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/leads/"+l.ID.String(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	apitest.CheckResponse(t, "GET", "/leads/{id}", rec)
}

type singleLead struct{ l lead.Lead }

func (s singleLead) List(context.Context, lead.ListParams) ([]lead.Lead, int, error) {
	return []lead.Lead{s.l}, 1, nil
}
func (s singleLead) Get(ctx context.Context, id uuid.UUID) (lead.Lead, error) {
	return fakeLeads(s).Get(ctx, id)
}
func (s singleLead) Create(context.Context, lead.Fields) (lead.Lead, error) { return s.l, nil }
func (s singleLead) Update(context.Context, uuid.UUID, []lead.Change) (lead.Lead, error) {
	return s.l, nil
}
func (s singleLead) Delete(context.Context, uuid.UUID) error { return nil }

package analysis

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/db"
	"leadintel/backend/internal/lead"
	"leadintel/backend/internal/llmsettings"
	"leadintel/backend/migrations"
)

// newTestStore connects to TEST_DATABASE_URL, applies migrations and empties
// the tables. Tests are skipped when the variable is not set.
func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping store integration tests")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = sqlDB.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE leads, analysis_jobs CASCADE"); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool), pool
}

func createLead(t *testing.T, pool *pgxpool.Pool) lead.Lead {
	t.Helper()
	l, err := lead.NewStore(pool).Create(context.Background(), lead.Fields{CompanyName: "Acme", Status: api.StatusNew})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func models() llmsettings.Resolved {
	r, _ := llmsettings.Resolve(configured())
	return r
}

func TestStoreOneActiveJobPerLead(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	l := createLead(t, pool)

	// Concurrent triggers converge on one job.
	var wg sync.WaitGroup
	ids := make(chan string, 10)
	created := make(chan bool, 10)
	for range 10 {
		wg.Go(func() {
			job, c, err := s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme"})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- job.ID.String()
			created <- c
		})
	}
	wg.Wait()
	close(ids)
	close(created)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("got different jobs %s and %s", first, id)
		}
	}
	n := 0
	for c := range created {
		if c {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d jobs created, want 1", n)
	}

	// Once the job is finished, a new one can be queued.
	job, ok, err := s.ClaimNext(ctx, models())
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if _, _, err := s.Fail(ctx, job.ID, job.Steps, JobError{Code: "internal", Message: "x", Retryable: true}); err != nil {
		t.Fatal(err)
	}
	if _, c, err := s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme"}); err != nil || !c {
		t.Fatalf("second job: created=%v err=%v", c, err)
	}
}

func TestStoreCreateForMissingLead(t *testing.T) {
	s, pool := newTestStore(t)
	l := createLead(t, pool)
	if err := lead.NewStore(pool).Delete(context.Background(), l.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create(context.Background(), l.ID, LeadInput{}); !errors.Is(err, lead.ErrNotFound) {
		t.Fatalf("got %v, want lead.ErrNotFound", err)
	}
}

func TestStoreLifecycle(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	l := createLead(t, pool)

	queued, _, err := s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme", Website: "https://acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != api.JobQueued || len(queued.Steps) != len(Steps) || queued.Result != nil || queued.Error != nil {
		t.Fatalf("queued job: %+v", queued)
	}

	job, ok, err := s.ClaimNext(ctx, models())
	if err != nil || !ok || job.ID != queued.ID || job.Status != api.JobRunning || job.StartedAt == nil {
		t.Fatalf("claim: %+v %v %v", job, ok, err)
	}
	if job.ModelConfig[api.NodeAnalyzeFit].Model != "base" || job.Input.Website != "https://acme.example" {
		t.Fatalf("claimed job lost config or snapshot: %+v", job)
	}
	if _, ok, _ := s.ClaimNext(ctx, models()); ok {
		t.Fatal("claimed the same job twice")
	}
	if running, err := s.AddDispatchAttempt(ctx, job.ID); err != nil || !running {
		t.Fatalf("dispatch attempt: %v %v", running, err)
	}

	steps := job.Steps
	steps[0].State = api.StepDone
	if ok, err := s.UpdateProgress(ctx, job.ID, steps, 12); err != nil || !ok {
		t.Fatalf("progress: %v %v", ok, err)
	}
	done, ok, err := s.Complete(ctx, job.ID, steps, *sampleResult())
	if err != nil || !ok {
		t.Fatalf("complete: %v %v", ok, err)
	}
	if done.Status != api.JobSucceeded || done.Result == nil || done.LeadID != l.ID {
		t.Fatalf("returned job: %+v", done)
	}

	got, err := s.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != api.JobSucceeded || got.Progress != 100 || got.FinishedAt == nil || got.Result == nil || got.DispatchAttempts != 1 {
		t.Fatalf("completed job: %+v", got)
	}
	updated, err := lead.NewStore(pool).Get(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AIScore == nil || *updated.AIScore != 80 || updated.AITier == nil || *updated.AITier != api.TierHot || updated.LastAnalyzedAt == nil {
		t.Fatalf("lead score not updated: %+v", updated)
	}

	// Terminal states are final: first writer wins.
	if _, ok, _ := s.Fail(ctx, job.ID, steps, JobError{Code: "timeout"}); ok {
		t.Fatal("failed a succeeded job")
	}
	if ok, _ := s.UpdateProgress(ctx, job.ID, steps, 50); ok {
		t.Fatal("updated progress of a finished job")
	}
	if _, ok, _ := s.Complete(ctx, job.ID, steps, *sampleResult()); ok {
		t.Fatal("completed a job twice")
	}

	latest, err := s.Latest(ctx, l.ID)
	if err != nil || latest == nil || latest.ID != job.ID {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	if _, err := s.Get(ctx, l.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown job: %v", err)
	}
}

func TestStoreFailRecordsError(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	l := createLead(t, pool)
	queued, _, _ := s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme"})
	got, ok, err := s.Fail(ctx, queued.ID, queued.Steps, JobError{Code: "ai_unavailable", Message: "Retry.", Retryable: true})
	if err != nil || !ok {
		t.Fatalf("fail queued: %v %v", ok, err)
	}
	if got.Status != api.JobFailed || got.Error == nil || got.Error.Code != "ai_unavailable" || !got.Error.Retryable {
		t.Fatalf("failed job: %+v", got)
	}
}

func TestStoreCancel(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()

	t.Run("running job: cancel wins, later writers are no-ops", func(t *testing.T) {
		l := createLead(t, pool)
		queued, _, _ := s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme"})
		job, _, _ := s.ClaimNext(ctx, models())
		steps := job.Steps
		steps[0].State, steps[1].State = api.StepDone, api.StepRunning
		if ok, err := s.UpdateProgress(ctx, job.ID, steps, 12); err != nil || !ok {
			t.Fatalf("progress: %v %v", ok, err)
		}

		got, err := s.Cancel(ctx, queued.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != api.JobCancelled || got.FinishedAt == nil || got.Error != nil {
			t.Fatalf("cancelled job: %+v", got)
		}
		states := stepStates(got.Steps)
		if states[api.StepValidateInput] != api.StepDone || states[api.StepFetchWebsite] != api.StepSkipped ||
			states[api.StepAggregate] != api.StepSkipped || got.Steps[0].Name != Steps[0] {
			t.Fatalf("steps not settled in order: %v", got.Steps)
		}

		// The worker's writes after the cancel all lose.
		if running, _ := s.AddDispatchAttempt(ctx, job.ID); running {
			t.Fatal("dispatch counted for a cancelled job")
		}
		if ok, _ := s.UpdateProgress(ctx, job.ID, steps, 50); ok {
			t.Fatal("progress written to a cancelled job")
		}
		if _, ok, _ := s.Complete(ctx, job.ID, steps, *sampleResult()); ok {
			t.Fatal("completed a cancelled job")
		}
		if _, ok, _ := s.Fail(ctx, job.ID, steps, JobError{Code: CodeTimeout}); ok {
			t.Fatal("failed a cancelled job")
		}
		if lead, _ := lead.NewStore(pool).Get(ctx, l.ID); lead.AIScore != nil {
			t.Fatal("cancelled job scored the lead")
		}
		if _, err := s.Cancel(ctx, job.ID); !errors.Is(err, ErrNotCancellable) {
			t.Fatalf("second cancel: %v, want ErrNotCancellable", err)
		}
	})

	t.Run("complete wins: cancel is rejected", func(t *testing.T) {
		l := createLead(t, pool)
		_, _, _ = s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme"})
		job, _, _ := s.ClaimNext(ctx, models())
		if _, ok, err := s.Complete(ctx, job.ID, job.Steps, *sampleResult()); err != nil || !ok {
			t.Fatalf("complete: %v %v", ok, err)
		}
		if _, err := s.Cancel(ctx, job.ID); !errors.Is(err, ErrNotCancellable) {
			t.Fatalf("cancel after complete: %v, want ErrNotCancellable", err)
		}
		if got, _ := s.Get(ctx, job.ID); got.Status != api.JobSucceeded {
			t.Fatalf("status %s, want succeeded", got.Status)
		}
	})

	t.Run("unknown job", func(t *testing.T) {
		if _, err := s.Cancel(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("cancel active by lead", func(t *testing.T) {
		l := createLead(t, pool)
		if _, ok, err := s.CancelActive(ctx, l.ID); err != nil || ok {
			t.Fatalf("no active job: %v %v", ok, err)
		}
		queued, _, _ := s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme"})
		got, ok, err := s.CancelActive(ctx, l.ID)
		if err != nil || !ok || got.ID != queued.ID || got.Status != api.JobCancelled {
			t.Fatalf("cancel active: %+v %v %v", got, ok, err)
		}
		// The lead can be analyzed again right away.
		if _, created, err := s.Create(ctx, l.ID, LeadInput{CompanyName: "Acme"}); err != nil || !created {
			t.Fatalf("new job after cancel: %v %v", created, err)
		}
	})
}

func TestStoreFailInterrupted(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	running, _, _ := s.Create(ctx, createLead(t, pool).ID, LeadInput{CompanyName: "Acme"})
	job, _, err := s.ClaimNext(ctx, models())
	if err != nil {
		t.Fatal(err)
	}
	steps := job.Steps
	steps[0].State, steps[1].State = api.StepDone, api.StepRunning
	if _, err := s.UpdateProgress(ctx, job.ID, steps, 12); err != nil {
		t.Fatal(err)
	}
	queued, _, _ := s.Create(ctx, createLead(t, pool).ID, LeadInput{CompanyName: "Beta"})

	n, err := s.FailInterrupted(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recovered %d jobs (%v), want 1", n, err)
	}
	got, _ := s.Get(ctx, running.ID)
	if got.Status != api.JobFailed || got.Error == nil || got.Error.Code != CodeInterrupted || !got.Error.Retryable || got.FinishedAt == nil {
		t.Fatalf("interrupted job: %+v %+v", got, got.Error)
	}
	states := stepStates(got.Steps)
	if states[api.StepValidateInput] != api.StepDone || states[api.StepFetchWebsite] != api.StepFailed ||
		states[api.StepAggregate] != api.StepSkipped {
		t.Fatalf("steps not settled: %v", got.Steps)
	}
	if q, _ := s.Get(ctx, queued.ID); q.Status != api.JobQueued {
		t.Fatalf("queued job changed to %s", q.Status)
	}
}

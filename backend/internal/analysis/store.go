package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/lead"
	"leadintel/backend/internal/llmsettings"
)

const jobColumns = `id, lead_id, status, steps, progress, input_snapshot, result,
	error_code, error_message, retryable, dispatch_attempts, model_config,
	created_at, started_at, finished_at`

// Store persists analysis jobs. Every state transition is a conditional
// UPDATE on the expected status; the first writer wins (SPEC.md §6.4).
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func initialSteps() []Step {
	steps := make([]Step, len(Steps))
	for i, n := range Steps {
		steps[i] = Step{Name: n, State: api.StepPending}
	}
	return steps
}

// Create queues a job for the lead. When the lead already has an active job,
// that job is returned with created=false: "one active analysis per lead" is
// the idempotency rule, enforced by a partial unique index.
func (s *Store) Create(ctx context.Context, leadID uuid.UUID, input LeadInput) (job Job, created bool, err error) {
	snapshot, err := json.Marshal(input)
	if err != nil {
		return Job{}, false, fmt.Errorf("encode input snapshot: %w", err)
	}
	steps, err := json.Marshal(initialSteps())
	if err != nil {
		return Job{}, false, fmt.Errorf("encode steps: %w", err)
	}
	// Two attempts: the active job found by the unique violation may finish
	// before it is read back, in which case a new insert succeeds.
	for range 2 {
		row := s.db.QueryRow(ctx, `
			INSERT INTO analysis_jobs (lead_id, input_snapshot, steps)
			VALUES ($1, $2, $3)
			RETURNING `+jobColumns, leadID, snapshot, steps)
		job, err = scanJob(row)
		if err == nil {
			return job, true, nil
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			return Job{}, false, fmt.Errorf("create analysis: %w", err)
		}
		switch pgErr.Code {
		case "23503": // foreign_key_violation: the lead was deleted
			return Job{}, false, lead.ErrNotFound
		case "23505": // unique_violation: an analysis is already active
			job, err = s.active(ctx, leadID)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return job, false, err
		default:
			return Job{}, false, fmt.Errorf("create analysis: %w", err)
		}
	}
	return Job{}, false, fmt.Errorf("create analysis: active job changed concurrently")
}

func (s *Store) active(ctx context.Context, leadID uuid.UUID) (Job, error) {
	return getOne(s.db.QueryRow(ctx, `SELECT `+jobColumns+` FROM analysis_jobs
		WHERE lead_id = $1 AND status IN ('queued', 'running')`, leadID))
}

// ClaimNext moves the oldest queued job to running and records the model
// config it will run with. ok is false when nothing is queued.
func (s *Store) ClaimNext(ctx context.Context, models llmsettings.Resolved) (job Job, ok bool, err error) {
	cfg, err := json.Marshal(models)
	if err != nil {
		return Job{}, false, fmt.Errorf("encode model config: %w", err)
	}
	job, err = scanJob(s.db.QueryRow(ctx, `
		UPDATE analysis_jobs
		SET status = 'running', started_at = now(), updated_at = now(), model_config = $1
		WHERE id = (
			SELECT id FROM analysis_jobs WHERE status = 'queued'
			ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		RETURNING `+jobColumns, cfg))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("claim analysis: %w", err)
	}
	return job, true, nil
}

// AddDispatchAttempt counts one dispatch of a running job to the AI service.
func (s *Store) AddDispatchAttempt(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE analysis_jobs
		SET dispatch_attempts = dispatch_attempts + 1, updated_at = now()
		WHERE id = $1 AND status = 'running'`, id)
	if err != nil {
		return fmt.Errorf("count dispatch attempt: %w", err)
	}
	return nil
}

// UpdateProgress stores the step list of a running job. It returns false when
// the job is no longer running (e.g. cancelled or timed out meanwhile).
func (s *Store) UpdateProgress(ctx context.Context, id uuid.UUID, steps []Step, progress int) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE analysis_jobs
		SET steps = $2, progress = $3, updated_at = now()
		WHERE id = $1 AND status = 'running'`, id, steps, progress)
	if err != nil {
		return false, fmt.Errorf("update analysis progress: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Complete marks a running job succeeded and copies the score onto the lead,
// in one transaction. It returns the updated job, or ok=false when the job was
// no longer running.
func (s *Store) Complete(ctx context.Context, id uuid.UUID, steps []Step, result api.AnalysisResult) (job Job, ok bool, err error) {
	body, err := json.Marshal(result)
	if err != nil {
		return Job{}, false, fmt.Errorf("encode result: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	job, err = scanJob(tx.QueryRow(ctx, `UPDATE analysis_jobs
		SET status = 'succeeded', steps = $2, progress = 100, result = $3,
		    finished_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'running'
		RETURNING `+jobColumns, id, steps, body))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("complete analysis: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE leads
		SET ai_score = $2, ai_tier = $3, last_analyzed_at = now(), updated_at = now()
		WHERE id = $1`, job.LeadID, result.Score.Value, result.Score.Tier); err != nil {
		return Job{}, false, fmt.Errorf("update lead score: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("commit: %w", err)
	}
	return job, true, nil
}

// Fail marks a queued or running job failed. It returns the updated job, or
// ok=false when the job had already reached a terminal state.
func (s *Store) Fail(ctx context.Context, id uuid.UUID, steps []Step, e JobError) (job Job, ok bool, err error) {
	job, err = scanJob(s.db.QueryRow(ctx, `UPDATE analysis_jobs
		SET status = 'failed', steps = $2, error_code = $3, error_message = $4, retryable = $5,
		    finished_at = now(), updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'running')
		RETURNING `+jobColumns,
		id, steps, e.Code, e.Message, e.Retryable))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("fail analysis: %w", err)
	}
	return job, true, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (Job, error) {
	return getOne(s.db.QueryRow(ctx, `SELECT `+jobColumns+` FROM analysis_jobs WHERE id = $1`, id))
}

// ListByLead returns the lead's jobs, newest first.
func (s *Store) ListByLead(ctx context.Context, leadID uuid.UUID, limit int) ([]Job, error) {
	rows, err := s.db.Query(ctx, `SELECT `+jobColumns+` FROM analysis_jobs
		WHERE lead_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, leadID, limit)
	if err != nil {
		return nil, fmt.Errorf("list analyses: %w", err)
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan analysis: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list analyses: %w", err)
	}
	return jobs, nil
}

// Latest returns the lead's most recent job, or nil when there is none.
func (s *Store) Latest(ctx context.Context, leadID uuid.UUID) (*Job, error) {
	jobs, err := s.ListByLead(ctx, leadID, 1)
	if err != nil || len(jobs) == 0 {
		return nil, err
	}
	return &jobs[0], nil
}

func getOne(row pgx.Row) (Job, error) {
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get analysis: %w", err)
	}
	return j, nil
}

func scanJob(row pgx.Row) (Job, error) {
	var (
		j                Job
		snapshot, result []byte
		code, message    *string
		retryable        *bool
		progress         int16
		modelConfig      []byte
	)
	err := row.Scan(&j.ID, &j.LeadID, &j.Status, &j.Steps, &progress, &snapshot, &result,
		&code, &message, &retryable, &j.DispatchAttempts, &modelConfig,
		&j.CreatedAt, &j.StartedAt, &j.FinishedAt)
	if err != nil {
		return Job{}, err
	}
	j.Progress = int(progress)
	if len(result) > 0 {
		j.Result = json.RawMessage(result)
	}
	if code != nil {
		j.Error = &JobError{Code: *code, Message: deref(message), Retryable: retryable != nil && *retryable}
	}
	if err := json.Unmarshal(snapshot, &j.Input); err != nil {
		return Job{}, fmt.Errorf("decode input snapshot: %w", err)
	}
	if len(modelConfig) > 0 {
		if err := json.Unmarshal(modelConfig, &j.ModelConfig); err != nil {
			return Job{}, fmt.Errorf("decode model config: %w", err)
		}
	}
	return j, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

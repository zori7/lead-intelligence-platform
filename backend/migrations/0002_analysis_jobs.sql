-- +goose Up
CREATE TABLE analysis_jobs (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id           uuid        NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    status            text        NOT NULL DEFAULT 'queued'
                                  CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    steps             jsonb       NOT NULL DEFAULT '[]',
    progress          smallint    NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    input_snapshot    jsonb       NOT NULL,
    result            jsonb,
    error_code        text,
    error_message     text,
    retryable         boolean,
    dispatch_attempts int         NOT NULL DEFAULT 0,
    model_config      jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    started_at        timestamptz,
    finished_at       timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- At most ONE active analysis per lead (core dedupe invariant).
CREATE UNIQUE INDEX analysis_jobs_one_active_per_lead
    ON analysis_jobs (lead_id) WHERE status IN ('queued', 'running');

-- Worker claim scan.
CREATE INDEX analysis_jobs_queued ON analysis_jobs (created_at) WHERE status = 'queued';

-- History per lead.
CREATE INDEX analysis_jobs_lead_created ON analysis_jobs (lead_id, created_at DESC);

-- +goose Down
DROP TABLE analysis_jobs;

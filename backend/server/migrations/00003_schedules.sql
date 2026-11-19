-- Schedules: recurring jobs, the replacement for Celery beat (docs/04-DATA-MODEL.md §3.7, docs/05-JOB-LIFECYCLE.md §11).

-- +goose Up
CREATE TABLE schedules (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name                text NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9_.-]{0,99}$'),
    task_type           text NOT NULL CHECK (task_type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,3}$' AND char_length(task_type) <= 100),
    task_version        integer NOT NULL CHECK (task_version >= 1),
    queue               text CHECK (queue ~ '^[a-z][a-z0-9_.-]{0,62}$'),   -- NULL = the task type's queue
    payload             jsonb NOT NULL,
    interval_seconds    integer CHECK (interval_seconds BETWEEN 5 AND 31536000),
    cron                text CHECK (char_length(cron) BETWEEN 1 AND 100),
    timezone            text NOT NULL CHECK (char_length(timezone) BETWEEN 1 AND 64),
    enabled             boolean NOT NULL,
    allow_overlap       boolean NOT NULL,
    max_attempts        integer CHECK (max_attempts BETWEEN 1 AND 10),
    timeout_seconds     integer CHECK (timeout_seconds BETWEEN 1 AND 86400),
    next_run_at         timestamptz NOT NULL,
    last_run_at         timestamptz,
    last_job_id         uuid,   -- no foreign key: retention may delete the job
    last_error_code     text CHECK (last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    last_error_message  text CHECK (char_length(last_error_message) <= 1000),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT schedules_one_timing CHECK ((interval_seconds IS NULL) <> (cron IS NULL)),
    CONSTRAINT schedules_error_all_or_none CHECK ((last_error_code IS NULL) = (last_error_message IS NULL)),
    CONSTRAINT schedules_payload_size CHECK (pg_column_size(payload) <= 70000)
);
CREATE UNIQUE INDEX schedules_tenant_name_key ON schedules (tenant_id, name);
CREATE INDEX schedules_due_idx ON schedules (next_run_at) WHERE enabled;

GRANT SELECT, INSERT, UPDATE, DELETE ON schedules TO taskpilot_app;

-- +goose Down
DROP TABLE schedules;

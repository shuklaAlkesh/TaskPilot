-- Dynamic task registration, named queues, and worker presence (docs/14-DECISIONS/0009).

-- +goose Up
CREATE TABLE task_types (
    task_type                text NOT NULL CHECK (task_type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,3}$' AND char_length(task_type) <= 100),
    task_version             integer NOT NULL CHECK (task_version >= 1),
    queue                    text NOT NULL CHECK (queue ~ '^[a-z][a-z0-9_.-]{0,62}$'),
    title                    text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    description              text NOT NULL CHECK (char_length(description) <= 2000),
    payload_schema           jsonb NOT NULL,
    schema_sha256            bytea NOT NULL CHECK (octet_length(schema_sha256) = 32),
    example                  jsonb,
    default_timeout_seconds  integer NOT NULL CHECK (default_timeout_seconds BETWEEN 1 AND 86400),
    max_timeout_seconds      integer NOT NULL CHECK (max_timeout_seconds BETWEEN 1 AND 86400),
    default_max_attempts     integer NOT NULL CHECK (default_max_attempts BETWEEN 1 AND 10),
    idempotent               boolean NOT NULL,
    side_effects             boolean NOT NULL,
    test_only                boolean NOT NULL DEFAULT false,
    registered_by            text NOT NULL CHECK (char_length(registered_by) BETWEEN 1 AND 128),
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (task_type, task_version),
    CONSTRAINT task_types_timeouts CHECK (default_timeout_seconds <= max_timeout_seconds),
    -- docs/05-JOB-LIFECYCLE.md §7.3: non-idempotent side effects may run at most once.
    CONSTRAINT task_types_side_effects CHECK (NOT side_effects OR idempotent OR default_max_attempts = 1),
    CONSTRAINT task_types_schema_size CHECK (pg_column_size(payload_schema) <= 70000)
);

CREATE TABLE workers (
    worker_id     text PRIMARY KEY CHECK (char_length(worker_id) BETWEEN 1 AND 128),
    hostname      text NOT NULL CHECK (char_length(hostname) <= 255),
    version       text NOT NULL CHECK (char_length(version) <= 64),
    concurrency   integer NOT NULL CHECK (concurrency BETWEEN 1 AND 1024),
    queues        text[] NOT NULL CHECK (cardinality(queues) BETWEEN 1 AND 50),
    task_types    text[] NOT NULL,   -- "task_type@version" this worker can run
    started_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workers_last_seen_idx ON workers (last_seen_at);

ALTER TABLE jobs ADD COLUMN queue text NOT NULL DEFAULT 'default'
    CHECK (queue ~ '^[a-z][a-z0-9_.-]{0,62}$');
ALTER TABLE jobs ALTER COLUMN queue DROP DEFAULT;

-- Allow dotted names like synthetic_data.tasks.generate (2-4 segments) and long-running jobs (24 h).
ALTER TABLE jobs DROP CONSTRAINT jobs_task_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_task_type_check
    CHECK (task_type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,3}$' AND char_length(task_type) <= 100);
ALTER TABLE jobs DROP CONSTRAINT jobs_timeout_seconds_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_timeout_seconds_check CHECK (timeout_seconds BETWEEN 1 AND 86400);

-- Claims now filter by queue first.
DROP INDEX jobs_claim_idx;
CREATE INDEX jobs_claim_idx ON jobs (queue, available_at, id) WHERE status = 'queued';

GRANT SELECT, INSERT, UPDATE, DELETE ON task_types, workers TO taskpilot_app;

-- +goose Down
DROP INDEX jobs_claim_idx;
CREATE INDEX jobs_claim_idx ON jobs (available_at, id) WHERE status = 'queued';
ALTER TABLE jobs DROP CONSTRAINT jobs_timeout_seconds_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_timeout_seconds_check CHECK (timeout_seconds BETWEEN 1 AND 900);
ALTER TABLE jobs DROP CONSTRAINT jobs_task_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_task_type_check CHECK (task_type ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$');
ALTER TABLE jobs DROP COLUMN queue;
DROP TABLE workers;
DROP TABLE task_types;

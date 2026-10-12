-- Schema: docs/04-DATA-MODEL.md §3. State rules: docs/05-JOB-LIFECYCLE.md.

-- +goose Up
CREATE TABLE tenants (
    id           uuid PRIMARY KEY,
    name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    created_at   timestamptz NOT NULL DEFAULT now(),
    disabled_at  timestamptz
);
CREATE UNIQUE INDEX tenants_name_key ON tenants (lower(name));

CREATE TABLE api_keys (
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name          text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    prefix        text NOT NULL UNIQUE CHECK (prefix ~ '^[a-z0-9]{12}$'),
    secret_hash   bytea NOT NULL CHECK (octet_length(secret_hash) = 32),
    scopes        text[] NOT NULL CHECK (scopes <@ ARRAY['jobs:read','jobs:write','jobs:cancel']::text[]
                                         AND cardinality(scopes) > 0),
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz,
    revoked_at    timestamptz,
    last_used_at  timestamptz
);
CREATE INDEX api_keys_tenant_idx ON api_keys (tenant_id);

CREATE TABLE jobs (
    id                      uuid PRIMARY KEY,
    tenant_id               uuid NOT NULL REFERENCES tenants (id),
    task_type               text NOT NULL CHECK (task_type ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$'),
    task_version            integer NOT NULL CHECK (task_version >= 1),
    payload                 jsonb NOT NULL,
    status                  text NOT NULL CHECK (status IN ('queued','running','succeeded','failed','cancelled')),
    attempt_count           integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts            integer NOT NULL CHECK (max_attempts BETWEEN 1 AND 10),
    timeout_seconds         integer NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 900),
    available_at            timestamptz NOT NULL,
    current_attempt_id      uuid,
    lease_expires_at        timestamptz,
    cancel_requested_at     timestamptz,
    result                  jsonb,
    last_error_code         text,
    last_error_message      text CHECK (char_length(last_error_message) <= 1000),
    failure_reason          text CHECK (failure_reason IN ('permanent_error','max_attempts_exceeded')),
    idempotency_key         text CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    request_hash            bytea CHECK (octet_length(request_hash) = 32),
    idempotency_expires_at  timestamptz,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    started_at              timestamptz,
    finished_at             timestamptz,

    CONSTRAINT jobs_running_has_lease CHECK (
        (status = 'running') = (current_attempt_id IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CONSTRAINT jobs_terminal_has_finished CHECK (
        (status IN ('succeeded','failed','cancelled')) = (finished_at IS NOT NULL)),
    CONSTRAINT jobs_failed_has_reason CHECK (
        (status = 'failed') = (failure_reason IS NOT NULL)),
    CONSTRAINT jobs_result_only_on_success CHECK (
        result IS NULL OR status = 'succeeded'),
    CONSTRAINT jobs_attempts_bounded CHECK (attempt_count <= max_attempts),
    CONSTRAINT jobs_idempotency_all_or_none CHECK (
        (idempotency_key IS NULL) = (request_hash IS NULL)
        AND (idempotency_key IS NULL) = (idempotency_expires_at IS NULL)),
    CONSTRAINT jobs_payload_size CHECK (pg_column_size(payload) <= 70000),
    CONSTRAINT jobs_result_size  CHECK (result IS NULL OR pg_column_size(result) <= 70000)
);

CREATE TABLE job_attempts (
    id                 uuid PRIMARY KEY,
    job_id             uuid NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    tenant_id          uuid NOT NULL,
    attempt_no         integer NOT NULL CHECK (attempt_no >= 1),
    worker_id          text NOT NULL CHECK (char_length(worker_id) BETWEEN 1 AND 128),
    status             text NOT NULL CHECK (status IN
                         ('running','succeeded','failed','timed_out','lease_expired','cancelled')),
    started_at         timestamptz NOT NULL DEFAULT now(),
    deadline_at        timestamptz NOT NULL,
    last_heartbeat_at  timestamptz,
    finished_at        timestamptz,
    error_code         text CHECK (error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    error_message      text CHECK (char_length(error_message) <= 1000),
    error_retryable    boolean,
    CONSTRAINT job_attempts_job_attempt_no_key UNIQUE (job_id, attempt_no),
    CONSTRAINT job_attempts_finished CHECK ((status = 'running') = (finished_at IS NULL))
);

ALTER TABLE jobs ADD CONSTRAINT jobs_current_attempt_fk
    FOREIGN KEY (current_attempt_id) REFERENCES job_attempts (id);

CREATE TABLE audit_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id   uuid,
    actor_type  text NOT NULL CHECK (actor_type IN ('api_key','worker','system','operator')),
    actor_id    text NOT NULL,
    action      text NOT NULL,
    job_id      uuid,
    request_id  text,
    metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX jobs_claim_idx         ON jobs (available_at, id) WHERE status = 'queued';
CREATE INDEX jobs_lease_idx         ON jobs (lease_expires_at) WHERE status = 'running';
CREATE INDEX jobs_tenant_list_idx   ON jobs (tenant_id, created_at DESC, id DESC);
CREATE INDEX jobs_tenant_status_idx ON jobs (tenant_id, status, created_at DESC, id DESC);
CREATE UNIQUE INDEX jobs_idempotency_key ON jobs (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
CREATE INDEX jobs_retention_idx     ON jobs (finished_at) WHERE finished_at IS NOT NULL;
CREATE INDEX jobs_idem_expiry_idx   ON jobs (idempotency_expires_at) WHERE idempotency_key IS NOT NULL;
CREATE INDEX audit_events_tenant_idx  ON audit_events (tenant_id, created_at DESC);
CREATE INDEX audit_events_created_idx ON audit_events (created_at);

-- Least-privilege app role (docs/04-DATA-MODEL.md §7). The role itself is created outside migrations.
GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, api_keys, jobs, job_attempts, audit_events TO taskpilot_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO taskpilot_app;
GRANT SELECT ON goose_db_version TO taskpilot_app;

-- +goose Down
DROP TABLE audit_events;
ALTER TABLE jobs DROP CONSTRAINT jobs_current_attempt_fk;
DROP TABLE job_attempts;
DROP TABLE jobs;
DROP TABLE api_keys;
DROP TABLE tenants;

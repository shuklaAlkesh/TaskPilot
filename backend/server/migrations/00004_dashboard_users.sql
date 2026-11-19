-- Email sign-in for the dashboard, application (tenant) membership, and task types owned by an application.
-- API keys stay the credential for application-to-TaskPilot calls; users only sign in to the dashboard.

-- +goose Up
-- Only invited emails can sign in (taskpilot-admin user invite). Emails are stored lower-cased.
CREATE TABLE users (
    id             uuid PRIMARY KEY,
    email          text NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254 AND email = lower(email)),
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_login_at  timestamptz,
    disabled_at    timestamptz
);
CREATE UNIQUE INDEX users_email_key ON users (email);

-- An application is a tenant. Members can see its jobs and manage its API keys from the dashboard.
CREATE TABLE tenant_members (
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);
CREATE INDEX tenant_members_user_idx ON tenant_members (user_id);

-- One-time sign-in codes. Only a SHA-256 hash is stored; a code expires quickly and allows few guesses.
CREATE TABLE login_codes (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash    bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    attempts     integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz
);
CREATE INDEX login_codes_user_idx ON login_codes (user_id, created_at DESC);

-- Dashboard sessions. Only a SHA-256 hash of the bearer token is stored.
CREATE TABLE user_sessions (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz
);
CREATE INDEX user_sessions_user_idx ON user_sessions (user_id);

-- NULL = shared (any application can submit it); otherwise only the owning application sees it.
ALTER TABLE task_types ADD COLUMN tenant_id uuid REFERENCES tenants (id) ON DELETE CASCADE;

-- Dashboard actions are audited as the signed-in user.
ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_actor_type_check
    CHECK (actor_type IN ('api_key','user','worker','system','operator'));

GRANT SELECT, INSERT, UPDATE, DELETE ON users, tenant_members, login_codes, user_sessions TO taskpilot_app;

-- +goose Down
DELETE FROM audit_events WHERE actor_type = 'user';
ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_actor_type_check
    CHECK (actor_type IN ('api_key','worker','system','operator'));
ALTER TABLE task_types DROP COLUMN tenant_id;
DROP TABLE user_sessions;
DROP TABLE login_codes;
DROP TABLE tenant_members;
DROP TABLE users;

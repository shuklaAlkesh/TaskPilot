-- LOCAL DEVELOPMENT ONLY. Runs once when the Compose Postgres volume is created.
-- Production roles are created by the operator (docs/04-DATA-MODEL.md §7).
CREATE ROLE taskpilot_app LOGIN PASSWORD 'change-me-local-only';
ALTER ROLE taskpilot_app SET statement_timeout = '5s';
ALTER ROLE taskpilot_app SET idle_in_transaction_session_timeout = '10s';

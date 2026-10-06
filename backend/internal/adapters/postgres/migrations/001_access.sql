CREATE TABLE user_account (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login text NOT NULL,
    normalized_login text NOT NULL UNIQUE,
    display_name text NOT NULL,
    role text NOT NULL CHECK (role IN ('employee', 'analytics_admin')),
    state text NOT NULL CHECK (state IN ('pending_activation', 'active', 'deactivated')),
    password_hash text NOT NULL,
    temporary_password_expires_at timestamptz,
    password_changed_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE user_session (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES user_account(id),
    token_hash text NOT NULL UNIQUE CHECK (length(token_hash) = 64),
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);

CREATE INDEX user_session_account_active_idx
    ON user_session (account_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE audit_event (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_account_id uuid REFERENCES user_account(id),
    subject_account_id uuid REFERENCES user_account(id),
    event_type text NOT NULL,
    occurred_at timestamptz NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX audit_event_occurred_at_idx ON audit_event (occurred_at DESC);

CREATE FUNCTION reject_audit_event_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit events are immutable';
END;
$$;

CREATE TRIGGER audit_event_immutable
BEFORE UPDATE OR DELETE ON audit_event
FOR EACH ROW EXECUTE FUNCTION reject_audit_event_mutation();

CREATE TABLE login_throttle (
    throttle_key text PRIMARY KEY,
    failure_count integer NOT NULL CHECK (failure_count > 0),
    blocked_until timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE analytical_session (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    status text NOT NULL CHECK (status IN ('queued', 'collecting', 'complete', 'partial', 'failed', 'canceled')),
    created_by uuid NOT NULL REFERENCES user_account(id),
    created_at timestamptz NOT NULL,
    region_names text[] NOT NULL CHECK (cardinality(region_names) > 0),
    residential_complex_names text[] NOT NULL CHECK (cardinality(residential_complex_names) > 0),
    segments text[] NOT NULL CHECK (cardinality(segments) > 0),
    period_from date NOT NULL,
    period_to date NOT NULL CHECK (period_to >= period_from),
    deleted_at timestamptz
);

CREATE INDEX analytical_session_visible_idx
    ON analytical_session (created_at DESC)
    WHERE deleted_at IS NULL;

---- create above / drop below ----

DROP TABLE analytical_session;
DROP TABLE login_throttle;
DROP FUNCTION reject_audit_event_mutation;
DROP TABLE audit_event;
DROP TABLE user_session;
DROP TABLE user_account;

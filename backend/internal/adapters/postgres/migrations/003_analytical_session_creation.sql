ALTER TABLE analytical_session
    ADD COLUMN region_codes text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN residential_complex_ids text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN segment_codes text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN observed_from timestamptz,
    ADD COLUMN observed_to timestamptz;

ALTER TABLE analytical_session
    ALTER COLUMN period_from DROP NOT NULL,
    ALTER COLUMN period_to DROP NOT NULL;

CREATE TABLE eiszhs_current_observation (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    region_code text NOT NULL REFERENCES reference_region(code),
    complex_id text NOT NULL,
    segment_code text NOT NULL CHECK (segment_code IN ('residential', 'non_residential', 'parking')),
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > observed_at),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX eiszhs_current_observation_fresh_idx
    ON eiszhs_current_observation (region_code, complex_id, segment_code, observed_at DESC);

CREATE TABLE analytical_session_observation (
    analytical_session_id uuid NOT NULL REFERENCES analytical_session(id),
    observation_id uuid NOT NULL REFERENCES eiszhs_current_observation(id),
    PRIMARY KEY (analytical_session_id, observation_id)
);

CREATE TABLE analytical_session_idempotency (
    account_id uuid NOT NULL REFERENCES user_account(id),
    endpoint text NOT NULL,
    idempotency_key text NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 128
        AND idempotency_key ~ '^[ -~]+$'
    ),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    analytical_session_id uuid NOT NULL REFERENCES analytical_session(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, endpoint, idempotency_key)
);

CREATE FUNCTION reject_analytical_session_filter_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.region_codes IS DISTINCT FROM OLD.region_codes
        OR NEW.region_names IS DISTINCT FROM OLD.region_names
        OR NEW.residential_complex_ids IS DISTINCT FROM OLD.residential_complex_ids
        OR NEW.residential_complex_names IS DISTINCT FROM OLD.residential_complex_names
        OR NEW.segment_codes IS DISTINCT FROM OLD.segment_codes
        OR NEW.segments IS DISTINCT FROM OLD.segments
        OR NEW.period_from IS DISTINCT FROM OLD.period_from
        OR NEW.period_to IS DISTINCT FROM OLD.period_to
        OR NEW.observed_from IS DISTINCT FROM OLD.observed_from
        OR NEW.observed_to IS DISTINCT FROM OLD.observed_to
    THEN
        RAISE EXCEPTION 'analytical session filters are immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER analytical_session_filters_immutable
BEFORE UPDATE ON analytical_session
FOR EACH ROW EXECUTE FUNCTION reject_analytical_session_filter_mutation();

---- create above / drop below ----

DROP TRIGGER analytical_session_filters_immutable ON analytical_session;
DROP FUNCTION reject_analytical_session_filter_mutation;
DROP TABLE analytical_session_idempotency;
DROP TABLE analytical_session_observation;
DROP TABLE eiszhs_current_observation;
ALTER TABLE analytical_session
    DROP COLUMN observed_to,
    DROP COLUMN observed_from,
    DROP COLUMN segment_codes,
    DROP COLUMN residential_complex_ids,
    DROP COLUMN region_codes;
UPDATE analytical_session
SET period_from = created_at::date, period_to = created_at::date
WHERE period_from IS NULL OR period_to IS NULL;
ALTER TABLE analytical_session
    ALTER COLUMN period_from SET NOT NULL,
    ALTER COLUMN period_to SET NOT NULL;

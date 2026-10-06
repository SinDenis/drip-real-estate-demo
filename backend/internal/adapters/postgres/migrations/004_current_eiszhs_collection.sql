ALTER TABLE analytical_session
    ADD COLUMN residential_complex_region_codes text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN collection_completed_requests integer NOT NULL DEFAULT 0
        CHECK (collection_completed_requests >= 0),
    ADD COLUMN collection_total_requests integer
        CHECK (collection_total_requests IS NULL
            OR collection_total_requests >= collection_completed_requests);

ALTER TABLE eiszhs_current_observation
    ADD COLUMN object_id text,
    ADD COLUMN cumulative_ddu_count bigint CHECK (cumulative_ddu_count >= 0);

CREATE TABLE eiszhs_collection_step_claim (
    analytical_session_id uuid PRIMARY KEY REFERENCES analytical_session(id) ON DELETE CASCADE,
    claim_token uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    step_kind text NOT NULL CHECK (step_kind IN ('complex_membership', 'current_extended')),
    complex_id text NOT NULL,
    page integer,
    object_id text,
    claimed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > claimed_at),
    CHECK (
        (step_kind = 'complex_membership' AND page IS NOT NULL AND page >= 0 AND object_id IS NULL)
        OR
        (step_kind = 'current_extended' AND page IS NULL AND object_id IS NOT NULL)
    )
);

CREATE TABLE eiszhs_raw_artifact (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    analytical_session_id uuid NOT NULL REFERENCES analytical_session(id) ON DELETE CASCADE,
    step_kind text NOT NULL CHECK (step_kind IN ('complex_membership', 'current_extended')),
    complex_id text NOT NULL,
    page integer,
    object_id text,
    object_key text NOT NULL UNIQUE,
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    byte_size bigint NOT NULL CHECK (byte_size >= 0),
    content_type text NOT NULL,
    source_address text NOT NULL CHECK (source_address <> ''),
    observed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (step_kind = 'complex_membership' AND page IS NOT NULL AND page >= 0 AND object_id IS NULL)
        OR
        (step_kind = 'current_extended' AND page IS NULL AND object_id IS NOT NULL)
    )
);

CREATE TABLE eiszhs_collection_membership_checkpoint (
    analytical_session_id uuid NOT NULL REFERENCES analytical_session(id) ON DELETE CASCADE,
    complex_id text NOT NULL,
    page integer NOT NULL CHECK (page >= 0),
    total bigint NOT NULL CHECK (total >= 0),
    page_size integer NOT NULL CHECK (page_size = 50),
    object_ids text[] NOT NULL,
    raw_artifact_id uuid NOT NULL UNIQUE REFERENCES eiszhs_raw_artifact(id),
    completed_at timestamptz NOT NULL,
    PRIMARY KEY (analytical_session_id, complex_id, page)
);

CREATE TABLE eiszhs_collection_object (
    analytical_session_id uuid NOT NULL REFERENCES analytical_session(id) ON DELETE CASCADE,
    complex_id text NOT NULL,
    object_id text NOT NULL,
    discovered_page integer NOT NULL CHECK (discovered_page >= 0),
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    PRIMARY KEY (analytical_session_id, object_id),
    UNIQUE (analytical_session_id, complex_id, discovered_page, ordinal)
);

CREATE TABLE eiszhs_collection_extended_checkpoint (
    analytical_session_id uuid NOT NULL REFERENCES analytical_session(id) ON DELETE CASCADE,
    complex_id text NOT NULL,
    object_id text NOT NULL,
    raw_artifact_id uuid NOT NULL UNIQUE REFERENCES eiszhs_raw_artifact(id),
    completed_at timestamptz NOT NULL,
    PRIMARY KEY (analytical_session_id, object_id),
    FOREIGN KEY (analytical_session_id, object_id)
        REFERENCES eiszhs_collection_object(analytical_session_id, object_id)
);

ALTER TABLE eiszhs_current_observation
    ADD COLUMN raw_artifact_id uuid REFERENCES eiszhs_raw_artifact(id),
    ADD CONSTRAINT eiszhs_current_observation_raw_segment_unique
        UNIQUE (raw_artifact_id, segment_code);

CREATE FUNCTION reject_eiszhs_current_snapshot_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ЕИСЖС current snapshot rows are append-only';
END;
$$;

CREATE TRIGGER eiszhs_raw_artifact_append_only
BEFORE UPDATE OR DELETE ON eiszhs_raw_artifact
FOR EACH ROW EXECUTE FUNCTION reject_eiszhs_current_snapshot_mutation();
CREATE TRIGGER eiszhs_membership_checkpoint_append_only
BEFORE UPDATE OR DELETE ON eiszhs_collection_membership_checkpoint
FOR EACH ROW EXECUTE FUNCTION reject_eiszhs_current_snapshot_mutation();
CREATE TRIGGER eiszhs_extended_checkpoint_append_only
BEFORE UPDATE OR DELETE ON eiszhs_collection_extended_checkpoint
FOR EACH ROW EXECUTE FUNCTION reject_eiszhs_current_snapshot_mutation();
CREATE TRIGGER eiszhs_current_observation_append_only
BEFORE UPDATE OR DELETE ON eiszhs_current_observation
FOR EACH ROW EXECUTE FUNCTION reject_eiszhs_current_snapshot_mutation();

CREATE OR REPLACE FUNCTION reject_analytical_session_filter_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.region_codes IS DISTINCT FROM OLD.region_codes
        OR NEW.region_names IS DISTINCT FROM OLD.region_names
        OR NEW.residential_complex_ids IS DISTINCT FROM OLD.residential_complex_ids
        OR NEW.residential_complex_region_codes IS DISTINCT FROM OLD.residential_complex_region_codes
        OR NEW.residential_complex_names IS DISTINCT FROM OLD.residential_complex_names
        OR NEW.segment_codes IS DISTINCT FROM OLD.segment_codes
        OR NEW.segments IS DISTINCT FROM OLD.segments
        OR NEW.period_from IS DISTINCT FROM OLD.period_from
        OR NEW.period_to IS DISTINCT FROM OLD.period_to
    THEN
        RAISE EXCEPTION 'analytical session filters are immutable';
    END IF;
    RETURN NEW;
END;
$$;

---- create above / drop below ----

CREATE OR REPLACE FUNCTION reject_analytical_session_filter_mutation() RETURNS trigger
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

DROP TRIGGER eiszhs_current_observation_append_only ON eiszhs_current_observation;
DROP TRIGGER eiszhs_extended_checkpoint_append_only ON eiszhs_collection_extended_checkpoint;
DROP TRIGGER eiszhs_membership_checkpoint_append_only ON eiszhs_collection_membership_checkpoint;
DROP TRIGGER eiszhs_raw_artifact_append_only ON eiszhs_raw_artifact;
DROP FUNCTION reject_eiszhs_current_snapshot_mutation();
ALTER TABLE eiszhs_current_observation
    DROP CONSTRAINT eiszhs_current_observation_raw_segment_unique,
    DROP COLUMN raw_artifact_id,
    DROP COLUMN cumulative_ddu_count,
    DROP COLUMN object_id;
DROP TABLE eiszhs_collection_step_claim;
DROP TABLE eiszhs_collection_extended_checkpoint;
DROP TABLE eiszhs_collection_object;
DROP TABLE eiszhs_collection_membership_checkpoint;
DROP TABLE eiszhs_raw_artifact;
ALTER TABLE analytical_session
    DROP COLUMN collection_total_requests,
    DROP COLUMN collection_completed_requests,
    DROP COLUMN residential_complex_region_codes;

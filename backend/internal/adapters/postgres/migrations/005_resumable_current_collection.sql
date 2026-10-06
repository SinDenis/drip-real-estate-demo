ALTER TABLE analytical_session
    ADD COLUMN collection_next_action_at timestamptz;

UPDATE analytical_session
SET collection_next_action_at = created_at
WHERE status = 'collecting';

CREATE TABLE eiszhs_collection_membership_object (
    analytical_session_id uuid NOT NULL REFERENCES analytical_session(id) ON DELETE CASCADE,
    complex_id text NOT NULL,
    object_id text NOT NULL,
    first_discovered_page integer NOT NULL CHECK (first_discovered_page >= 0),
    first_ordinal integer NOT NULL CHECK (first_ordinal >= 0),
    PRIMARY KEY (analytical_session_id, complex_id, object_id)
);

INSERT INTO eiszhs_collection_membership_object (
    analytical_session_id, complex_id, object_id, first_discovered_page, first_ordinal
)
SELECT analytical_session_id, complex_id, object_id, discovered_page, ordinal
FROM eiszhs_collection_object;

CREATE TRIGGER eiszhs_membership_object_append_only
BEFORE UPDATE OR DELETE ON eiszhs_collection_membership_object
FOR EACH ROW EXECUTE FUNCTION reject_eiszhs_current_snapshot_mutation();

---- create above / drop below ----

DROP TRIGGER eiszhs_membership_object_append_only ON eiszhs_collection_membership_object;
DROP TABLE eiszhs_collection_membership_object;
ALTER TABLE analytical_session DROP COLUMN collection_next_action_at;

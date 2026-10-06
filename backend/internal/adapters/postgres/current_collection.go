package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	"github.com/jackc/pgx/v5"
)

type membershipProgress struct {
	total    int64
	pageSize int
	lastPage int
}

type currentCollectionQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const currentCollectionPendingSQL = `
	WITH membership AS (
		SELECT checkpoint.complex_id, min(checkpoint.total) AS total,
		       min(checkpoint.page_size) AS page_size, max(checkpoint.page) AS last_page
		FROM eiszhs_collection_membership_checkpoint AS checkpoint
		WHERE checkpoint.analytical_session_id = $1::uuid
		GROUP BY checkpoint.complex_id
	), pending_membership AS (
		SELECT 1
		FROM analytical_session AS session
		CROSS JOIN LATERAL unnest(session.residential_complex_ids) AS selected(complex_id)
		LEFT JOIN membership ON membership.complex_id = selected.complex_id
		WHERE session.id = $1::uuid
		  AND (membership.complex_id IS NULL
		       OR (membership.last_page + 1) * membership.page_size < membership.total)
	), pending_extended AS (
		SELECT 1
		FROM eiszhs_collection_object AS object
		LEFT JOIN eiszhs_collection_extended_checkpoint AS completed
		  ON completed.analytical_session_id = object.analytical_session_id
		 AND completed.object_id = object.object_id
		WHERE object.analytical_session_id = $1::uuid AND completed.object_id IS NULL
	)
	SELECT EXISTS (SELECT 1 FROM pending_membership)
	    OR EXISTS (SELECT 1 FROM pending_extended)`

// ClaimCurrentCollection acquires a cross-replica lease before quota-bearing I/O.
//
//nolint:funlen,gocyclo // Row locking, plan recomputation, and lease creation form one atomic claim.
func (store *Store) ClaimCurrentCollection(
	ctx context.Context,
	sessionID string,
	now time.Time,
	lease time.Duration,
) (application.CurrentCollectionPlan, *application.CurrentCollectionClaim, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.CurrentCollectionPlan{}, nil, false, fmt.Errorf("begin current collection claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM analytical_session
		WHERE id = $1::uuid AND deleted_at IS NULL FOR UPDATE`, sessionID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidTextRepresentation(err) {
			return application.CurrentCollectionPlan{}, nil, false, nil
		}
		return application.CurrentCollectionPlan{}, nil, false, fmt.Errorf("lock current collection claim: %w", err)
	}
	if status != "collecting" {
		if err := tx.Commit(ctx); err != nil {
			return application.CurrentCollectionPlan{}, nil, false, fmt.Errorf("commit terminal collection claim: %w", err)
		}
		plan, found, loadErr := store.LoadCurrentCollection(ctx, sessionID)
		return plan, nil, found, loadErr
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM eiszhs_collection_step_claim
		WHERE analytical_session_id = $1::uuid AND expires_at <= $2`, sessionID, now); err != nil {
		return application.CurrentCollectionPlan{}, nil, false, fmt.Errorf("expire current collection claim: %w", err)
	}
	var claimed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM eiszhs_collection_step_claim
			WHERE analytical_session_id = $1::uuid
		)`, sessionID).Scan(&claimed); err != nil {
		return application.CurrentCollectionPlan{}, nil, false, fmt.Errorf("find current collection claim: %w", err)
	}
	if claimed {
		return application.CurrentCollectionPlan{}, nil, true, application.ErrConflict
	}
	plan, found, err := store.LoadCurrentCollection(ctx, sessionID)
	if err != nil || !found || plan.Next == nil {
		return store.finishEmptyClaim(ctx, tx, plan, found, err)
	}
	claim, err := insertCurrentCollectionClaim(ctx, tx, sessionID, *plan.Next, now, lease)
	if err != nil {
		return application.CurrentCollectionPlan{}, nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return application.CurrentCollectionPlan{}, nil, false, fmt.Errorf("commit current collection claim: %w", err)
	}
	return plan, &claim, true, nil
}

func (store *Store) finishEmptyClaim(
	ctx context.Context,
	tx pgx.Tx,
	plan application.CurrentCollectionPlan,
	found bool,
	loadErr error,
) (application.CurrentCollectionPlan, *application.CurrentCollectionClaim, bool, error) {
	if loadErr != nil || !found {
		return application.CurrentCollectionPlan{}, nil, found, loadErr
	}
	if err := tx.Commit(ctx); err != nil {
		return application.CurrentCollectionPlan{}, nil, false, fmt.Errorf("commit empty collection claim: %w", err)
	}
	return plan, nil, true, nil
}

func insertCurrentCollectionClaim(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
	step application.CurrentCollectionStep,
	now time.Time,
	lease time.Duration,
) (application.CurrentCollectionClaim, error) {
	var page *int
	if step.Kind == application.CollectionStepComplexMembership {
		page = &step.Page
	}
	var token string
	err := tx.QueryRow(ctx, `
		INSERT INTO eiszhs_collection_step_claim (
			analytical_session_id, step_kind, complex_id, page, object_id, claimed_at, expires_at
		) VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''), $6, $7)
		RETURNING claim_token::text`, sessionID, string(step.Kind), step.ComplexID,
		page, step.ObjectID, now, now.Add(lease)).Scan(&token)
	if err != nil {
		return application.CurrentCollectionClaim{}, fmt.Errorf("insert current collection claim: %w", err)
	}
	return application.CurrentCollectionClaim{Token: token, SessionID: sessionID, Step: step}, nil
}

// RenewCurrentCollectionClaim extends only the caller's live, still-current lease.
func (store *Store) RenewCurrentCollectionClaim(
	ctx context.Context,
	claim application.CurrentCollectionClaim,
	now time.Time,
	lease time.Duration,
) (bool, error) {
	result, err := store.pool.Exec(ctx, `
		UPDATE eiszhs_collection_step_claim
		SET expires_at = $3
		WHERE analytical_session_id = $1::uuid
		  AND claim_token = $2::uuid
		  AND expires_at > $4`, claim.SessionID, claim.Token, now.Add(lease), now)
	if err != nil {
		return false, fmt.Errorf("renew current collection claim: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

// ReleaseCurrentCollectionClaim releases only the caller's still-current lease.
func (store *Store) ReleaseCurrentCollectionClaim(
	ctx context.Context,
	claim application.CurrentCollectionClaim,
) error {
	if _, err := store.pool.Exec(ctx, `
		DELETE FROM eiszhs_collection_step_claim
		WHERE analytical_session_id = $1::uuid AND claim_token = $2::uuid`,
		claim.SessionID, claim.Token); err != nil {
		return fmt.Errorf("release current collection claim: %w", err)
	}
	return nil
}

// LoadCurrentCollection returns the durable next step for one visible session.
func (store *Store) LoadCurrentCollection(
	ctx context.Context,
	sessionID string,
) (application.CurrentCollectionPlan, bool, error) {
	session, found, err := store.GetAnalyticalSession(ctx, sessionID)
	if err != nil || !found {
		return application.CurrentCollectionPlan{}, found, err
	}
	plan := application.CurrentCollectionPlan{Session: session}
	if session.CollectionProgress != nil {
		plan.Progress = *session.CollectionProgress
	}
	if session.Status != "collecting" {
		return plan, true, nil
	}
	next, found, err := store.nextMembershipStep(ctx, store.pool, sessionID, session.ResidentialComplexIDs)
	if err != nil {
		return application.CurrentCollectionPlan{}, false, err
	}
	if !found {
		next, err = store.nextExtendedStep(ctx, store.pool, sessionID, session.ResidentialComplexIDs)
		if err != nil {
			return application.CurrentCollectionPlan{}, false, err
		}
	}
	plan.Next = next
	return plan, true, nil
}

func (store *Store) enrichCurrentCollectionProgress(
	ctx context.Context,
	querier currentCollectionQuerier,
	session *application.AnalyticalSession,
) error {
	progress := session.CollectionProgress
	if progress == nil {
		return nil
	}
	if err := querier.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM eiszhs_collection_object
		   WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM eiszhs_collection_extended_checkpoint
		   WHERE analytical_session_id = $1::uuid)`, session.ID).Scan(
		&progress.DiscoveredTotal, &progress.Fetched,
	); err != nil {
		return fmt.Errorf("query detailed current collection progress: %w", err)
	}
	setExactCollectionProgress(progress)
	return store.setCurrentCollectionStep(ctx, querier, session)
}

func setExactCollectionProgress(progress *application.CollectionProgress) {
	if progress.TotalRequests != nil {
		remaining := max(*progress.TotalRequests-progress.CompletedRequests, 0)
		progress.Remaining = &remaining
		percent := 100
		if *progress.TotalRequests > 0 {
			percent = progress.CompletedRequests * 100 / *progress.TotalRequests
		}
		progress.Percent = &percent
	}
}

func (store *Store) setCurrentCollectionStep(
	ctx context.Context,
	querier currentCollectionQuerier,
	session *application.AnalyticalSession,
) error {
	progress := session.CollectionProgress
	if session.Status != "collecting" {
		progress.Phase = "complete"
		progress.NextActionAt = nil
		return nil
	}
	next, found, err := store.nextMembershipStep(ctx, querier, session.ID, session.ResidentialComplexIDs)
	if err != nil {
		return err
	}
	if found {
		progress.Phase = "discovering_objects"
		progress.CurrentComplexID = &next.ComplexID
		return nil
	}
	next, err = store.nextExtendedStep(ctx, querier, session.ID, session.ResidentialComplexIDs)
	if err != nil {
		return err
	}
	progress.Phase = "collecting_objects"
	if next != nil {
		progress.CurrentComplexID = &next.ComplexID
		progress.CurrentObjectID = &next.ObjectID
	}
	return nil
}

func (store *Store) nextMembershipStep(
	ctx context.Context,
	querier currentCollectionQuerier,
	sessionID string,
	complexIDs []string,
) (*application.CurrentCollectionStep, bool, error) {
	progress, err := store.loadMembershipProgress(ctx, querier, sessionID)
	if err != nil {
		return nil, false, err
	}
	for _, complexID := range complexIDs {
		checkpoint, exists := progress[complexID]
		if !exists {
			return &application.CurrentCollectionStep{
				Kind: application.CollectionStepComplexMembership, ComplexID: complexID, Page: 0,
			}, true, nil
		}
		if int64((checkpoint.lastPage+1)*checkpoint.pageSize) < checkpoint.total {
			return &application.CurrentCollectionStep{
				Kind:      application.CollectionStepComplexMembership,
				ComplexID: complexID, Page: checkpoint.lastPage + 1,
			}, true, nil
		}
	}
	return nil, false, nil
}

func (store *Store) nextExtendedStep(
	ctx context.Context,
	querier currentCollectionQuerier,
	sessionID string,
	complexIDs []string,
) (*application.CurrentCollectionStep, error) {
	var complexID, objectID string
	err := querier.QueryRow(ctx, `
		SELECT object.complex_id, object.object_id
		FROM eiszhs_collection_object AS object
		LEFT JOIN eiszhs_collection_extended_checkpoint AS completed
		  ON completed.analytical_session_id = object.analytical_session_id
		 AND completed.object_id = object.object_id
		WHERE object.analytical_session_id = $1::uuid
		  AND completed.object_id IS NULL
		ORDER BY array_position($2::text[], object.complex_id),
		         object.discovered_page, object.ordinal
		LIMIT 1`, sessionID, complexIDs).Scan(&complexID, &objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"query next current extended object: %w", err,
		)
	}
	return &application.CurrentCollectionStep{
		Kind: application.CollectionStepCurrentExtended, ComplexID: complexID, ObjectID: objectID,
	}, nil
}

//nolint:funlen // The grouped query and complete row scan preserve one consistency boundary.
func (store *Store) loadMembershipProgress(
	ctx context.Context,
	querier currentCollectionQuerier,
	sessionID string,
) (map[string]membershipProgress, error) {
	rows, err := querier.Query(ctx, `
		SELECT checkpoint.complex_id, min(checkpoint.total),
		       min(checkpoint.page_size), max(checkpoint.page)
		FROM eiszhs_collection_membership_checkpoint AS checkpoint
		WHERE checkpoint.analytical_session_id = $1::uuid
		GROUP BY checkpoint.complex_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query current membership progress: %w", err)
	}
	defer rows.Close()
	result := make(map[string]membershipProgress)
	for rows.Next() {
		var complexID string
		var checkpoint membershipProgress
		if err := rows.Scan(
			&complexID, &checkpoint.total, &checkpoint.pageSize, &checkpoint.lastPage,
		); err != nil {
			return nil, fmt.Errorf("scan current membership progress: %w", err)
		}
		result[complexID] = checkpoint
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current membership progress: %w", err)
	}
	return result, nil
}

// CheckpointComplexMembership atomically records provenance and the next normalized objects.
//
//nolint:funlen,gocyclo // The transaction keeps provenance, normalized objects, and progress atomic.
func (store *Store) CheckpointComplexMembership(
	ctx context.Context,
	checkpoint application.MembershipCheckpoint,
) (application.AnalyticalSession, application.CollectionProgress, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"begin membership checkpoint: %w", err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockCollectingSession(ctx, tx, checkpoint.SessionID); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if err := consumeCurrentCollectionClaim(ctx, tx, application.CurrentCollectionClaim{
		Token: checkpoint.ClaimToken, SessionID: checkpoint.SessionID,
		Step: application.CurrentCollectionStep{
			Kind:      application.CollectionStepComplexMembership,
			ComplexID: checkpoint.ComplexID, Page: checkpoint.Page,
		},
	}); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM eiszhs_collection_membership_checkpoint
			WHERE analytical_session_id = $1::uuid AND complex_id = $2 AND page = $3
		)`, checkpoint.SessionID, checkpoint.ComplexID, checkpoint.Page).Scan(&exists); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"find membership checkpoint: %w", err,
		)
	}
	if exists {
		return store.replayedCollectionCheckpoint(ctx, tx, checkpoint.SessionID)
	}
	if err := validateMembershipContinuity(ctx, tx, checkpoint); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	artifactID, err := insertRawArtifact(ctx, tx, checkpoint.SessionID,
		application.CollectionStepComplexMembership, checkpoint.ComplexID,
		&checkpoint.Page, "", checkpoint.RawArtifact)
	if err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO eiszhs_collection_membership_checkpoint (
			analytical_session_id, complex_id, page, total, page_size,
			object_ids, raw_artifact_id, completed_at
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid, $8)`,
		checkpoint.SessionID, checkpoint.ComplexID, checkpoint.Page,
		checkpoint.Total, checkpoint.PageSize, checkpoint.ObjectIDs,
		artifactID, checkpoint.RawArtifact.ObservedAt,
	); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"insert membership checkpoint: %w", err,
		)
	}
	if err := insertMembershipObjects(ctx, tx, checkpoint); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE analytical_session
		SET collection_completed_requests = collection_completed_requests + 1,
		    collection_next_action_at = $2
		WHERE id = $1::uuid`, checkpoint.SessionID, checkpoint.RawArtifact.ObservedAt); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"advance membership progress: %w", err,
		)
	}
	if err := setCollectionTotalAfterPlanning(ctx, tx, checkpoint.SessionID); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if err := finalizeCurrentCollection(ctx, tx, checkpoint.SessionID); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	return store.committedCollectionCheckpoint(ctx, tx, checkpoint.SessionID)
}

func insertMembershipObjects(
	ctx context.Context,
	tx pgx.Tx,
	checkpoint application.MembershipCheckpoint,
) error {
	for ordinal, objectID := range checkpoint.ObjectIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO eiszhs_collection_membership_object (
				analytical_session_id, complex_id, object_id, first_discovered_page, first_ordinal
			) VALUES ($1::uuid, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`,
			checkpoint.SessionID, checkpoint.ComplexID, objectID, checkpoint.Page, ordinal,
		); err != nil {
			return fmt.Errorf("insert current membership object: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO eiszhs_collection_object (
				analytical_session_id, complex_id, object_id, discovered_page, ordinal
			) VALUES ($1::uuid, $2, $3, $4, $5)
			ON CONFLICT (analytical_session_id, object_id) DO NOTHING`,
			checkpoint.SessionID, checkpoint.ComplexID, objectID, checkpoint.Page, ordinal,
		); err != nil {
			return fmt.Errorf("insert current collection object: %w", err)
		}
	}
	return nil
}

//nolint:funlen // Stable pagination and cross-page uniqueness are one persistence invariant.
func validateMembershipContinuity(
	ctx context.Context,
	tx pgx.Tx,
	checkpoint application.MembershipCheckpoint,
) error {
	if err := validateMembershipPageShape(checkpoint); err != nil {
		return err
	}
	var total int64
	var pageSize int
	err := tx.QueryRow(ctx, `
		SELECT total, page_size
		FROM eiszhs_collection_membership_checkpoint
		WHERE analytical_session_id = $1::uuid AND complex_id = $2
		ORDER BY page LIMIT 1`, checkpoint.SessionID, checkpoint.ComplexID).Scan(&total, &pageSize)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read prior membership checkpoint: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) && checkpoint.Page != 0 {
		return errors.New("ЕИСЖС membership pages must start at page zero")
	}
	if err == nil && (checkpoint.Total != total || checkpoint.PageSize != pageSize) {
		return errors.New("ЕИСЖС membership pagination changed total or page size")
	}
	return nil
}

func validateMembershipPageShape(checkpoint application.MembershipCheckpoint) error {
	pageStart := int64(checkpoint.Page * checkpoint.PageSize)
	if pageStart > checkpoint.Total || (pageStart == checkpoint.Total && checkpoint.Total != 0) {
		return errors.New("ЕИСЖС membership page starts beyond total")
	}
	expected := int64(checkpoint.PageSize)
	if remaining := checkpoint.Total - pageStart; remaining < expected {
		expected = remaining
	}
	if int64(len(checkpoint.ObjectIDs)) != expected {
		return errors.New("ЕИСЖС membership page size does not match total")
	}
	return nil
}

//nolint:funlen // Unknown-to-exact planning is one transactional progress transition.
func setCollectionTotalAfterPlanning(ctx context.Context, tx pgx.Tx, sessionID string) error {
	var pendingMembership bool
	if err := tx.QueryRow(ctx, `
		WITH membership AS (
			SELECT checkpoint.complex_id, min(checkpoint.total) AS total,
			       min(checkpoint.page_size) AS page_size, max(checkpoint.page) AS last_page
			FROM eiszhs_collection_membership_checkpoint AS checkpoint
			WHERE checkpoint.analytical_session_id = $1::uuid
			GROUP BY checkpoint.complex_id
		)
		SELECT EXISTS (
			SELECT 1 FROM analytical_session AS session
			CROSS JOIN LATERAL unnest(session.residential_complex_ids) AS selected(complex_id)
			LEFT JOIN membership ON membership.complex_id = selected.complex_id
			WHERE session.id = $1::uuid
			  AND (membership.complex_id IS NULL
			       OR (membership.last_page + 1) * membership.page_size < membership.total)
		)`, sessionID).Scan(&pendingMembership); err != nil {
		return fmt.Errorf("query current collection planning: %w", err)
	}
	if pendingMembership {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE analytical_session AS session
		SET collection_total_requests = session.collection_completed_requests + (
			SELECT count(*) FROM eiszhs_collection_object AS object
			LEFT JOIN eiszhs_collection_extended_checkpoint AS completed
			  ON completed.analytical_session_id = object.analytical_session_id
			 AND completed.object_id = object.object_id
			WHERE object.analytical_session_id = session.id AND completed.object_id IS NULL
		)
		WHERE session.id = $1::uuid`, sessionID); err != nil {
		return fmt.Errorf("publish exact current collection total: %w", err)
	}
	return nil
}

// CheckpointCurrentExtended atomically records raw provenance, all categories, and session pins.
//
//nolint:funlen,gocyclo // The transaction maps each rollback boundary to its persistence operation.
func (store *Store) CheckpointCurrentExtended(
	ctx context.Context,
	checkpoint application.ExtendedCheckpoint,
) (application.AnalyticalSession, application.CollectionProgress, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"begin current extended checkpoint: %w", err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockCollectingSession(ctx, tx, checkpoint.SessionID); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if err := consumeCurrentCollectionClaim(ctx, tx, application.CurrentCollectionClaim{
		Token: checkpoint.ClaimToken, SessionID: checkpoint.SessionID,
		Step: application.CurrentCollectionStep{
			Kind:      application.CollectionStepCurrentExtended,
			ComplexID: checkpoint.ComplexID, ObjectID: checkpoint.ObjectID,
		},
	}); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM eiszhs_collection_extended_checkpoint
			WHERE analytical_session_id = $1::uuid AND object_id = $2
		)`, checkpoint.SessionID, checkpoint.ObjectID).Scan(&exists); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"find current extended checkpoint: %w", err,
		)
	}
	if exists {
		return store.replayedCollectionCheckpoint(ctx, tx, checkpoint.SessionID)
	}
	artifactID, err := insertRawArtifact(ctx, tx, checkpoint.SessionID,
		application.CollectionStepCurrentExtended, checkpoint.ComplexID,
		nil, checkpoint.ObjectID, checkpoint.RawArtifact)
	if err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO eiszhs_collection_extended_checkpoint (
			analytical_session_id, complex_id, object_id, raw_artifact_id, completed_at
		) VALUES ($1::uuid, $2, $3, $4::uuid, $5)`, checkpoint.SessionID,
		checkpoint.ComplexID, checkpoint.ObjectID, artifactID, checkpoint.RawArtifact.ObservedAt,
	); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"insert current extended checkpoint: %w", err,
		)
	}
	regionCode, err := collectionComplexRegion(ctx, tx, checkpoint.SessionID, checkpoint.ComplexID)
	if err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if err := insertCurrentObservations(ctx, tx, checkpoint, regionCode, artifactID); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO analytical_session_observation (analytical_session_id, observation_id)
		SELECT session.id, observation.id
		FROM analytical_session AS session
		JOIN eiszhs_current_observation AS observation
		  ON observation.raw_artifact_id = $2::uuid
		 AND observation.segment_code = ANY(session.segment_codes)
		WHERE session.id = $1::uuid
		ON CONFLICT DO NOTHING`, checkpoint.SessionID, artifactID); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"pin current observations: %w", err,
		)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE analytical_session
		SET collection_completed_requests = collection_completed_requests + 1,
		    collection_next_action_at = $2
		WHERE id = $1::uuid`, checkpoint.SessionID, checkpoint.RawArtifact.ObservedAt); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"advance current extended progress: %w", err,
		)
	}
	if err := finalizeCurrentCollection(ctx, tx, checkpoint.SessionID); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	return store.committedCollectionCheckpoint(ctx, tx, checkpoint.SessionID)
}

func insertCurrentObservations(
	ctx context.Context,
	tx pgx.Tx,
	checkpoint application.ExtendedCheckpoint,
	regionCode string,
	artifactID string,
) error {
	for _, metric := range checkpoint.Metrics {
		if _, err := tx.Exec(ctx, `
			INSERT INTO eiszhs_current_observation (
				region_code, complex_id, object_id, segment_code, cumulative_ddu_count,
				observed_at, expires_at, raw_artifact_id
			) VALUES (
				$1, $2, $3, $4, $5,
				$6::timestamptz, $6::timestamptz + interval '24 hours', $7::uuid
			)`,
			regionCode, checkpoint.ComplexID, checkpoint.ObjectID, metric.SegmentCode,
			metric.CumulativeDDUCount, checkpoint.RawArtifact.ObservedAt, artifactID,
		); err != nil {
			return fmt.Errorf("insert current observation: %w", err)
		}
	}
	return nil
}

func lockCollectingSession(ctx context.Context, tx pgx.Tx, sessionID string) error {
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM analytical_session WHERE id = $1::uuid FOR UPDATE`, sessionID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidTextRepresentation(err) {
			return application.ErrNotFound
		}
		return fmt.Errorf("lock current collection session: %w", err)
	}
	if status != "collecting" {
		return application.ErrConflict
	}
	return nil
}

func consumeCurrentCollectionClaim(
	ctx context.Context,
	tx pgx.Tx,
	claim application.CurrentCollectionClaim,
) error {
	var page *int
	if claim.Step.Kind == application.CollectionStepComplexMembership {
		page = &claim.Step.Page
	}
	result, err := tx.Exec(ctx, `
		DELETE FROM eiszhs_collection_step_claim
		WHERE analytical_session_id = $1::uuid
		  AND claim_token = $2::uuid
		  AND step_kind = $3
		  AND complex_id = $4
		  AND page IS NOT DISTINCT FROM $5::integer
		  AND object_id IS NOT DISTINCT FROM NULLIF($6, '')`,
		claim.SessionID, claim.Token, string(claim.Step.Kind), claim.Step.ComplexID,
		page, claim.Step.ObjectID)
	if err != nil {
		return fmt.Errorf("consume current collection claim: %w", err)
	}
	if result.RowsAffected() != 1 {
		return application.ErrConflict
	}
	return nil
}

func insertRawArtifact(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
	kind application.CurrentCollectionStepKind,
	complexID string,
	page *int,
	objectID string,
	artifact application.RawArtifact,
) (string, error) {
	var artifactID string
	err := tx.QueryRow(ctx, `
		INSERT INTO eiszhs_raw_artifact (
			analytical_session_id, step_kind, complex_id, page, object_id,
			object_key, sha256, byte_size, content_type, source_address, observed_at
		) VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, $10, $11)
		RETURNING id::text`, sessionID, string(kind), complexID, page, objectID,
		artifact.Key, artifact.SHA256, artifact.Size, artifact.ContentType,
		artifact.SourceAddress, artifact.ObservedAt,
	).Scan(&artifactID)
	if err != nil {
		return "", fmt.Errorf("insert raw artifact metadata: %w", err)
	}
	return artifactID, nil
}

func collectionComplexRegion(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
	complexID string,
) (string, error) {
	var regionCode string
	err := tx.QueryRow(ctx, `
		SELECT selected.region_code
		FROM analytical_session AS session
		CROSS JOIN LATERAL unnest(
			session.residential_complex_ids,
			session.residential_complex_region_codes
		) AS selected(complex_id, region_code)
		WHERE session.id = $1::uuid AND selected.complex_id = $2`, sessionID, complexID).Scan(&regionCode)
	if err != nil {
		return "", fmt.Errorf("resolve immutable collection complex region: %w", err)
	}
	return regionCode, nil
}

func finalizeCurrentCollection(ctx context.Context, tx pgx.Tx, sessionID string) error {
	var pending bool
	if err := tx.QueryRow(ctx, currentCollectionPendingSQL, sessionID).Scan(&pending); err != nil {
		return fmt.Errorf("query current collection completion: %w", err)
	}
	if pending {
		return nil
	}
	return completeCurrentCollection(ctx, tx, sessionID)
}

func completeCurrentCollection(ctx context.Context, tx pgx.Tx, sessionID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE analytical_session AS session
		SET status = CASE
		        WHEN session.segment_codes <@ ARRAY['residential', 'non_residential', 'parking']::text[]
		        THEN 'complete' ELSE 'partial' END,
		    observed_from = observation_window.observed_from,
		    observed_to = observation_window.observed_to,
		    collection_next_action_at = NULL
		FROM (
			SELECT min(observation.observed_at) AS observed_from,
			       max(observation.observed_at) AS observed_to
			FROM analytical_session_observation AS pin
			JOIN eiszhs_current_observation AS observation ON observation.id = pin.observation_id
			WHERE pin.analytical_session_id = $1::uuid
		) AS observation_window
		WHERE session.id = $1::uuid`, sessionID)
	if err != nil {
		return fmt.Errorf("complete current collection: %w", err)
	}
	return nil
}

func (store *Store) replayedCollectionCheckpoint(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
) (application.AnalyticalSession, application.CollectionProgress, error) {
	if err := tx.Rollback(ctx); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"release current collection lock: %w", err,
		)
	}
	session, found, err := store.GetAnalyticalSession(ctx, sessionID)
	if err != nil || !found {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	return session, collectionProgress(session), nil
}

func (store *Store) committedCollectionCheckpoint(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
) (application.AnalyticalSession, application.CollectionProgress, error) {
	if err := tx.Commit(ctx); err != nil {
		return application.AnalyticalSession{}, application.CollectionProgress{}, fmt.Errorf(
			"commit current collection checkpoint: %w", err,
		)
	}
	session, found, err := store.GetAnalyticalSession(ctx, sessionID)
	if err != nil || !found {
		return application.AnalyticalSession{}, application.CollectionProgress{}, err
	}
	return session, collectionProgress(session), nil
}

func collectionProgress(session application.AnalyticalSession) application.CollectionProgress {
	if session.CollectionProgress == nil {
		return application.CollectionProgress{}
	}
	return *session.CollectionProgress
}

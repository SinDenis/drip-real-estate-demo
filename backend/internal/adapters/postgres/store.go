package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store persists access and workspace state in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore opens a connection pool and applies embedded migrations.
func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure PostgreSQL pool: %w", err)
	}
	store := &Store{pool: pool}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) migrate(ctx context.Context) error {
	connection, err := store.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire PostgreSQL migration connection: %w", err)
	}
	defer connection.Release()
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	migrator, err := migrate.NewMigrator(ctx, connection.Conn(), "public.schema_version")
	if err != nil {
		return fmt.Errorf("configure PostgreSQL migrations: %w", err)
	}
	if err := migrator.LoadMigrations(migrations); err != nil {
		return fmt.Errorf("load PostgreSQL migrations: %w", err)
	}
	if err := migrator.Migrate(ctx); err != nil {
		return fmt.Errorf("run PostgreSQL migrations: %w", err)
	}
	return nil
}

// Check verifies that PostgreSQL accepts queries.
func (store *Store) Check(ctx context.Context) error {
	if err := store.pool.Ping(ctx); err != nil {
		return fmt.Errorf("check PostgreSQL: %w", err)
	}
	return nil
}

// Close releases the PostgreSQL connection pool.
func (store *Store) Close() {
	store.pool.Close()
}

// ListRegions returns the versioned selectable region seed in display order.
func (store *Store) ListRegions(ctx context.Context) ([]application.Region, error) {
	rows, err := store.pool.Query(ctx, `SELECT code, name FROM reference_region ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query reference regions: %w", err)
	}
	defer rows.Close()
	regions := make([]application.Region, 0, 89)
	for rows.Next() {
		var region application.Region
		if err := rows.Scan(&region.Code, &region.Name); err != nil {
			return nil, fmt.Errorf("scan reference region: %w", err)
		}
		regions = append(regions, region)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reference regions: %w", err)
	}
	return regions, nil
}

// FindRegion reports whether a code belongs to the versioned region seed.
func (store *Store) FindRegion(ctx context.Context, code string) (bool, error) {
	var found bool
	if err := store.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM reference_region WHERE code = $1)`, code,
	).Scan(&found); err != nil {
		return false, fmt.Errorf("find reference region: %w", err)
	}
	return found, nil
}

// LatestComplexCatalog returns the newest immutable snapshot for a region.
//
//nolint:funlen // Snapshot metadata and items are reconstructed as one repository operation.
func (store *Store) LatestComplexCatalog(
	ctx context.Context,
	regionCode string,
) (application.ComplexCatalog, bool, error) {
	var catalog application.ComplexCatalog
	var snapshotID string
	err := store.pool.QueryRow(ctx, `
		SELECT id::text, region_code, observed_at, expires_at
		FROM residential_complex_catalog_snapshot
		WHERE region_code = $1 ORDER BY observed_at DESC LIMIT 1`, regionCode,
	).Scan(&snapshotID, &catalog.RegionCode, &catalog.ObservedAt, &catalog.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ComplexCatalog{}, false, nil
	}
	if err != nil {
		return application.ComplexCatalog{}, false, fmt.Errorf("query complex catalog snapshot: %w", err)
	}
	rows, err := store.pool.Query(ctx, `
		SELECT complex_id, full_name FROM residential_complex_catalog_item
		WHERE snapshot_id = $1::uuid ORDER BY full_name, complex_id`, snapshotID)
	if err != nil {
		return application.ComplexCatalog{}, false, fmt.Errorf("query complex catalog items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item application.ResidentialComplex
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return application.ComplexCatalog{}, false, fmt.Errorf("scan complex catalog item: %w", err)
		}
		catalog.Items = append(catalog.Items, item)
	}
	if err := rows.Err(); err != nil {
		return application.ComplexCatalog{}, false, fmt.Errorf("iterate complex catalog items: %w", err)
	}
	return catalog, true, nil
}

// LoadComplexCatalogPages returns normalized successful pages from an interrupted refresh.
func (store *Store) LoadComplexCatalogPages(
	ctx context.Context,
	regionCode string,
) ([]application.ComplexCatalogPage, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT page, page_size, total, items
		FROM residential_complex_catalog_page_checkpoint
		WHERE region_code = $1 ORDER BY page`, regionCode)
	if err != nil {
		return nil, fmt.Errorf("query complex catalog page checkpoints: %w", err)
	}
	defer rows.Close()
	pages := make([]application.ComplexCatalogPage, 0)
	for rows.Next() {
		page := application.ComplexCatalogPage{RegionCode: regionCode}
		var items []byte
		if err := rows.Scan(&page.Page, &page.PageSize, &page.Total, &items); err != nil {
			return nil, fmt.Errorf("scan complex catalog page checkpoint: %w", err)
		}
		if err := json.Unmarshal(items, &page.Items); err != nil {
			return nil, fmt.Errorf("decode complex catalog page checkpoint: %w", err)
		}
		pages = append(pages, page)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate complex catalog page checkpoints: %w", err)
	}
	return pages, nil
}

// SaveComplexCatalogPage durably checkpoints one successful quota-bearing page.
func (store *Store) SaveComplexCatalogPage(
	ctx context.Context,
	page application.ComplexCatalogPage,
) error {
	items, err := json.Marshal(page.Items)
	if err != nil {
		return fmt.Errorf("encode complex catalog page checkpoint: %w", err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO residential_complex_catalog_page_checkpoint (
			region_code, page, page_size, total, items
		) VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (region_code, page) DO UPDATE
		SET page_size = EXCLUDED.page_size, total = EXCLUDED.total,
		    items = EXCLUDED.items, observed_at = clock_timestamp()`,
		page.RegionCode, page.Page, page.PageSize, page.Total, items)
	if err != nil {
		return fmt.Errorf("save complex catalog page checkpoint: %w", err)
	}
	return nil
}

// ClearComplexCatalogPages discards one semantically inconsistent refresh generation.
func (store *Store) ClearComplexCatalogPages(ctx context.Context, regionCode string) error {
	if _, err := store.pool.Exec(ctx, `
		DELETE FROM residential_complex_catalog_page_checkpoint WHERE region_code = $1`,
		regionCode); err != nil {
		return fmt.Errorf("clear complex catalog page checkpoints: %w", err)
	}
	return nil
}

// AcquireComplexCatalogRefresh serializes cold or expired refreshes for one region.
func (store *Store) AcquireComplexCatalogRefresh(
	ctx context.Context,
	regionCode string,
) (func(), error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin complex catalog refresh lease: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended('eiszhs:complexes:' || $1, 0))`,
		regionCode); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("acquire complex catalog refresh lease: %w", err)
	}
	return func() { _ = tx.Rollback(context.Background()) }, nil
}

// SaveComplexCatalog appends one complete immutable snapshot and its items atomically.
//
//nolint:funlen // Snapshot publication and checkpoint cleanup must commit atomically.
func (store *Store) SaveComplexCatalog(
	ctx context.Context,
	catalog application.ComplexCatalog,
) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin complex catalog snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var snapshotID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO residential_complex_catalog_snapshot (region_code, observed_at, expires_at)
		VALUES ($1, $2, $3) RETURNING id::text`,
		catalog.RegionCode, catalog.ObservedAt, catalog.ExpiresAt,
	).Scan(&snapshotID); err != nil {
		return fmt.Errorf("insert complex catalog snapshot: %w", err)
	}
	for _, item := range catalog.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO residential_complex_catalog_item (snapshot_id, complex_id, full_name)
			VALUES ($1::uuid, $2, $3)`, snapshotID, item.ID, item.Name); err != nil {
			return fmt.Errorf("insert complex catalog item: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM residential_complex_catalog_page_checkpoint WHERE region_code = $1`,
		catalog.RegionCode); err != nil {
		return fmt.Errorf("clear complex catalog page checkpoints: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit complex catalog snapshot: %w", err)
	}
	return nil
}

// FindAnalyticalSessionReplay returns a committed create result before mutable input resolution.
func (store *Store) FindAnalyticalSessionReplay(
	ctx context.Context,
	idempotency application.AnalyticalSessionIdempotency,
) (application.AnalyticalSession, bool, error) {
	var requestHash, sessionID string
	err := store.pool.QueryRow(ctx, `
		SELECT request_hash, analytical_session_id::text
		FROM analytical_session_idempotency
		WHERE account_id = $1::uuid AND endpoint = $2 AND idempotency_key = $3`,
		idempotency.AccountID, idempotency.Endpoint, idempotency.IdempotencyKey,
	).Scan(&requestHash, &sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.AnalyticalSession{}, false, nil
	}
	if err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("query analytical session replay: %w", err)
	}
	if requestHash != idempotency.RequestHash {
		return application.AnalyticalSession{}, false, application.ErrIdempotencyConflict
	}
	session, found, err := store.GetAnalyticalSession(ctx, sessionID)
	if err != nil {
		return application.AnalyticalSession{}, false, err
	}
	if !found {
		return application.AnalyticalSession{}, false, errors.New("idempotent analytical session is missing")
	}
	return session, true, nil
}

// ResolveSessionSelection returns authoritative display snapshots and reusable observations.
//
//nolint:funlen,gocyclo // Three ordered public result sets form one authoritative selection snapshot.
func (store *Store) ResolveSessionSelection(
	ctx context.Context,
	input application.CreateSessionInput,
	now time.Time,
) (application.ResolvedSessionSelection, error) {
	selection := application.ResolvedSessionSelection{}
	rows, err := store.pool.Query(ctx, `
		SELECT code, name FROM reference_region WHERE code = ANY($1) ORDER BY code`,
		input.RegionCodes)
	if err != nil {
		return selection, fmt.Errorf("query selected regions: %w", err)
	}
	for rows.Next() {
		var region application.SessionRegion
		if err := rows.Scan(&region.Code, &region.Name); err != nil {
			rows.Close()
			return selection, fmt.Errorf("scan selected region: %w", err)
		}
		selection.Regions = append(selection.Regions, region)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return selection, fmt.Errorf("iterate selected regions: %w", err)
	}
	rows.Close()

	rows, err = store.pool.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (region_code) id, region_code
			FROM residential_complex_catalog_snapshot
			WHERE region_code = ANY($1)
			ORDER BY region_code, observed_at DESC, id DESC
		)
		SELECT latest.region_code, item.complex_id, item.full_name
		FROM latest
		JOIN residential_complex_catalog_item AS item ON item.snapshot_id = latest.id
		WHERE item.complex_id = ANY($2)
		ORDER BY item.complex_id`, input.RegionCodes, input.ResidentialComplexIDs)
	if err != nil {
		return selection, fmt.Errorf("query selected complexes: %w", err)
	}
	for rows.Next() {
		var residentialComplex application.SessionComplex
		if err := rows.Scan(
			&residentialComplex.RegionCode, &residentialComplex.ID, &residentialComplex.Name,
		); err != nil {
			rows.Close()
			return selection, fmt.Errorf("scan selected complex: %w", err)
		}
		selection.Complexes = append(selection.Complexes, residentialComplex)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return selection, fmt.Errorf("iterate selected complexes: %w", err)
	}
	rows.Close()

	complexRegionCodes := make([]string, 0, len(selection.Complexes))
	complexIDs := make([]string, 0, len(selection.Complexes))
	for _, residentialComplex := range selection.Complexes {
		complexRegionCodes = append(complexRegionCodes, residentialComplex.RegionCode)
		complexIDs = append(complexIDs, residentialComplex.ID)
	}
	rows, err = store.pool.Query(ctx, `
		SELECT DISTINCT ON (observation.region_code, observation.complex_id, observation.segment_code)
		       observation.id::text, observation.region_code, observation.complex_id,
		       observation.segment_code, observation.observed_at
		FROM eiszhs_current_observation AS observation
		JOIN unnest($1::text[], $2::text[]) AS selected(region_code, complex_id)
		  ON selected.region_code = observation.region_code
		 AND selected.complex_id = observation.complex_id
		WHERE observation.segment_code = ANY($3)
		  AND observation.raw_artifact_id IS NULL
		  AND observation.expires_at > $4
		  AND observation.observed_at >= date_trunc('month', $4::timestamptz)
		ORDER BY observation.region_code, observation.complex_id, observation.segment_code,
		         observation.observed_at DESC, observation.id DESC`,
		complexRegionCodes, complexIDs, input.SegmentCodes, now)
	if err != nil {
		return selection, fmt.Errorf("query fresh current observations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var observation application.SessionObservation
		if err := rows.Scan(
			&observation.ID, &observation.RegionCode, &observation.ComplexID,
			&observation.SegmentCode, &observation.ObservedAt,
		); err != nil {
			return selection, fmt.Errorf("scan fresh current observation: %w", err)
		}
		selection.Observations = append(selection.Observations, observation)
	}
	if err := rows.Err(); err != nil {
		return selection, fmt.Errorf("iterate fresh current observations: %w", err)
	}
	return selection, nil
}

// CreateAnalyticalSession atomically applies idempotency and persists immutable snapshots.
//
//nolint:funlen,gocognit,gocyclo // The transaction keeps idempotency and observation pins atomic.
func (store *Store) CreateAnalyticalSession(
	ctx context.Context,
	created application.NewAnalyticalSession,
) (application.AnalyticalSession, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("begin analytical session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3, 0))`,
		created.AccountID, created.Endpoint, created.IdempotencyKey); err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("lock analytical session idempotency: %w", err)
	}
	var requestHash, sessionID string
	err = tx.QueryRow(ctx, `
		SELECT request_hash, analytical_session_id::text
		FROM analytical_session_idempotency
		WHERE account_id = $1::uuid AND endpoint = $2 AND idempotency_key = $3`,
		created.AccountID, created.Endpoint, created.IdempotencyKey,
	).Scan(&requestHash, &sessionID)
	if err == nil {
		if requestHash != created.RequestHash {
			return application.AnalyticalSession{}, false, application.ErrIdempotencyConflict
		}
		if err := tx.Rollback(ctx); err != nil {
			return application.AnalyticalSession{}, false, fmt.Errorf("release idempotency replay lock: %w", err)
		}
		session, found, err := store.GetAnalyticalSession(ctx, sessionID)
		if err != nil {
			return application.AnalyticalSession{}, false, err
		}
		if !found {
			return application.AnalyticalSession{}, false, errors.New("idempotent analytical session is missing")
		}
		return session, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return application.AnalyticalSession{}, false, fmt.Errorf("query analytical session idempotency: %w", err)
	}
	session := created.Session
	completedRequests := 0
	var totalRequests *int
	var nextActionAt *time.Time
	if session.CollectionProgress != nil {
		completedRequests = session.CollectionProgress.CompletedRequests
		totalRequests = session.CollectionProgress.TotalRequests
		nextActionAt = session.CollectionProgress.NextActionAt
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO analytical_session (
			id, status, created_by, created_at, region_names, residential_complex_names,
			segments, period_from, period_to, region_codes, residential_complex_ids,
			residential_complex_region_codes, segment_codes, observed_from, observed_to,
			collection_completed_requests, collection_total_requests, collection_next_action_at
		) VALUES (
			$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18
		)`, session.ID, session.Status, created.AccountID, session.CreatedAt,
		session.Regions, session.ResidentialComplexes, session.Segments,
		session.PeriodFrom, session.PeriodTo, session.RegionCodes,
		session.ResidentialComplexIDs, session.ResidentialComplexRegionCodes,
		session.SegmentCodes, session.ObservedFrom, session.ObservedTo,
		completedRequests, totalRequests, nextActionAt,
	); err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("insert analytical session: %w", err)
	}
	for _, observationID := range session.ObservationIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO analytical_session_observation (analytical_session_id, observation_id)
			VALUES ($1::uuid, $2::uuid)`, session.ID, observationID); err != nil {
			return application.AnalyticalSession{}, false, fmt.Errorf("pin analytical session observation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO analytical_session_idempotency (
			account_id, endpoint, idempotency_key, request_hash, analytical_session_id, created_at
		) VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6)`,
		created.AccountID, created.Endpoint, created.IdempotencyKey,
		created.RequestHash, session.ID, session.CreatedAt); err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("insert analytical session idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("commit analytical session: %w", err)
	}
	return session, false, nil
}

// WaitForEISZHSRequest coordinates the six-second business-call gate in PostgreSQL.
//
//nolint:funlen // The transaction lock, cancellable wait, and gate advance are one invariant.
func (store *Store) WaitForEISZHSRequest(ctx context.Context) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ЕИСЖС request gate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var delaySeconds float64
	if err := tx.QueryRow(ctx, `
		SELECT GREATEST(EXTRACT(EPOCH FROM (next_allowed_at - clock_timestamp())), 0)
		FROM source_request_gate
		WHERE source_key = 'eiszhs' FOR UPDATE`,
	).Scan(&delaySeconds); err != nil {
		return fmt.Errorf("lock ЕИСЖС request gate: %w", err)
	}
	delay := time.Duration(delaySeconds * float64(time.Second))
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for ЕИСЖС request gate: %w", ctx.Err())
		case <-timer.C:
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE source_request_gate
		SET next_allowed_at = clock_timestamp() + interval '6 seconds'
		WHERE source_key = 'eiszhs'`); err != nil {
		return fmt.Errorf("advance ЕИСЖС request gate: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ЕИСЖС request gate: %w", err)
	}
	return nil
}

// CountAccounts returns the number of local accounts.
func (store *Store) CountAccounts(ctx context.Context) (int64, error) {
	var count int64
	if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM user_account").Scan(&count); err != nil {
		return 0, fmt.Errorf("count user accounts: %w", err)
	}
	return count, nil
}

// CreateBootstrapAdmin atomically creates the first account when none exists.
//
//nolint:funlen // Keeping the lock, insert, audit, and commit visible preserves the transaction invariant.
func (store *Store) CreateBootstrapAdmin(
	ctx context.Context,
	account application.NewAccount,
) (bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "LOCK TABLE user_account IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return false, fmt.Errorf("lock user accounts for bootstrap: %w", err)
	}
	var count int64
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM user_account").Scan(&count); err != nil {
		return false, fmt.Errorf("count user accounts during bootstrap: %w", err)
	}
	if count > 0 {
		return false, nil
	}
	var accountID string
	err = tx.QueryRow(ctx, `
		INSERT INTO user_account (
			login, normalized_login, display_name, role, state, password_hash,
			temporary_password_expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		RETURNING id::text`,
		account.Login, account.NormalizedLogin, account.DisplayName, account.Role, account.State,
		account.PasswordHash, account.TemporaryPasswordExpiresAt, account.CreatedAt,
	).Scan(&accountID)
	if err != nil {
		return false, fmt.Errorf("insert bootstrap administrator: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (subject_account_id, event_type, occurred_at)
		VALUES ($1::uuid, 'access.bootstrap_admin_created', $2)`, accountID, account.CreatedAt); err != nil {
		return false, fmt.Errorf("record bootstrap audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit bootstrap transaction: %w", err)
	}
	return true, nil
}

// FindAccountByLogin resolves an account by its normalized login.
func (store *Store) FindAccountByLogin(
	ctx context.Context,
	normalizedLogin string,
) (application.Account, bool, error) {
	return scanAccount(store.pool.QueryRow(ctx, `
		SELECT id::text, login, display_name, role, state, password_hash,
		       temporary_password_expires_at, created_at, updated_at
		FROM user_account
		WHERE normalized_login = $1`, normalizedLogin))
}

// StartSession persists a hashed server-side session and its audit event.
func (store *Store) StartSession(ctx context.Context, session application.NewSession) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin login transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_session (
			account_id, token_hash, created_at, last_seen_at, expires_at, idle_expires_at
		) VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
		session.AccountID, session.TokenHash, session.CreatedAt, session.LastSeenAt,
		session.ExpiresAt, session.IdleExpiresAt,
	); err != nil {
		return fmt.Errorf("insert user session: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (actor_account_id, subject_account_id, event_type, occurred_at)
		VALUES ($1::uuid, $1::uuid, 'access.login_succeeded', $2)`,
		session.AccountID, session.CreatedAt,
	); err != nil {
		return fmt.Errorf("record login audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit login transaction: %w", err)
	}
	return nil
}

// FindAccountBySession resolves and refreshes an unexpired session.
func (store *Store) FindAccountBySession(
	ctx context.Context,
	tokenHash string,
	now time.Time,
	idleExpiresAt time.Time,
) (application.Account, bool, error) {
	return scanAccount(store.pool.QueryRow(ctx, `
		UPDATE user_session AS session
		SET last_seen_at = $2,
		    idle_expires_at = LEAST(session.expires_at, $3)
		FROM user_account AS account
		WHERE session.account_id = account.id
		  AND session.token_hash = $1
		  AND session.revoked_at IS NULL
		  AND session.expires_at > $2
		  AND session.idle_expires_at > $2
		  AND account.state <> 'deactivated'
		RETURNING account.id::text, account.login, account.display_name, account.role,
		          account.state, account.password_hash, account.temporary_password_expires_at,
		          account.created_at, account.updated_at`,
		tokenHash, now, idleExpiresAt,
	))
}

// ActivateAccount sets a permanent password and replaces temporary sessions.
//
//nolint:funlen // Account activation is one auditable transaction and should remain readable as a unit.
func (store *Store) ActivateAccount(
	ctx context.Context,
	accountID string,
	passwordHash string,
	session application.NewSession,
	now time.Time,
) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin account activation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `
		UPDATE user_account
		SET state = 'active', password_hash = $2, temporary_password_expires_at = NULL,
		    password_changed_at = $3, updated_at = $3
		WHERE id = $1::uuid AND state = 'pending_activation'`, accountID, passwordHash, now)
	if err != nil {
		return fmt.Errorf("activate user account: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("pending user account no longer exists")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE user_session SET revoked_at = $2
		WHERE account_id = $1::uuid AND revoked_at IS NULL`, accountID, now); err != nil {
		return fmt.Errorf("revoke temporary user sessions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_session (
			account_id, token_hash, created_at, last_seen_at, expires_at, idle_expires_at
		) VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
		session.AccountID, session.TokenHash, session.CreatedAt, session.LastSeenAt,
		session.ExpiresAt, session.IdleExpiresAt,
	); err != nil {
		return fmt.Errorf("insert activated user session: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (actor_account_id, subject_account_id, event_type, occurred_at)
		VALUES ($1::uuid, $1::uuid, 'access.password_changed', $2)`, accountID, now); err != nil {
		return fmt.Errorf("record password change audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit account activation transaction: %w", err)
	}
	return nil
}

// RevokeSession revokes one hashed server-side session.
func (store *Store) RevokeSession(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE user_session SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash, now)
	if err != nil {
		return fmt.Errorf("revoke user session: %w", err)
	}
	return nil
}

// LoginAllowed reports whether login and client address throttles have elapsed.
func (store *Store) LoginAllowed(
	ctx context.Context,
	normalizedLogin string,
	clientIP string,
	now time.Time,
) (bool, time.Duration, error) {
	var blockedUntil *time.Time
	err := store.pool.QueryRow(ctx, `
		SELECT max(blocked_until) FROM login_throttle
		WHERE throttle_key = ANY($1::text[])`, throttleKeys(normalizedLogin, clientIP)).Scan(&blockedUntil)
	if err != nil {
		return false, 0, fmt.Errorf("query login throttle: %w", err)
	}
	if blockedUntil == nil || !blockedUntil.After(now) {
		return true, 0, nil
	}
	return false, blockedUntil.Sub(now), nil
}

// RecordLoginFailure appends an audit event and advances durable throttles.
//
//nolint:funlen // Audit and both throttle dimensions must commit or roll back together.
func (store *Store) RecordLoginFailure(
	ctx context.Context,
	normalizedLogin string,
	clientIP string,
	now time.Time,
) error {
	metadata, err := json.Marshal(map[string]string{"login": normalizedLogin})
	if err != nil {
		return fmt.Errorf("encode login audit metadata: %w", err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failed login transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_event (event_type, occurred_at, metadata)
		VALUES ('access.login_failed', $1, $2::jsonb)`, now, metadata)
	if err != nil {
		return fmt.Errorf("record failed login audit event: %w", err)
	}
	for index, key := range throttleKeys(normalizedLogin, clientIP) {
		minimumFailures := 1
		if index == 1 {
			minimumFailures = 10
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO login_throttle (throttle_key, failure_count, blocked_until, updated_at)
			VALUES ($1, 1, $2, $2)
			ON CONFLICT (throttle_key) DO UPDATE
			SET failure_count = login_throttle.failure_count + 1,
			    blocked_until = CASE
			        WHEN login_throttle.failure_count + 1 >= $3
			        THEN $2 + make_interval(secs => LEAST(
			            power(2, login_throttle.failure_count + 1 - $3)::integer, 30
			        ))
			        ELSE $2
			    END,
			    updated_at = $2`, key, now, minimumFailures); err != nil {
			return fmt.Errorf("update login throttle: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed login transaction: %w", err)
	}
	return nil
}

// ClearLoginFailures removes only the account-specific throttle after a successful login.
// The address throttle remains durable so one valid account cannot reset protection for every login.
func (store *Store) ClearLoginFailures(ctx context.Context, normalizedLogin string) error {
	_, err := store.pool.Exec(ctx, `
		DELETE FROM login_throttle WHERE throttle_key = $1`,
		"login:"+normalizedLogin,
	)
	if err != nil {
		return fmt.Errorf("delete login throttle: %w", err)
	}
	return nil
}

// ListAnalyticalSessions returns an ordered offset cursor page.
//
//nolint:funlen // The explicit scan keeps the query projection and domain mapping reviewable together.
func (store *Store) ListAnalyticalSessions(
	ctx context.Context,
	offset int,
	limit int,
) ([]application.AnalyticalSession, bool, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT session.id::text, session.status, session.created_at,
		       account.id::text, account.display_name, session.region_names,
		       session.residential_complex_names, session.segments,
		       session.period_from::timestamptz, session.period_to::timestamptz,
		       session.region_codes, session.residential_complex_ids,
		       session.residential_complex_region_codes, session.segment_codes,
		       session.observed_from, session.observed_to
		FROM analytical_session AS session
		JOIN user_account AS account ON account.id = session.created_by
		WHERE session.deleted_at IS NULL
		ORDER BY
			CASE WHEN session.status IN ('queued', 'collecting') THEN 0 ELSE 1 END,
			session.created_at DESC, session.id
		LIMIT $1 OFFSET $2`, limit+1, offset)
	if err != nil {
		return nil, false, fmt.Errorf("query analytical sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]application.AnalyticalSession, 0)
	for rows.Next() {
		var session application.AnalyticalSession
		if err := rows.Scan(
			&session.ID, &session.Status, &session.CreatedAt,
			&session.CreatedByID, &session.CreatedByDisplayName, &session.Regions,
			&session.ResidentialComplexes, &session.Segments,
			&session.PeriodFrom, &session.PeriodTo, &session.RegionCodes,
			&session.ResidentialComplexIDs, &session.ResidentialComplexRegionCodes,
			&session.SegmentCodes,
			&session.ObservedFrom, &session.ObservedTo,
		); err != nil {
			return nil, false, fmt.Errorf("scan analytical session: %w", err)
		}
		session.Name = application.DeterministicSessionName(session)
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate analytical sessions: %w", err)
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	return sessions, hasMore, nil
}

// GetAnalyticalSession returns a visible session by identifier.
//
//nolint:funlen // The complete immutable snapshot is scanned through one auditable query.
func (store *Store) GetAnalyticalSession(
	ctx context.Context,
	sessionID string,
) (application.AnalyticalSession, bool, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("begin analytical session snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	session, found, err := store.getAnalyticalSession(ctx, tx, sessionID)
	if err != nil {
		return application.AnalyticalSession{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("commit analytical session snapshot: %w", err)
	}
	return session, found, nil
}

//nolint:funlen // The complete snapshot projection and domain mapping stay auditable together.
func (store *Store) getAnalyticalSession(
	ctx context.Context,
	querier currentCollectionQuerier,
	sessionID string,
) (application.AnalyticalSession, bool, error) {
	var session application.AnalyticalSession
	var completedRequests int
	var totalRequests *int
	var nextActionAt *time.Time
	var cumulativeDDUCount *int64
	err := querier.QueryRow(ctx, `
		SELECT session.id::text, session.status, session.created_at,
		       account.id::text, account.display_name, session.region_names,
		       session.residential_complex_names, session.segments,
		       session.period_from::timestamptz, session.period_to::timestamptz,
		       session.region_codes, session.residential_complex_ids,
		       session.residential_complex_region_codes, session.segment_codes,
		       session.observed_from, session.observed_to,
		       session.collection_completed_requests, session.collection_total_requests,
		       session.collection_next_action_at,
		       COALESCE((
		           SELECT array_agg(pin.observation_id::text ORDER BY pin.observation_id::text)
		           FROM analytical_session_observation AS pin
		           WHERE pin.analytical_session_id = session.id
		       ), '{}'::text[]),
		       (
		           SELECT sum(observation.cumulative_ddu_count)
		           FROM analytical_session_observation AS pin
		           JOIN eiszhs_current_observation AS observation ON observation.id = pin.observation_id
		           WHERE pin.analytical_session_id = session.id
		       )
		FROM analytical_session AS session
		JOIN user_account AS account ON account.id = session.created_by
		WHERE session.id = $1::uuid AND session.deleted_at IS NULL`, sessionID).Scan(
		&session.ID, &session.Status, &session.CreatedAt,
		&session.CreatedByID, &session.CreatedByDisplayName, &session.Regions,
		&session.ResidentialComplexes, &session.Segments,
		&session.PeriodFrom, &session.PeriodTo, &session.RegionCodes,
		&session.ResidentialComplexIDs, &session.ResidentialComplexRegionCodes,
		&session.SegmentCodes,
		&session.ObservedFrom, &session.ObservedTo,
		&completedRequests, &totalRequests, &nextActionAt,
		&session.ObservationIDs, &cumulativeDDUCount,
	)
	if errors.Is(err, pgx.ErrNoRows) || isInvalidTextRepresentation(err) {
		return application.AnalyticalSession{}, false, nil
	}
	if err != nil {
		return application.AnalyticalSession{}, false, fmt.Errorf("scan analytical session: %w", err)
	}
	session.Name = application.DeterministicSessionName(session)
	if session.Status == "collecting" || totalRequests != nil {
		session.CollectionProgress = &application.CollectionProgress{
			CompletedRequests: completedRequests, TotalRequests: totalRequests,
			NextActionAt: nextActionAt,
		}
		if err := store.enrichCurrentCollectionProgress(ctx, querier, &session); err != nil {
			return application.AnalyticalSession{}, false, err
		}
	}
	if len(session.ObservationIDs) > 0 {
		session.CurrentDashboard = &application.CurrentDashboard{
			SourceLabel: "Текущий срез ЕИСЖС", CumulativeDDUCount: cumulativeDDUCount,
		}
	}
	return session, true, nil
}

// ListAccounts returns every local account in display order.
func (store *Store) ListAccounts(ctx context.Context) ([]application.Account, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id::text, login, display_name, role, state, password_hash,
		       temporary_password_expires_at, created_at, updated_at
		FROM user_account
		ORDER BY display_name, login`)
	if err != nil {
		return nil, fmt.Errorf("query user accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]application.Account, 0)
	for rows.Next() {
		account, _, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user accounts: %w", err)
	}
	return accounts, nil
}

// CreateAccount persists a pending account and audit event atomically.
//
//nolint:funlen // Account creation and its audit event form one transaction boundary.
func (store *Store) CreateAccount(
	ctx context.Context,
	actorID string,
	newAccount application.NewAccount,
) (application.Account, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.Account{}, fmt.Errorf("begin account creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	account, _, err := scanAccount(tx.QueryRow(ctx, `
		INSERT INTO user_account (
			login, normalized_login, display_name, role, state, password_hash,
			temporary_password_expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		RETURNING id::text, login, display_name, role, state, password_hash,
		          temporary_password_expires_at, created_at, updated_at`,
		newAccount.Login, newAccount.NormalizedLogin, newAccount.DisplayName, newAccount.Role,
		newAccount.State, newAccount.PasswordHash, newAccount.TemporaryPasswordExpiresAt,
		newAccount.CreatedAt,
	))
	if isUniqueViolation(err) {
		return application.Account{}, application.ErrConflict
	}
	if err != nil {
		return application.Account{}, fmt.Errorf("insert user account: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, account.ID, "access.employee_created", newAccount.CreatedAt); err != nil {
		return application.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return application.Account{}, fmt.Errorf("commit account creation: %w", err)
	}
	return account, nil
}

// UpdateAccount changes mutable identity fields with last-administrator protection.
//
//nolint:funlen // Authorization invariant, update, audit, and commit are one transaction boundary.
func (store *Store) UpdateAccount(
	ctx context.Context,
	actorID string,
	employeeID string,
	changes application.EmployeeChanges,
	now time.Time,
) (application.Account, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.Account{}, fmt.Errorf("begin account update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, found, err := accountForUpdate(ctx, tx, employeeID)
	if err != nil {
		return application.Account{}, err
	}
	if !found {
		return application.Account{}, application.ErrNotFound
	}
	if err := validateRoleChange(ctx, tx, current, employeeID, changes.Role); err != nil {
		return application.Account{}, err
	}
	account, _, err := scanAccount(tx.QueryRow(ctx, `
		UPDATE user_account
		SET display_name = COALESCE($2, display_name), role = COALESCE($3, role), updated_at = $4
		WHERE id = $1::uuid
		RETURNING id::text, login, display_name, role, state, password_hash,
		          temporary_password_expires_at, created_at, updated_at`,
		employeeID, changes.DisplayName, changes.Role, now,
	))
	if err != nil {
		return application.Account{}, fmt.Errorf("update user account: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, employeeID, "access.employee_updated", now); err != nil {
		return application.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return application.Account{}, fmt.Errorf("commit account update: %w", err)
	}
	return account, nil
}

// DeactivateAccount disables access and revokes all sessions atomically.
//
//nolint:funlen // Deactivation, session revocation, and audit must stay in one visible transaction.
func (store *Store) DeactivateAccount(
	ctx context.Context,
	actorID string,
	employeeID string,
	now time.Time,
) (application.Account, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.Account{}, fmt.Errorf("begin account deactivation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, found, err := accountForUpdate(ctx, tx, employeeID)
	if err != nil {
		return application.Account{}, err
	}
	if !found {
		return application.Account{}, application.ErrNotFound
	}
	if current.Role == application.RoleAnalyticsAdmin && current.State == application.AccountActive {
		if err := requireAnotherActiveAdmin(ctx, tx, employeeID); err != nil {
			return application.Account{}, err
		}
	}
	account, _, err := scanAccount(tx.QueryRow(ctx, `
		UPDATE user_account
		SET state = 'deactivated', temporary_password_expires_at = NULL, updated_at = $2
		WHERE id = $1::uuid
		RETURNING id::text, login, display_name, role, state, password_hash,
		          temporary_password_expires_at, created_at, updated_at`, employeeID, now))
	if err != nil {
		return application.Account{}, fmt.Errorf("deactivate user account: %w", err)
	}
	if err := revokeAllSessions(ctx, tx, employeeID, now); err != nil {
		return application.Account{}, err
	}
	if err := insertAudit(ctx, tx, actorID, employeeID, "access.employee_deactivated", now); err != nil {
		return application.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return application.Account{}, fmt.Errorf("commit account deactivation: %w", err)
	}
	return account, nil
}

// IssueTemporaryPassword stores a new hash and returns the account to pending activation.
//
//nolint:funlen // Password replacement, revocation, and audit must commit atomically.
func (store *Store) IssueTemporaryPassword(
	ctx context.Context,
	actorID string,
	employeeID string,
	passwordHash string,
	expiresAt time.Time,
	now time.Time,
	restore bool,
) (application.Account, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return application.Account{}, fmt.Errorf("begin temporary password issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, found, err := accountForUpdate(ctx, tx, employeeID)
	if err != nil {
		return application.Account{}, err
	}
	if !found {
		return application.Account{}, application.ErrNotFound
	}
	if err := validateTemporaryPasswordIssue(ctx, tx, current, employeeID, restore); err != nil {
		return application.Account{}, err
	}
	account, _, err := scanAccount(tx.QueryRow(ctx, `
		UPDATE user_account
		SET state = 'pending_activation', password_hash = $2,
		    temporary_password_expires_at = $3, updated_at = $4
		WHERE id = $1::uuid
		RETURNING id::text, login, display_name, role, state, password_hash,
		          temporary_password_expires_at, created_at, updated_at`,
		employeeID, passwordHash, expiresAt, now,
	))
	if err != nil {
		return application.Account{}, fmt.Errorf("issue temporary password: %w", err)
	}
	if err := revokeAllSessions(ctx, tx, employeeID, now); err != nil {
		return application.Account{}, err
	}
	eventType := "access.password_reset"
	if restore {
		eventType = "access.employee_restored"
	}
	if err := insertAudit(ctx, tx, actorID, employeeID, eventType, now); err != nil {
		return application.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return application.Account{}, fmt.Errorf("commit temporary password issue: %w", err)
	}
	return account, nil
}

// RevokeAllSessions revokes every session for an existing account.
func (store *Store) RevokeAllSessions(
	ctx context.Context,
	actorID string,
	employeeID string,
	now time.Time,
) (bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin session revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_account WHERE id = $1::uuid)`, employeeID).Scan(&exists); err != nil {
		if isInvalidTextRepresentation(err) {
			return false, nil
		}
		return false, fmt.Errorf("find session revocation account: %w", err)
	}
	if !exists {
		return false, nil
	}
	if err := revokeAllSessions(ctx, tx, employeeID, now); err != nil {
		return false, err
	}
	if err := insertAudit(ctx, tx, actorID, employeeID, "access.sessions_revoked", now); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit session revocation: %w", err)
	}
	return true, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanAccount(row rowScanner) (application.Account, bool, error) {
	var account application.Account
	err := row.Scan(
		&account.ID, &account.Login, &account.DisplayName, &account.Role,
		&account.State, &account.PasswordHash, &account.TemporaryPasswordExpiresAt,
		&account.CreatedAt, &account.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.Account{}, false, nil
	}
	if err != nil {
		return application.Account{}, false, fmt.Errorf("scan user account: %w", err)
	}
	return account, true, nil
}

func throttleKeys(normalizedLogin string, clientIP string) []string {
	if clientIP == "" {
		clientIP = "unknown"
	}
	return []string{"login:" + normalizedLogin, "ip:" + clientIP}
}

func accountForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	employeeID string,
) (application.Account, bool, error) {
	account, found, err := scanAccount(tx.QueryRow(ctx, `
		SELECT id::text, login, display_name, role, state, password_hash,
		       temporary_password_expires_at, created_at, updated_at
		FROM user_account WHERE id = $1::uuid FOR UPDATE`, employeeID))
	if isInvalidTextRepresentation(err) {
		return application.Account{}, false, nil
	}
	return account, found, err
}

func requireAnotherActiveAdmin(ctx context.Context, tx pgx.Tx, employeeID string) error {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM user_account
		WHERE role = 'analytics_admin' AND state = 'active' AND id <> $1::uuid`, employeeID).Scan(&count); err != nil {
		return fmt.Errorf("count remaining active administrators: %w", err)
	}
	if count == 0 {
		return application.ErrConflict
	}
	return nil
}

func validateRoleChange(
	ctx context.Context,
	tx pgx.Tx,
	current application.Account,
	employeeID string,
	role *application.Role,
) error {
	removesActiveAdministrator := role != nil &&
		current.Role == application.RoleAnalyticsAdmin &&
		*role != application.RoleAnalyticsAdmin &&
		current.State == application.AccountActive
	if !removesActiveAdministrator {
		return nil
	}
	return requireAnotherActiveAdmin(ctx, tx, employeeID)
}

func validateTemporaryPasswordIssue(
	ctx context.Context,
	tx pgx.Tx,
	current application.Account,
	employeeID string,
	restore bool,
) error {
	if restore != (current.State == application.AccountDeactivated) {
		return application.ErrConflict
	}
	if current.Role != application.RoleAnalyticsAdmin || current.State != application.AccountActive {
		return nil
	}
	return requireAnotherActiveAdmin(ctx, tx, employeeID)
}

func revokeAllSessions(ctx context.Context, tx pgx.Tx, employeeID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE user_session SET revoked_at = $2
		WHERE account_id = $1::uuid AND revoked_at IS NULL`, employeeID, now); err != nil {
		return fmt.Errorf("revoke all user sessions: %w", err)
	}
	return nil
}

func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	subjectID string,
	eventType string,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_event (actor_account_id, subject_account_id, event_type, occurred_at)
		VALUES ($1::uuid, $2::uuid, $3, $4)`, actorID, subjectID, eventType, now); err != nil {
		return fmt.Errorf("record %s audit event: %w", eventType, err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}

func isInvalidTextRepresentation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "22P02"
}

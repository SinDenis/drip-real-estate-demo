//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestPublicAPIUsesReadyInfrastructure(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")

	readiness := getJSON(t, client, baseURL+"/health/ready")
	if readiness["status"] != "ready" {
		t.Fatalf("readiness = %#v, want ready", readiness)
	}
}

func TestAdministrativeAPIRequiresAServerSideSession(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")

	response, err := client.Get(baseURL + "/api/v1/employees")
	if err != nil {
		t.Fatalf("GET employees: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET employees status = %d, want 401", response.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode GET employees: %v", err)
	}
	if body.Error.Code != "unauthenticated" {
		t.Fatalf("GET employees error code = %q, want unauthenticated", body.Error.Code)
	}
}

func TestCurrentMarketCatalogUsesPostgreSQLAndFakeEISZHS(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	regions := requestJSON(t, admin, http.MethodGet, baseURL+"/api/v1/catalog/regions", nil)
	if regions.Status != http.StatusOK {
		t.Fatalf("GET regions = %d: %s", regions.Status, regions.Body)
	}
	var regionCatalog struct {
		Version string `json:"version"`
		Items   []struct {
			Code string `json:"code"`
		} `json:"items"`
	}
	if err := json.Unmarshal(regions.Body, &regionCatalog); err != nil {
		t.Fatalf("decode regions: %v", err)
	}
	if regionCatalog.Version != "fns-ssrf-2025-10-01" || len(regionCatalog.Items) != 89 {
		t.Fatalf("region catalog = version %q, items %d", regionCatalog.Version, len(regionCatalog.Items))
	}
	segments := requestJSON(t, admin, http.MethodGet, baseURL+"/api/v1/catalog/segments", nil)
	if segments.Status != http.StatusOK || !bytes.Contains(segments.Body, []byte(`"industrial_warehouse"`)) {
		t.Fatalf("GET segments = %d: %s", segments.Status, segments.Body)
	}
	complexes := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/catalog/regions/77/complexes", nil)
	if complexes.Status != http.StatusOK || !bytes.Contains(complexes.Body, []byte(`"complexId":"1001"`)) {
		t.Fatalf("GET complexes = %d: %s", complexes.Status, complexes.Body)
	}
}

func TestWarmCurrentObservationCreatesAnImmutableIdempotentSession(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 15 * time.Second
	complexes := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/catalog/regions/78/complexes", nil)
	if complexes.Status != http.StatusOK {
		t.Fatalf("prepare fresh complex catalog = %d: %s", complexes.Status, complexes.Body)
	}
	database := openDatabase(t)
	defer database.Close(t.Context())
	var snapshotID string
	var originalObservedAt, originalExpiresAt time.Time
	if err := database.QueryRow(t.Context(), `
		SELECT id::text, observed_at, expires_at
		FROM residential_complex_catalog_snapshot
		WHERE region_code = '77' ORDER BY observed_at DESC, id DESC LIMIT 1`,
	).Scan(&snapshotID, &originalObservedAt, &originalExpiresAt); err != nil {
		t.Fatalf("load selected complex snapshot: %v", err)
	}
	if _, err := database.Exec(t.Context(), `
		UPDATE residential_complex_catalog_snapshot
		SET observed_at = now() - interval '48 hours', expires_at = now() - interval '24 hours'
		WHERE id = $1::uuid`, snapshotID); err != nil {
		t.Fatalf("make selected complex snapshot stale: %v", err)
	}
	defer func() {
		_, _ = database.Exec(context.Background(), `
			UPDATE residential_complex_catalog_snapshot SET observed_at = $2, expires_at = $3
			WHERE id = $1::uuid`, snapshotID, originalObservedAt, originalExpiresAt)
	}()
	if _, err := database.Exec(t.Context(), `
		DELETE FROM eiszhs_current_observation
		WHERE region_code = '77' AND complex_id = '1001' AND segment_code = 'residential';
		INSERT INTO eiszhs_current_observation (
			region_code, complex_id, segment_code, observed_at, expires_at
		) VALUES (
			'77', '1001', 'residential', now() - interval '30 minutes', now() + interval '23 hours'
		)`); err != nil {
		t.Fatalf("seed fresh common observation: %v", err)
	}
	body := map[string]any{
		"regionCodes": []string{"77"}, "residentialComplexIds": []string{"1001"},
		"segmentCodes": []string{"residential"},
	}
	first := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions", body,
		map[string]string{"Idempotency-Key": "backend-e2e-warm-session"})
	if first.Status != http.StatusCreated {
		t.Fatalf("create warm session = %d: %s", first.Status, first.Body)
	}
	var created analyticalSessionDetail
	if err := json.Unmarshal(first.Body, &created); err != nil {
		t.Fatalf("decode created session: %v", err)
	}
	identifier, err := uuid.Parse(created.ID)
	if err != nil || identifier.Version() != 7 || created.Status != "complete" ||
		len(created.Filters.RegionCodes) != 1 || created.Filters.Period != nil ||
		len(created.ObservedWindow.From) == 0 {
		t.Fatalf("created session = %#v, UUID error = %v", created, err)
	}
	if _, err := database.Exec(t.Context(), `
		UPDATE residential_complex_catalog_item SET complex_id = 'temporarily-unavailable'
		WHERE snapshot_id = $1::uuid AND complex_id = '1001'`, snapshotID); err != nil {
		t.Fatalf("make mutable catalog unable to resolve the original request: %v", err)
	}
	replay := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions", body,
		map[string]string{"Idempotency-Key": "backend-e2e-warm-session"})
	var replayed analyticalSessionDetail
	if replay.Status != http.StatusCreated || json.Unmarshal(replay.Body, &replayed) != nil || replayed.ID != created.ID {
		t.Fatalf("idempotent replay = %d %s, first ID = %s", replay.Status, replay.Body, created.ID)
	}
	conflictBody := map[string]any{
		"regionCodes": []string{"77"}, "residentialComplexIds": []string{"1001"},
		"segmentCodes": []string{"parking"},
	}
	conflict := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions", conflictBody,
		map[string]string{"Idempotency-Key": "backend-e2e-warm-session"})
	if conflict.Status != http.StatusConflict || !bytes.Contains(conflict.Body, []byte("idempotency_conflict")) {
		t.Fatalf("conflicting replay = %d: %s", conflict.Status, conflict.Body)
	}
	if _, err := database.Exec(t.Context(), `
		UPDATE residential_complex_catalog_item SET complex_id = '1001'
		WHERE snapshot_id = $1::uuid AND complex_id = 'temporarily-unavailable'`, snapshotID); err != nil {
		t.Fatalf("restore catalog before an independent submission: %v", err)
	}
	if _, err := database.Exec(t.Context(), `
		UPDATE residential_complex_catalog_snapshot SET observed_at = $2, expires_at = $3
		WHERE id = $1::uuid`, snapshotID, originalObservedAt, originalExpiresAt); err != nil {
		t.Fatalf("restore catalog freshness before an independent submission: %v", err)
	}
	second := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions", body,
		map[string]string{"Idempotency-Key": "backend-e2e-second-submit"})
	var independentlyCreated analyticalSessionDetail
	if second.Status != http.StatusCreated || json.Unmarshal(second.Body, &independentlyCreated) != nil ||
		independentlyCreated.ID == created.ID {
		t.Fatalf("independent submit = %d %s, first ID = %s", second.Status, second.Body, created.ID)
	}
	if _, err := database.Exec(t.Context(), `
		UPDATE residential_complex_catalog_item SET full_name = 'ЖК Переименованный'
		WHERE snapshot_id = (
			SELECT id FROM residential_complex_catalog_snapshot
			WHERE region_code = '77' ORDER BY observed_at DESC LIMIT 1
		) AND complex_id = '1001'`); err != nil {
		t.Fatalf("change shared catalog after session creation: %v", err)
	}
	defer func() {
		_, _ = database.Exec(context.Background(), `
			UPDATE residential_complex_catalog_item SET full_name = 'ЖК Северный, регион 77'
			WHERE snapshot_id = (
				SELECT id FROM residential_complex_catalog_snapshot
				WHERE region_code = '77' ORDER BY observed_at DESC LIMIT 1
			) AND complex_id = '1001'`)
	}()
	persisted := requestJSON(t, admin, http.MethodGet, baseURL+"/api/v1/sessions/"+created.ID, nil)
	if persisted.Status != http.StatusOK || !bytes.Contains(persisted.Body, []byte("ЖК Северный")) ||
		bytes.Contains(persisted.Body, []byte("ЖК Переименованный")) {
		t.Fatalf("persisted session snapshot = %d: %s", persisted.Status, persisted.Body)
	}
}

//nolint:funlen // The public journey proves raw S3 provenance and normalized PostgreSQL state together.
func TestColdCurrentSnapshotCollectsSequentiallyIntoADashboard(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 15 * time.Second
	complexes := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/catalog/regions/78/complexes", nil)
	if complexes.Status != http.StatusOK {
		t.Fatalf("prepare complex catalog = %d: %s", complexes.Status, complexes.Body)
	}
	database := openDatabase(t)
	defer database.Close(t.Context())
	createdResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"78"}, "residentialComplexIds": []string{"1001"},
			"segmentCodes": []string{"residential"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-cold-current-session"})
	var created analyticalSessionDetail
	if createdResponse.Status != http.StatusCreated ||
		json.Unmarshal(createdResponse.Body, &created) != nil || created.Status != "collecting" ||
		created.CollectionProgress.CompletedRequests != 0 || created.CollectionProgress.TotalRequests != nil {
		t.Fatalf("cold created = %d: %s", createdResponse.Status, createdResponse.Body)
	}
	if _, err := database.Exec(t.Context(), `
		INSERT INTO eiszhs_collection_step_claim (
			analytical_session_id, step_kind, complex_id, page, claimed_at, expires_at
		) VALUES ($1::uuid, 'complex_membership', '1001', 0, now() - interval '2 minutes',
		          now() - interval '1 minute')`, created.ID); err != nil {
		t.Fatalf("seed expired collection claim: %v", err)
	}
	requestsBefore := fakeCurrentRequestCount(t)
	responses := make(chan jsonResponse, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			responses <- requestJSON(t, admin, http.MethodPost,
				baseURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
		}()
	}
	close(start)
	var first jsonResponse
	statuses := make(map[int]int)
	for range 2 {
		response := <-responses
		statuses[response.Status]++
		if response.Status == http.StatusOK {
			first = response
		}
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusConflict] != 1 ||
		fakeCurrentRequestCount(t)-requestsBefore != 1 {
		t.Fatalf("concurrent collect statuses=%#v current request delta=%d",
			statuses, fakeCurrentRequestCount(t)-requestsBefore)
	}
	var membership analyticalSessionDetail
	if first.Status != http.StatusOK || json.Unmarshal(first.Body, &membership) != nil ||
		membership.Status != "collecting" || membership.CollectionProgress.CompletedRequests != 1 ||
		requestTotal(membership.CollectionProgress.TotalRequests) != 2 {
		t.Fatalf("membership collect = %d: %s", first.Status, first.Body)
	}
	if bytes.Contains(first.Body, []byte("constructionObjectShortInfo")) ||
		bytes.Contains(first.Body, []byte("offline-e2e-token")) {
		t.Fatalf("membership response exposed private source data: %s", first.Body)
	}
	assertRawArtifacts(t, database, created.ID, 1)

	second := requestJSON(t, admin, http.MethodPost,
		baseURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
	var completed analyticalSessionDetail
	if second.Status != http.StatusOK || json.Unmarshal(second.Body, &completed) != nil ||
		completed.Status != "complete" || completed.CollectionProgress.CompletedRequests != 2 ||
		requestTotal(completed.CollectionProgress.TotalRequests) != 2 ||
		completed.CurrentDashboard.SourceLabel != "Текущий срез ЕИСЖС" ||
		completed.CurrentDashboard.CumulativeDDUCount != 17 {
		t.Fatalf("extended collect = %d: %s", second.Status, second.Body)
	}
	if bytes.Contains(second.Body, []byte("dduInfo")) || bytes.Contains(second.Body, []byte("raw")) ||
		bytes.Contains(second.Body, []byte("offline-e2e-token")) {
		t.Fatalf("dashboard response exposed private source data: %s", second.Body)
	}
	assertRawArtifacts(t, database, created.ID, 2)
	var observations, pins int
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM eiszhs_current_observation AS observation
		JOIN eiszhs_raw_artifact AS artifact ON artifact.id = observation.raw_artifact_id
		WHERE artifact.analytical_session_id = $1::uuid`, created.ID).Scan(&observations); err != nil {
		t.Fatalf("count normalized observations: %v", err)
	}
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM analytical_session_observation
		WHERE analytical_session_id = $1::uuid`, created.ID).Scan(&pins); err != nil {
		t.Fatalf("count session observation pins: %v", err)
	}
	if observations != 3 || pins != 1 {
		t.Fatalf("normalized observations=%d pins=%d, want 3 and 1", observations, pins)
	}
	assertAppendOnlyCurrentPersistence(t, database, created.ID)

	repeatedResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"78"}, "residentialComplexIds": []string{"1001"},
			"segmentCodes": []string{"residential"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-repeated-cold-current-session"})
	var repeated analyticalSessionDetail
	if repeatedResponse.Status != http.StatusCreated || json.Unmarshal(repeatedResponse.Body, &repeated) != nil ||
		repeated.ID == created.ID || repeated.Status != "collecting" ||
		repeated.CollectionProgress.CompletedRequests != 0 || repeated.CollectionProgress.TotalRequests != nil {
		t.Fatalf("repeated cold session = %d: %s", repeatedResponse.Status, repeatedResponse.Body)
	}
	var repeatedPins int
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM analytical_session_observation
		WHERE analytical_session_id = $1::uuid`, repeated.ID).Scan(&repeatedPins); err != nil {
		t.Fatalf("count repeated session pins: %v", err)
	}
	original := requestJSON(t, admin, http.MethodGet, baseURL+"/api/v1/sessions/"+created.ID, nil)
	if repeatedPins != 0 || original.Status != http.StatusOK ||
		!bytes.Contains(original.Body, []byte(`"status":"complete"`)) ||
		!bytes.Contains(original.Body, []byte(`"cumulativeDduCount":17`)) {
		t.Fatalf("repeated pins=%d original=%d: %s", repeatedPins, original.Status, original.Body)
	}
}

//nolint:funlen // One public journey proves pagination, deduplication, exact planning, and resume.
func TestCurrentCollectionResumesAllUniqueObjectsAcrossComplexes(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 15 * time.Second
	setFakeCurrentMode(t, "paged-duplicates")
	defer setFakeCurrentMode(t, "healthy")
	complexes := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/catalog/regions/78/complexes", nil)
	if complexes.Status != http.StatusOK {
		t.Fatalf("prepare paged complex catalog = %d: %s", complexes.Status, complexes.Body)
	}
	createdResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"78"}, "residentialComplexIds": []string{"1001", "1002"},
			"segmentCodes": []string{"residential"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-paged-resumable-current-session"})
	var created analyticalSessionDetail
	if createdResponse.Status != http.StatusCreated || json.Unmarshal(createdResponse.Body, &created) != nil ||
		created.Status != "collecting" || created.CollectionProgress.Phase != "discovering_objects" ||
		created.CollectionProgress.TotalRequests != nil || created.CollectionProgress.Percent != nil ||
		created.CollectionProgress.CurrentComplexID != "1001" || created.CollectionProgress.NextActionAt == "" {
		t.Fatalf("paged created = %d: %s", createdResponse.Status, createdResponse.Body)
	}
	requestsBefore := fakeCurrentRequestCount(t)
	first := collectSession(t, admin, baseURL, created.ID)
	if first.CollectionProgress.CompletedRequests != 1 || first.CollectionProgress.DiscoveredTotal != 1 ||
		first.CollectionProgress.TotalRequests != nil || first.CollectionProgress.CurrentComplexID != "1001" {
		t.Fatalf("paged first membership progress=%#v", first.CollectionProgress)
	}
	second := collectSession(t, admin, baseURL, created.ID)
	if second.CollectionProgress.CompletedRequests != 2 || second.CollectionProgress.DiscoveredTotal != 2 ||
		second.CollectionProgress.TotalRequests != nil || second.CollectionProgress.CurrentComplexID != "1002" {
		t.Fatalf("paged second membership progress=%#v", second.CollectionProgress)
	}

	requestsBeforeReload := fakeCurrentRequestCount(t)
	resumedResponse := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/sessions/"+created.ID, nil)
	var resumed analyticalSessionDetail
	if resumedResponse.Status != http.StatusOK || json.Unmarshal(resumedResponse.Body, &resumed) != nil ||
		resumed.CollectionProgress.CompletedRequests != 2 ||
		resumed.CollectionProgress.CurrentComplexID != "1002" ||
		fakeCurrentRequestCount(t) != requestsBeforeReload {
		t.Fatalf("resumed checkpoint = %d: %s", resumedResponse.Status, resumedResponse.Body)
	}

	planned := collectSession(t, admin, baseURL, created.ID)
	if planned.CollectionProgress.Phase != "collecting_objects" ||
		requestTotal(planned.CollectionProgress.TotalRequests) != 6 ||
		planned.CollectionProgress.CompletedRequests != 3 ||
		planned.CollectionProgress.DiscoveredTotal != 3 || planned.CollectionProgress.Remaining != 3 ||
		planned.CollectionProgress.CurrentObjectID != "9001" ||
		planned.CollectionProgress.Percent == nil || *planned.CollectionProgress.Percent != 50 {
		t.Fatalf("exact P + N plan progress=%#v", planned.CollectionProgress)
	}
	for expectedFetched := 1; expectedFetched <= 3; expectedFetched++ {
		planned = collectSession(t, admin, baseURL, created.ID)
		if planned.CollectionProgress.Fetched != expectedFetched {
			t.Fatalf("fetched after object %d progress=%#v", expectedFetched, planned.CollectionProgress)
		}
	}
	if planned.Status != "complete" || planned.CollectionProgress.Phase != "complete" ||
		planned.CollectionProgress.CompletedRequests != 6 || planned.CollectionProgress.Remaining != 0 ||
		planned.CollectionProgress.Cached != 0 || planned.CollectionProgress.Failed != 0 ||
		planned.CollectionProgress.Percent == nil || *planned.CollectionProgress.Percent != 100 ||
		planned.CollectionProgress.CurrentComplexID != "" || planned.CollectionProgress.CurrentObjectID != "" ||
		planned.CollectionProgress.NextActionAt != "" ||
		fakeCurrentRequestCount(t)-requestsBefore != 6 {
		t.Fatalf("completed paged collection requests=%d progress=%#v",
			fakeCurrentRequestCount(t)-requestsBefore, planned.CollectionProgress)
	}
	database := openDatabase(t)
	defer database.Close(t.Context())
	var membershipObjects, plannedObjects, extendedObjects int
	if err := database.QueryRow(t.Context(), `
		SELECT
		  (SELECT count(*) FROM eiszhs_collection_membership_object WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM eiszhs_collection_object WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM eiszhs_collection_extended_checkpoint WHERE analytical_session_id = $1::uuid)`,
		created.ID).Scan(&membershipObjects, &plannedObjects, &extendedObjects); err != nil {
		t.Fatalf("read paged collection persistence: %v", err)
	}
	if membershipObjects != 4 || plannedObjects != 3 || extendedObjects != 3 {
		t.Fatalf("membership objects=%d plan=%d extended=%d, want 4, 3, 3",
			membershipObjects, plannedObjects, extendedObjects)
	}
}

func collectSession(
	t *testing.T,
	client *http.Client,
	baseURL string,
	sessionID string,
) analyticalSessionDetail {
	t.Helper()
	response := requestJSON(t, client, http.MethodPost,
		baseURL+"/api/v1/sessions/"+sessionID+"/collect", nil)
	var session analyticalSessionDetail
	if response.Status != http.StatusOK || json.Unmarshal(response.Body, &session) != nil {
		t.Fatalf("collect session = %d: %s", response.Status, response.Body)
	}
	return session
}

//nolint:funlen // Real locks make the public HTTP read straddle a concurrent checkpoint deterministically.
func TestCurrentCollectionProgressUsesOnePostgreSQLSnapshot(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 15 * time.Second
	complexes := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/catalog/regions/78/complexes", nil)
	if complexes.Status != http.StatusOK {
		t.Fatalf("prepare snapshot complex catalog = %d: %s", complexes.Status, complexes.Body)
	}
	createdResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"78"}, "residentialComplexIds": []string{"1001"},
			"segmentCodes": []string{"residential"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-consistent-progress-snapshot"})
	var created analyticalSessionDetail
	if createdResponse.Status != http.StatusCreated || json.Unmarshal(createdResponse.Body, &created) != nil {
		t.Fatalf("create snapshot session = %d: %s", createdResponse.Status, createdResponse.Body)
	}

	locker := openDatabase(t)
	defer locker.Close(t.Context())
	lockTx, err := locker.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin progress read blocker: %v", err)
	}
	defer func() { _ = lockTx.Rollback(t.Context()) }()
	if _, err := lockTx.Exec(t.Context(),
		"LOCK TABLE eiszhs_collection_object IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock collection objects: %v", err)
	}

	type asynchronousResponse struct {
		status int
		body   []byte
		err    error
	}
	responseChannel := make(chan asynchronousResponse, 1)
	go func() {
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet,
			baseURL+"/api/v1/sessions/"+created.ID, nil)
		if requestErr != nil {
			responseChannel <- asynchronousResponse{err: requestErr}
			return
		}
		response, requestErr := admin.Do(request)
		if requestErr != nil {
			responseChannel <- asynchronousResponse{err: requestErr}
			return
		}
		defer response.Body.Close()
		body, readErr := io.ReadAll(response.Body)
		responseChannel <- asynchronousResponse{status: response.StatusCode, body: body, err: readErr}
	}()

	database := openDatabase(t)
	defer database.Close(t.Context())
	waitForBlockedProgressRead(t, database)
	checkpointProgressDirectly(t, database, created.ID)
	if err := lockTx.Commit(t.Context()); err != nil {
		t.Fatalf("release progress read blocker: %v", err)
	}

	asynchronous := <-responseChannel
	var snapshot analyticalSessionDetail
	if asynchronous.err != nil || asynchronous.status != http.StatusOK ||
		json.Unmarshal(asynchronous.body, &snapshot) != nil {
		t.Fatalf("read concurrent session status=%d error=%v body=%s",
			asynchronous.status, asynchronous.err, asynchronous.body)
	}
	if snapshot.CollectionProgress.CompletedRequests != 0 ||
		snapshot.CollectionProgress.Phase != "discovering_objects" ||
		snapshot.CollectionProgress.CurrentComplexID != "1001" {
		t.Fatalf("torn progress snapshot: %#v", snapshot.CollectionProgress)
	}
	currentResponse := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/sessions/"+created.ID, nil)
	var current analyticalSessionDetail
	if currentResponse.Status != http.StatusOK || json.Unmarshal(currentResponse.Body, &current) != nil ||
		current.CollectionProgress.CompletedRequests != 1 ||
		current.CollectionProgress.Phase != "collecting_objects" {
		t.Fatalf("checkpoint was not published after snapshot: %d %s", currentResponse.Status, currentResponse.Body)
	}
}

func waitForBlockedProgressRead(t *testing.T, database *pgx.Conn) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		err := database.QueryRow(t.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE wait_event_type = 'Lock'
				  AND query LIKE '%eiszhs_collection_object%'
			)`).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe blocked progress read: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("progress read did not reach the locked collection table")
}

func checkpointProgressDirectly(t *testing.T, database *pgx.Conn, sessionID string) {
	t.Helper()
	tx, err := database.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin concurrent checkpoint: %v", err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	var artifactID string
	err = tx.QueryRow(t.Context(), `
		INSERT INTO eiszhs_raw_artifact (
			analytical_session_id, step_kind, complex_id, page, object_id,
			object_key, sha256, byte_size, content_type, source_address, observed_at
		) VALUES ($1::uuid, 'complex_membership', '1001', 0, NULL,
			$2, repeat('0', 64), 0, 'application/json', 'e2e://snapshot', now())
		RETURNING id::text`, sessionID, "e2e/progress-snapshot/"+uuid.NewString()+".json").Scan(&artifactID)
	if err != nil {
		t.Fatalf("insert concurrent raw artifact: %v", err)
	}
	if _, err := tx.Exec(t.Context(), `
		INSERT INTO eiszhs_collection_membership_checkpoint (
			analytical_session_id, complex_id, page, total, page_size,
			object_ids, raw_artifact_id, completed_at
		) VALUES ($1::uuid, '1001', 0, 0, 50, '{}'::text[], $2::uuid, now())`,
		sessionID, artifactID); err != nil {
		t.Fatalf("write concurrent membership checkpoint: %v", err)
	}
	if _, err := tx.Exec(t.Context(), `
		UPDATE analytical_session
		SET collection_completed_requests = 1, collection_next_action_at = now()
		WHERE id = $1::uuid`, sessionID); err != nil {
		t.Fatalf("publish concurrent checkpoint progress: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit concurrent checkpoint: %v", err)
	}
}

//nolint:funlen // A blocked public request across two replicas proves lease renewal and fencing.
func TestCurrentCollectionRenewsClaimAcrossReplicasWhileSourceIsBlocked(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	replicaURL := valueOr("E2E_REPLICA_BASE_URL", "http://app-replica:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 30 * time.Second
	replicaAdmin := authenticatedClient(t, replicaURL, "admin", "Reliable analytics access 2026")
	replicaAdmin.Timeout = 30 * time.Second
	complexes := requestJSON(t, admin, http.MethodGet,
		baseURL+"/api/v1/catalog/regions/77/complexes", nil)
	if complexes.Status != http.StatusOK {
		t.Fatalf("prepare lease complex catalog = %d: %s", complexes.Status, complexes.Body)
	}
	createdResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"77"}, "residentialComplexIds": []string{"1001"},
			"segmentCodes": []string{"parking"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-renewed-current-claim"})
	var created analyticalSessionDetail
	if createdResponse.Status != http.StatusCreated || json.Unmarshal(createdResponse.Body, &created) != nil ||
		created.Status != "collecting" {
		t.Fatalf("create renewed-claim session = %d: %s", createdResponse.Status, createdResponse.Body)
	}
	setFakeCurrentBlock(t, true)
	defer setFakeCurrentBlock(t, false)
	requestsBefore := fakeCurrentStats(t).CurrentRequests
	type asyncResult struct {
		response jsonResponse
		err      error
	}
	firstResult := make(chan asyncResult, 1)
	go func() {
		response, err := requestJSONAsync(t.Context(), admin, http.MethodPost,
			baseURL+"/api/v1/sessions/"+created.ID+"/collect")
		firstResult <- asyncResult{response: response, err: err}
	}()
	waitForFakeCurrentInFlight(t, 1)
	time.Sleep(900 * time.Millisecond)
	second := requestJSON(t, replicaAdmin, http.MethodPost,
		replicaURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
	if second.Status != http.StatusConflict || fakeCurrentStats(t).CurrentRequests-requestsBefore != 1 {
		t.Fatalf("replica collect = %d: %s, current request delta=%d",
			second.Status, second.Body, fakeCurrentStats(t).CurrentRequests-requestsBefore)
	}
	setFakeCurrentBlock(t, false)
	firstAsync := <-firstResult
	if firstAsync.err != nil {
		t.Fatalf("first renewed collect: %v", firstAsync.err)
	}
	first := firstAsync.response
	var membership analyticalSessionDetail
	if first.Status != http.StatusOK || json.Unmarshal(first.Body, &membership) != nil ||
		membership.Status != "collecting" || membership.CollectionProgress.CompletedRequests != 1 ||
		requestTotal(membership.CollectionProgress.TotalRequests) != 2 {
		t.Fatalf("renewed first collect = %d: %s", first.Status, first.Body)
	}
	database := openDatabase(t)
	defer database.Close(t.Context())
	var checkpoints, claims int
	if err := database.QueryRow(t.Context(), `
		SELECT
		  (SELECT count(*) FROM eiszhs_collection_membership_checkpoint WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM eiszhs_collection_step_claim WHERE analytical_session_id = $1::uuid)`,
		created.ID).Scan(&checkpoints, &claims); err != nil {
		t.Fatalf("read renewed claim persistence: %v", err)
	}
	if checkpoints != 1 || claims != 0 || fakeCurrentStats(t).CurrentRequests-requestsBefore != 1 {
		t.Fatalf("renewed claim checkpoints=%d claims=%d current request delta=%d",
			checkpoints, claims, fakeCurrentStats(t).CurrentRequests-requestsBefore)
	}
}

func assertAppendOnlyCurrentPersistence(t *testing.T, database *pgx.Conn, sessionID string) {
	t.Helper()
	mutations := []string{
		`UPDATE eiszhs_raw_artifact SET sha256 = sha256 WHERE analytical_session_id = $1::uuid`,
		`DELETE FROM eiszhs_raw_artifact WHERE analytical_session_id = $1::uuid`,
		`UPDATE eiszhs_collection_membership_checkpoint SET total = total WHERE analytical_session_id = $1::uuid`,
		`DELETE FROM eiszhs_collection_membership_checkpoint WHERE analytical_session_id = $1::uuid`,
		`UPDATE eiszhs_collection_membership_object SET object_id = object_id
		 WHERE analytical_session_id = $1::uuid`,
		`DELETE FROM eiszhs_collection_membership_object WHERE analytical_session_id = $1::uuid`,
		`UPDATE eiszhs_collection_extended_checkpoint SET completed_at = completed_at WHERE analytical_session_id = $1::uuid`,
		`DELETE FROM eiszhs_collection_extended_checkpoint WHERE analytical_session_id = $1::uuid`,
		`UPDATE eiszhs_current_observation SET cumulative_ddu_count = cumulative_ddu_count
		 WHERE raw_artifact_id IN (
		     SELECT id FROM eiszhs_raw_artifact WHERE analytical_session_id = $1::uuid
		 )`,
		`DELETE FROM eiszhs_current_observation
		 WHERE raw_artifact_id IN (
		     SELECT id FROM eiszhs_raw_artifact WHERE analytical_session_id = $1::uuid
		 )`,
	}
	for _, statement := range mutations {
		tx, err := database.Begin(t.Context())
		if err != nil {
			t.Fatalf("begin append-only mutation: %v", err)
		}
		_, mutationErr := tx.Exec(t.Context(), statement, sessionID)
		_ = tx.Rollback(t.Context())
		if mutationErr == nil || !strings.Contains(mutationErr.Error(), "append-only") {
			t.Fatalf("mutation was not rejected as append-only: %s error=%v", statement, mutationErr)
		}
	}
}

func TestIncompleteMembershipPageCannotCompleteDiscovery(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 15 * time.Second
	setFakeCurrentMode(t, "overlap")
	defer setFakeCurrentMode(t, "healthy")
	createdResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"78"}, "residentialComplexIds": []string{"1002"},
			"segmentCodes": []string{"non_residential"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-overlap-current-session"})
	var created analyticalSessionDetail
	if createdResponse.Status != http.StatusCreated || json.Unmarshal(createdResponse.Body, &created) != nil {
		t.Fatalf("create overlap session = %d: %s", createdResponse.Status, createdResponse.Body)
	}
	first := requestJSON(t, admin, http.MethodPost,
		baseURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
	if first.Status != http.StatusInternalServerError {
		t.Fatalf("incomplete membership page = %d: %s, want rejected checkpoint", first.Status, first.Body)
	}
	database := openDatabase(t)
	defer database.Close(t.Context())
	var checkpoints, objects int
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM eiszhs_collection_membership_checkpoint
		WHERE analytical_session_id = $1::uuid`, created.ID).Scan(&checkpoints); err != nil {
		t.Fatalf("count overlap checkpoints: %v", err)
	}
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM eiszhs_collection_object
		WHERE analytical_session_id = $1::uuid`, created.ID).Scan(&objects); err != nil {
		t.Fatalf("count overlap objects: %v", err)
	}
	persisted := requestJSON(t, admin, http.MethodGet, baseURL+"/api/v1/sessions/"+created.ID, nil)
	if checkpoints != 0 || objects != 0 || persisted.Status != http.StatusOK ||
		!bytes.Contains(persisted.Body, []byte(`"status":"collecting"`)) {
		t.Fatalf("incomplete checkpoints=%d objects=%d session=%d: %s",
			checkpoints, objects, persisted.Status, persisted.Body)
	}
}

func TestIncompleteExtendedPayloadCannotPersistOrCompleteSession(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 15 * time.Second
	setFakeCurrentMode(t, "healthy")
	createdResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"78"}, "residentialComplexIds": []string{"1002"},
			"segmentCodes": []string{"parking"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-incomplete-current-session"})
	var created analyticalSessionDetail
	if createdResponse.Status != http.StatusCreated || json.Unmarshal(createdResponse.Body, &created) != nil {
		t.Fatalf("create incomplete session = %d: %s", createdResponse.Status, createdResponse.Body)
	}
	membership := requestJSON(t, admin, http.MethodPost,
		baseURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
	if membership.Status != http.StatusOK {
		t.Fatalf("incomplete membership = %d: %s", membership.Status, membership.Body)
	}
	setFakeCurrentMode(t, "incomplete")
	defer setFakeCurrentMode(t, "healthy")
	extended := requestJSON(t, admin, http.MethodPost,
		baseURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
	if extended.Status != http.StatusInternalServerError {
		t.Fatalf("incomplete extended = %d: %s", extended.Status, extended.Body)
	}
	database := openDatabase(t)
	defer database.Close(t.Context())
	var extendedCheckpoints, observations, claims int
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM eiszhs_collection_extended_checkpoint
		WHERE analytical_session_id = $1::uuid`, created.ID).Scan(&extendedCheckpoints); err != nil {
		t.Fatalf("count incomplete checkpoints: %v", err)
	}
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM eiszhs_current_observation AS observation
		JOIN eiszhs_raw_artifact AS artifact ON artifact.id = observation.raw_artifact_id
		WHERE artifact.analytical_session_id = $1::uuid`, created.ID).Scan(&observations); err != nil {
		t.Fatalf("count incomplete observations: %v", err)
	}
	if err := database.QueryRow(t.Context(), `
		SELECT count(*) FROM eiszhs_collection_step_claim
		WHERE analytical_session_id = $1::uuid`, created.ID).Scan(&claims); err != nil {
		t.Fatalf("count incomplete claims: %v", err)
	}
	persisted := requestJSON(t, admin, http.MethodGet, baseURL+"/api/v1/sessions/"+created.ID, nil)
	if extendedCheckpoints != 0 || observations != 0 || claims != 0 || persisted.Status != http.StatusOK ||
		!bytes.Contains(persisted.Body, []byte(`"status":"collecting"`)) {
		t.Fatalf("incomplete checkpoints=%d observations=%d claims=%d session=%d: %s",
			extendedCheckpoints, observations, claims, persisted.Status, persisted.Body)
	}
	setFakeCurrentMode(t, "healthy")
	retry := requestJSON(t, admin, http.MethodPost,
		baseURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
	if retry.Status != http.StatusOK || !bytes.Contains(retry.Body, []byte(`"status":"complete"`)) {
		t.Fatalf("healthy retry after incomplete payload = %d: %s", retry.Status, retry.Body)
	}
}

func TestRawStorageFailurePrecedesAllNormalizedPersistence(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	admin.Timeout = 15 * time.Second
	setFakeCurrentMode(t, "healthy")
	createdResponse := requestJSONWithHeaders(t, admin, http.MethodPost, baseURL+"/api/v1/sessions",
		map[string]any{
			"regionCodes": []string{"77"}, "residentialComplexIds": []string{"1002"},
			"segmentCodes": []string{"non_residential"},
		}, map[string]string{"Idempotency-Key": "backend-e2e-raw-failure-current-session"})
	var created analyticalSessionDetail
	if createdResponse.Status != http.StatusCreated || json.Unmarshal(createdResponse.Body, &created) != nil {
		t.Fatalf("create raw-failure session = %d: %s", createdResponse.Status, createdResponse.Body)
	}
	if created.Status != "collecting" {
		t.Fatalf("raw-failure session is unexpectedly warm: %s", createdResponse.Body)
	}
	conflictingKey := seedConflictingMembershipArtifact(t, created.ID, "1002")
	defer deleteS3Object(t, conflictingKey)
	collect := requestJSON(t, admin, http.MethodPost,
		baseURL+"/api/v1/sessions/"+created.ID+"/collect", nil)
	if collect.Status != http.StatusInternalServerError {
		t.Fatalf("raw storage failure collect = %d: %s", collect.Status, collect.Body)
	}
	database := openDatabase(t)
	defer database.Close(t.Context())
	var rawArtifacts, membershipCheckpoints, objects, observations, claims int
	row := database.QueryRow(t.Context(), `
		SELECT
		  (SELECT count(*) FROM eiszhs_raw_artifact WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM eiszhs_collection_membership_checkpoint WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM eiszhs_collection_object WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM analytical_session_observation WHERE analytical_session_id = $1::uuid),
		  (SELECT count(*) FROM eiszhs_collection_step_claim WHERE analytical_session_id = $1::uuid)`, created.ID)
	if err := row.Scan(&rawArtifacts, &membershipCheckpoints, &objects, &observations, &claims); err != nil {
		t.Fatalf("read raw-failure persistence: %v", err)
	}
	if rawArtifacts != 0 || membershipCheckpoints != 0 || objects != 0 || observations != 0 || claims != 0 {
		t.Fatalf("raw failure persisted raw=%d checkpoints=%d objects=%d observations=%d claims=%d",
			rawArtifacts, membershipCheckpoints, objects, observations, claims)
	}
}

func seedConflictingMembershipArtifact(t *testing.T, sessionID string, complexID string) string {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://fake-eiszhs:8090/complexes/"+complexID+"/construction-objects?page=0&size=50", nil)
	if err != nil {
		t.Fatalf("create source fixture request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("read source fixture: %v", err)
	}
	raw, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("source fixture status=%d error=%v", response.StatusCode, readErr)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(raw))
	key := fmt.Sprintf("eiszhs/sessions/%s/complexes/%s/pages/0/%s.json",
		sessionID, complexID, checksum)
	put, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		"http://s3mock:9090/analytics/"+key, strings.NewReader("conflicting raw bytes"))
	if err != nil {
		t.Fatalf("create conflicting raw artifact: %v", err)
	}
	put.Header.Set("Content-Type", "application/json")
	putResponse, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("seed conflicting raw artifact: %v", err)
	}
	defer putResponse.Body.Close()
	if putResponse.StatusCode != http.StatusOK {
		t.Fatalf("seed conflicting raw artifact status=%d", putResponse.StatusCode)
	}
	return key
}

func deleteS3Object(t *testing.T, key string) {
	t.Helper()
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodDelete, "http://s3mock:9090/analytics/"+key, nil,
	)
	if err != nil {
		t.Errorf("create raw cleanup: %v", err)
		return
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Errorf("delete raw cleanup: %v", err)
		return
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("delete raw cleanup status=%d", response.StatusCode)
	}
}

type fakeCurrentStatistics struct {
	CurrentRequests int64 `json:"currentRequests"`
	CurrentInFlight int64 `json:"currentInFlight"`
}

func fakeCurrentRequestCount(t *testing.T) int64 {
	return fakeCurrentStats(t).CurrentRequests
}

func fakeCurrentStats(t *testing.T) fakeCurrentStatistics {
	t.Helper()
	request, err := http.NewRequestWithContext(
		t.Context(), http.MethodGet, "http://fake-eiszhs:8090/admin/stats", nil,
	)
	if err != nil {
		t.Fatalf("create fake ЕИСЖС stats request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("read fake ЕИСЖС stats: %v", err)
	}
	defer response.Body.Close()
	var stats fakeCurrentStatistics
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&stats) != nil {
		t.Fatalf("fake ЕИСЖС stats status=%d", response.StatusCode)
	}
	return stats
}

func waitForFakeCurrentInFlight(t *testing.T, expected int64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if fakeCurrentStats(t).CurrentInFlight == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fake current in-flight=%d, want %d", fakeCurrentStats(t).CurrentInFlight, expected)
}

func requestJSONAsync(
	ctx context.Context,
	client *http.Client,
	method string,
	url string,
) (jsonResponse, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return jsonResponse{}, fmt.Errorf("create %s %s: %w", method, url, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return jsonResponse{}, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return jsonResponse{}, fmt.Errorf("read %s %s: %w", method, url, err)
	}
	return jsonResponse{Status: response.StatusCode, Header: response.Header, Body: data}, nil
}

func setFakeCurrentBlock(t *testing.T, blocked bool) {
	t.Helper()
	value := "released"
	if blocked {
		value = "blocked"
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://fake-eiszhs:8090/admin/current-block?value="+value, nil)
	if err != nil {
		t.Fatalf("create fake ЕИСЖС current block request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("set fake ЕИСЖС current block: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("set fake ЕИСЖС current block status=%d", response.StatusCode)
	}
}

func setFakeCurrentMode(t *testing.T, mode string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://fake-eiszhs:8090/admin/current-mode?value="+mode, nil)
	if err != nil {
		t.Fatalf("create fake ЕИСЖС mode request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("set fake ЕИСЖС mode: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("set fake ЕИСЖС mode status=%d", response.StatusCode)
	}
}

func assertRawArtifacts(t *testing.T, database *pgx.Conn, sessionID string, expected int) {
	t.Helper()
	rows, err := database.Query(t.Context(), `
		SELECT object_key, sha256, source_address FROM eiszhs_raw_artifact
		WHERE analytical_session_id = $1::uuid ORDER BY object_key`, sessionID)
	if err != nil {
		t.Fatalf("query raw artifacts: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var key, expectedSHA, sourceAddress string
		if err := rows.Scan(&key, &expectedSHA, &sourceAddress); err != nil {
			t.Fatalf("scan raw artifact: %v", err)
		}
		if !strings.HasPrefix(sourceAddress, "http://fake-eiszhs:8090/") ||
			strings.Contains(sourceAddress, "token") {
			t.Fatalf("raw artifact source address = %q", sourceAddress)
		}
		request, err := http.NewRequestWithContext(
			t.Context(), http.MethodGet, "http://s3mock:9090/analytics/"+key, nil,
		)
		if err != nil {
			t.Fatalf("create raw artifact request: %v", err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("GET raw artifact: %v", err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		actualSHA := fmt.Sprintf("%x", sha256.Sum256(body))
		if readErr != nil || response.StatusCode != http.StatusOK || actualSHA != expectedSHA ||
			!strings.Contains(key, expectedSHA) {
			t.Fatalf("raw artifact %q status=%d sha=%q/%q error=%v",
				key, response.StatusCode, actualSHA, expectedSHA, readErr)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate raw artifacts: %v", err)
	}
	if count != expected {
		t.Fatalf("raw artifacts=%d, want %d", count, expected)
	}
}

type analyticalSessionDetail struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Filters struct {
		RegionCodes []string  `json:"regionCodes"`
		Period      *struct{} `json:"period"`
	} `json:"filters"`
	ObservedWindow struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"observedWindow"`
	CollectionProgress struct {
		Phase             string `json:"phase"`
		CompletedRequests int    `json:"completedRequests"`
		TotalRequests     *int   `json:"totalRequests"`
		DiscoveredTotal   int    `json:"discoveredTotal"`
		Cached            int    `json:"cached"`
		Fetched           int    `json:"fetched"`
		Failed            int    `json:"failed"`
		Remaining         int    `json:"remaining"`
		CurrentComplexID  string `json:"currentComplexId"`
		CurrentObjectID   string `json:"currentObjectId"`
		NextActionAt      string `json:"nextActionAt"`
		Percent           *int   `json:"percent"`
	} `json:"collectionProgress"`
	CurrentDashboard struct {
		SourceLabel        string `json:"sourceLabel"`
		CumulativeDDUCount int64  `json:"cumulativeDduCount"`
	} `json:"currentEiszhsDashboard"`
}

func requestTotal(total *int) int {
	if total == nil {
		return -1
	}
	return *total
}

func TestAuthenticatedAccountAndSessionLifecycle(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	credential := createEmployee(t, admin, baseURL, "api.employee")
	employee := authenticatedClient(t, baseURL, "api.employee", credential.TemporaryPassword)
	assertStatusCode(t, employee, http.MethodGet, baseURL+"/api/v1/sessions", nil, http.StatusUnauthorized)
	assertStatusCode(t, employee, http.MethodPost, baseURL+"/api/v1/auth/change-password",
		map[string]string{"password": "Reliable API employee access 2026"}, http.StatusOK)
	assertStatusCode(t, employee, http.MethodGet, baseURL+"/api/v1/employees", nil, http.StatusForbidden)

	secondSession := authenticatedClient(t, baseURL, "api.employee", "Reliable API employee access 2026")
	assertStatusCode(t, admin, http.MethodPost,
		baseURL+"/api/v1/employees/"+credential.Employee.ID+"/revoke-sessions", nil, http.StatusNoContent)
	assertStatusCode(t, employee, http.MethodGet, baseURL+"/api/v1/me", nil, http.StatusUnauthorized)
	assertStatusCode(t, secondSession, http.MethodGet, baseURL+"/api/v1/me", nil, http.StatusUnauthorized)
}

func TestExpiredSessionsAndTemporaryPasswordsAreRejected(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	admin := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	credential := createEmployee(t, admin, baseURL, "expired.employee")
	database := openDatabase(t)
	defer database.Close(t.Context())
	if _, err := database.Exec(t.Context(), `
		UPDATE user_account SET temporary_password_expires_at = now() - interval '1 minute'
		WHERE id = $1::uuid`, credential.Employee.ID); err != nil {
		t.Fatalf("expire temporary password: %v", err)
	}
	login := requestJSON(t, &http.Client{Timeout: 5 * time.Second}, http.MethodPost,
		baseURL+"/api/v1/auth/login", map[string]string{
			"login": "expired.employee", "password": credential.TemporaryPassword,
		})
	if login.Status != http.StatusUnauthorized {
		t.Fatalf("expired temporary password login = %d: %s", login.Status, login.Body)
	}
	expireClientSession(t, database, admin, baseURL)
	assertStatusCode(t, admin, http.MethodGet, baseURL+"/api/v1/me", nil, http.StatusUnauthorized)
	assertAuditExcludesSecrets(t, database, credential.TemporaryPassword)
}

func TestLogoutRevokesOnlyTheCurrentSession(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	first := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	second := authenticatedClient(t, baseURL, "admin", "Reliable analytics access 2026")
	assertStatusCode(t, first, http.MethodPost, baseURL+"/api/v1/auth/logout", nil, http.StatusNoContent)
	assertStatusCode(t, first, http.MethodGet, baseURL+"/api/v1/me", nil, http.StatusUnauthorized)
	assertStatusCode(t, second, http.MethodGet, baseURL+"/api/v1/me", nil, http.StatusOK)
}

func TestLoginFailuresDoNotRevealWhetherAnAccountExists(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	client := &http.Client{Timeout: 5 * time.Second}
	wrongPassword := requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login",
		map[string]string{"login": "admin", "password": "wrong password"})
	unknownAccount := requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login",
		map[string]string{"login": "unknown.user", "password": "wrong password"})
	if wrongPassword.Status != http.StatusUnauthorized || unknownAccount.Status != http.StatusUnauthorized ||
		!bytes.Equal(wrongPassword.Body, unknownAccount.Body) {
		t.Fatalf("login failures differ: wrong=%d %s unknown=%d %s",
			wrongPassword.Status, wrongPassword.Body, unknownAccount.Status, unknownAccount.Body)
	}
}

func TestLoginAttemptsAreRateLimitedByLoginAndClientAddress(t *testing.T) {
	requireAuthenticatedPhase(t)
	baseURL := valueOr("E2E_BASE_URL", "http://localhost:8080")
	client := &http.Client{Timeout: 5 * time.Second}
	database := openDatabase(t)
	defer database.Close(t.Context())
	clearLoginThrottles(t, database)
	defer clearLoginThrottles(t, database)

	for attempt := 0; attempt < 2; attempt++ {
		response := requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login",
			map[string]string{"login": "rate.login", "password": "wrong password"})
		if response.Status != http.StatusUnauthorized {
			t.Fatalf("login throttle attempt %d = %d, want 401", attempt+1, response.Status)
		}
	}
	assertRateLimited(t, requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login",
		map[string]string{"login": "rate.login", "password": "wrong password"}))

	clearLoginThrottles(t, database)
	for attempt := 0; attempt < 10; attempt++ {
		response := requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login",
			map[string]string{
				"login": fmt.Sprintf("rate.ip.%d", attempt), "password": "wrong password",
			})
		if response.Status != http.StatusUnauthorized {
			t.Fatalf("address throttle attempt %d = %d, want 401", attempt+1, response.Status)
		}
	}
	assertRateLimited(t, requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login",
		map[string]string{"login": "rate.ip.blocked", "password": "wrong password"}))
}

func clearLoginThrottles(t *testing.T, database *pgx.Conn) {
	t.Helper()
	if _, err := database.Exec(t.Context(), `DELETE FROM login_throttle`); err != nil {
		t.Fatalf("clear login throttles: %v", err)
	}
}

func assertRateLimited(t *testing.T, response jsonResponse) {
	t.Helper()
	if response.Status != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("rate-limited login = %d Retry-After %q: %s",
			response.Status, response.Header.Get("Retry-After"), response.Body)
	}
}

type temporaryCredential struct {
	Employee struct {
		ID string `json:"id"`
	} `json:"employee"`
	TemporaryPassword string `json:"temporaryPassword"`
}

func createEmployee(
	t *testing.T,
	client *http.Client,
	baseURL string,
	login string,
) temporaryCredential {
	t.Helper()
	response := requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/employees", map[string]string{
		"login": login, "displayName": "API Employee", "role": "employee",
	})
	if response.Status != http.StatusCreated || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("create employee = %d Cache-Control %q: %s",
			response.Status, response.Header.Get("Cache-Control"), response.Body)
	}
	var credential temporaryCredential
	if err := json.Unmarshal(response.Body, &credential); err != nil {
		t.Fatalf("decode temporary credential: %v", err)
	}
	return credential
}

func openDatabase(t *testing.T) *pgx.Conn {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for authenticated E2E tests")
	}
	connection, err := pgx.Connect(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("connect to E2E PostgreSQL: %v", err)
	}
	return connection
}

func expireClientSession(
	t *testing.T,
	database *pgx.Conn,
	client *http.Client,
	baseURL string,
) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL, nil)
	if err != nil {
		t.Fatalf("create cookie request: %v", err)
	}
	var rawToken string
	for _, cookie := range client.Jar.Cookies(request.URL) {
		if cookie.Name == "ad_session" {
			rawToken = cookie.Value
		}
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(rawToken)))
	result, err := database.Exec(t.Context(), `
		UPDATE user_session SET idle_expires_at = now() - interval '1 minute'
		WHERE token_hash = $1`, digest)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("expire server-side session: affected=%d err=%v", result.RowsAffected(), err)
	}
}

func assertAuditExcludesSecrets(t *testing.T, database *pgx.Conn, temporaryPassword string) {
	t.Helper()
	for _, secret := range []string{
		"Bootstrap-Admin-2026!",
		"Reliable analytics access 2026",
		"Reliable API employee access 2026",
		temporaryPassword,
	} {
		var leaked bool
		if err := database.QueryRow(t.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM audit_event WHERE position($1 in metadata::text) > 0
			)`, secret).Scan(&leaked); err != nil {
			t.Fatalf("inspect access audit metadata: %v", err)
		}
		if leaked {
			t.Fatalf("access audit metadata contains a credential value")
		}
	}
}

func authenticatedClient(t *testing.T, baseURL string, login string, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	response := requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/login",
		map[string]string{"login": login, "password": password})
	if response.Status != http.StatusOK {
		t.Fatalf("login %s = %d: %s", login, response.Status, response.Body)
	}
	setCookie := response.Header.Get("Set-Cookie")
	if !strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie, "SameSite=Lax") {
		t.Fatalf("login cookie lacks security attributes: %q", setCookie)
	}
	return client
}

func assertStatusCode(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body any,
	want int,
) {
	t.Helper()
	response := requestJSON(t, client, method, url, body)
	if response.Status != want {
		t.Fatalf("%s %s = %d: %s, want %d", method, url, response.Status, response.Body, want)
	}
}

type jsonResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

func requestJSON(t *testing.T, client *http.Client, method string, url string, body any) jsonResponse {
	return requestJSONWithHeaders(t, client, method, url, body, nil)
}

func requestJSONWithHeaders(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body any,
	headers map[string]string,
) jsonResponse {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode %s %s: %v", method, url, err)
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, url, payload)
	if err != nil {
		t.Fatalf("create %s %s: %v", method, url, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, url, err)
	}
	return jsonResponse{Status: response.StatusCode, Header: response.Header, Body: data}
}

func requireAuthenticatedPhase(t *testing.T) {
	t.Helper()
	if os.Getenv("E2E_AUTHENTICATED") != "true" {
		t.Skip("authenticated phase runs after browser bootstrap")
	}
}

func getJSON(t *testing.T, client *http.Client, url string) map[string]string {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, response.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode GET %s: %v", url, err)
	}
	return body
}

func valueOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

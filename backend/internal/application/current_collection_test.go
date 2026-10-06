package application_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

//nolint:funlen // One public-seam scenario keeps source, raw storage, and checkpoint assertions together.
func TestCollectCurrentSnapshotCheckpointsOneMembershipRequest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	repository := &memoryCurrentCollectionRepository{plan: application.CurrentCollectionPlan{
		Session: application.AnalyticalSession{
			ID: "01996c43-6c00-7abc-8def-1234567890ab", Status: "collecting",
		},
		Progress: application.CollectionProgress{CompletedRequests: 0},
		Next: &application.CurrentCollectionStep{
			Kind: application.CollectionStepComplexMembership, ComplexID: "1001", Page: 0,
		},
	}}
	source := &memoryCurrentSource{membership: application.CurrentSourceResponse{
		Body:        []byte(`{"total":1,"limitItems":50,"elements":{"constructionObjectShortInfo":[{"objectId":2001}]}}`),
		ContentType: "application/json",
	}}
	storage := &recordingRawStorage{}
	service := application.NewCurrentCollectionService(
		activeSessionIdentity(), repository, source, memoryCurrentNormalizer{
			membership: application.CurrentMembership{
				Total: 1, PageSize: 50, ObjectIDs: []string{"2001"},
			},
		}, storage,
		func() time.Time { return now },
	)

	session, progress, err := service.Collect(
		t.Context(), "cookie", "01996c43-6c00-7abc-8def-1234567890ab",
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if session.Status != "collecting" || progress.CompletedRequests != 1 || progressTotal(progress) != 2 {
		t.Fatalf("session=%#v progress=%#v", session, progress)
	}
	if source.membershipCalls != 1 || source.extendedCalls != 0 {
		t.Fatalf("source calls membership=%d extended=%d", source.membershipCalls, source.extendedCalls)
	}
	if storage.saved.Key != "eiszhs/sessions/01996c43-6c00-7abc-8def-1234567890ab/complexes/1001/pages/0/ed0ee2c9e214d4a651fd8343818664716a54cf3456a7f85d8480b699a02f8036.json" ||
		storage.saved.SHA256 != "ed0ee2c9e214d4a651fd8343818664716a54cf3456a7f85d8480b699a02f8036" {
		t.Fatalf("saved raw = %#v", storage.saved)
	}
	if repository.membership.ObjectIDs[0] != "2001" ||
		repository.membership.RawArtifact.Key != storage.saved.Key {
		t.Fatalf("membership checkpoint = %#v", repository.membership)
	}
}

//nolint:funlen,gocyclo // One public-seam scenario verifies every supported source category together.
func TestCollectCurrentSnapshotStoresAllCategoriesAndCompletesDashboard(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 13, 6, 0, 0, time.UTC)
	count := int64(17)
	repository := &memoryCurrentCollectionRepository{plan: application.CurrentCollectionPlan{
		Session: application.AnalyticalSession{
			ID: "01996c49-ea40-7abc-8def-1234567890ab", Status: "collecting",
		},
		Progress: application.CollectionProgress{CompletedRequests: 1, TotalRequests: intPointer(2)},
		Next: &application.CurrentCollectionStep{
			Kind: application.CollectionStepCurrentExtended, ComplexID: "1001", ObjectID: "2001",
		},
	}}
	repository.extendedResult = application.AnalyticalSession{
		ID: "01996c49-ea40-7abc-8def-1234567890ab", Status: "complete",
		CurrentDashboard: &application.CurrentDashboard{
			SourceLabel: "Текущий срез ЕИСЖС", CumulativeDDUCount: &count,
		},
	}
	source := &memoryCurrentSource{extended: application.CurrentSourceResponse{
		Body:          []byte(`{"objectId":2001,"dduInfo":{"dduLivNumber":17,"dduNonlivNumber":4,"dduParkingNumber":2}}`),
		ContentType:   "application/json; charset=utf-8",
		SourceAddress: "https://xn--f1abdax.xn--p1ai/construction-objects/2001/extended",
	}}
	storage := &recordingRawStorage{}
	service := application.NewCurrentCollectionService(
		activeSessionIdentity(), repository, source, memoryCurrentNormalizer{metrics: []application.CurrentCategoryMetrics{
			{SegmentCode: "residential", CumulativeDDUCount: 17},
			{SegmentCode: "non_residential", CumulativeDDUCount: 4},
			{SegmentCode: "parking", CumulativeDDUCount: 2},
		}}, storage,
		func() time.Time { return now },
	)

	session, progress, err := service.Collect(
		t.Context(), "cookie", "01996c49-ea40-7abc-8def-1234567890ab",
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if session.Status != "complete" || progress.CompletedRequests != 2 || progressTotal(progress) != 2 ||
		session.CurrentDashboard == nil || session.CurrentDashboard.CumulativeDDUCount == nil ||
		*session.CurrentDashboard.CumulativeDDUCount != 17 {
		t.Fatalf("session=%#v progress=%#v", session, progress)
	}
	if source.membershipCalls != 0 || source.extendedCalls != 1 {
		t.Fatalf("source calls membership=%d extended=%d", source.membershipCalls, source.extendedCalls)
	}
	if len(repository.extended.Metrics) != 3 ||
		repository.extended.Metrics[0] != (application.CurrentCategoryMetrics{
			SegmentCode: "residential", CumulativeDDUCount: 17,
		}) ||
		repository.extended.Metrics[1].CumulativeDDUCount != 4 ||
		repository.extended.Metrics[2].CumulativeDDUCount != 2 {
		t.Fatalf("extended checkpoint = %#v", repository.extended)
	}
	if repository.extended.RawArtifact.Key != storage.saved.Key ||
		storage.saved.ContentType != "application/json" ||
		storage.saved.SourceAddress != source.extended.SourceAddress {
		t.Fatalf("raw artifact=%#v checkpoint=%#v", storage.saved, repository.extended)
	}
}

func TestCollectCurrentSnapshotRejectsIncompletePayloadAfterRawStorage(t *testing.T) {
	t.Parallel()
	repository := &memoryCurrentCollectionRepository{plan: application.CurrentCollectionPlan{
		Session: application.AnalyticalSession{
			ID: "01996c49-ea40-7abc-8def-1234567890ac", Status: "collecting",
		},
		Progress: application.CollectionProgress{CompletedRequests: 1, TotalRequests: intPointer(2)},
		Next: &application.CurrentCollectionStep{
			Kind: application.CollectionStepCurrentExtended, ComplexID: "1001", ObjectID: "2001",
		},
	}}
	source := &memoryCurrentSource{extended: application.CurrentSourceResponse{
		Body: []byte(`{"objectId":2001,"dduInfo":{"dduLivNumber":17}}`), ContentType: "application/json",
	}}
	storage := &recordingRawStorage{}
	service := application.NewCurrentCollectionService(
		activeSessionIdentity(), repository, source, memoryCurrentNormalizer{
			err: errors.New("missing current category"),
		}, storage, time.Now,
	)

	_, _, err := service.Collect(
		t.Context(), "cookie", "01996c49-ea40-7abc-8def-1234567890ac",
	)
	if err == nil {
		t.Fatal("Collect succeeded with missing current categories")
	}
	if storage.saved.Key == "" || repository.extended.ObjectID != "" || repository.released != 1 {
		t.Fatalf("raw=%#v checkpoint=%#v releases=%d", storage.saved, repository.extended, repository.released)
	}
}

//nolint:funlen // Blocking the source across lease expiry proves the public use-case heartbeat.
func TestCollectCurrentSnapshotRenewsClaimAcrossOriginalLeaseExpiry(t *testing.T) {
	t.Parallel()
	repository := &memoryCurrentCollectionRepository{plan: application.CurrentCollectionPlan{
		Session: application.AnalyticalSession{
			ID: "01996c49-ea40-7abc-8def-1234567890ad", Status: "collecting",
		},
		Progress: application.CollectionProgress{},
		Next: &application.CurrentCollectionStep{
			Kind: application.CollectionStepComplexMembership, ComplexID: "1001", Page: 0,
		},
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	source := &memoryCurrentSource{
		membership:        application.CurrentSourceResponse{Body: []byte(`membership`), ContentType: "application/json"},
		membershipStarted: started, membershipRelease: release,
	}
	service := application.NewCurrentCollectionServiceWithTiming(
		activeSessionIdentity(), repository, source, memoryCurrentNormalizer{
			membership: application.CurrentMembership{Total: 1, PageSize: 50, ObjectIDs: []string{"2001"}},
		}, &recordingRawStorage{}, time.Now,
		application.CurrentCollectionTiming{Lease: 40 * time.Millisecond, RenewEvery: 10 * time.Millisecond},
	)
	type result struct{ err error }
	resultChannel := make(chan result, 1)
	go func() {
		_, _, err := service.Collect(
			t.Context(), "cookie", "01996c49-ea40-7abc-8def-1234567890ad",
		)
		resultChannel <- result{err: err}
	}()
	<-started
	time.Sleep(90 * time.Millisecond)
	close(release)
	completed := <-resultChannel
	if completed.err != nil || repository.renewals.Load() < 3 || repository.membership.SessionID == "" {
		t.Fatalf("collect error=%v renewals=%d checkpoint=%#v",
			completed.err, repository.renewals.Load(), repository.membership)
	}
}

//nolint:funlen // The blocked source and asynchronous cancellation belong in one behavior test.
func TestCollectCurrentSnapshotStopsWhenClaimRenewalLosesOwnership(t *testing.T) {
	t.Parallel()
	repository := &memoryCurrentCollectionRepository{
		plan: application.CurrentCollectionPlan{
			Session:  application.AnalyticalSession{ID: "01996c49-ea40-7abc-8def-1234567890ae", Status: "collecting"},
			Progress: application.CollectionProgress{},
			Next: &application.CurrentCollectionStep{
				Kind: application.CollectionStepComplexMembership, ComplexID: "1001", Page: 0,
			},
		},
		renewLost: true,
	}
	started := make(chan struct{})
	service := application.NewCurrentCollectionServiceWithTiming(
		activeSessionIdentity(), repository, &memoryCurrentSource{
			membershipStarted: started, membershipRelease: make(chan struct{}),
		}, memoryCurrentNormalizer{}, &recordingRawStorage{}, time.Now,
		application.CurrentCollectionTiming{Lease: 40 * time.Millisecond, RenewEvery: 10 * time.Millisecond},
	)
	resultChannel := make(chan error, 1)
	go func() {
		_, _, err := service.Collect(
			t.Context(), "cookie", "01996c49-ea40-7abc-8def-1234567890ae",
		)
		resultChannel <- err
	}()
	<-started
	select {
	case err := <-resultChannel:
		if err == nil || repository.membership.SessionID != "" {
			t.Fatalf("collect error=%v checkpoint=%#v", err, repository.membership)
		}
	case <-time.After(time.Second):
		t.Fatal("collect continued after losing claim ownership")
	}
}

type memoryCurrentCollectionRepository struct {
	plan           application.CurrentCollectionPlan
	membership     application.MembershipCheckpoint
	extended       application.ExtendedCheckpoint
	extendedResult application.AnalyticalSession
	released       int
	renewals       atomic.Int64
	renewLost      bool
}

func (repository *memoryCurrentCollectionRepository) ClaimCurrentCollection(
	_ context.Context,
	sessionID string,
	_ time.Time,
	_ time.Duration,
) (application.CurrentCollectionPlan, *application.CurrentCollectionClaim, bool, error) {
	claim := &application.CurrentCollectionClaim{
		Token: "01996c4a-0000-7000-8000-000000000001", SessionID: sessionID,
		Step: *repository.plan.Next,
	}
	return repository.plan, claim, true, nil
}

func (repository *memoryCurrentCollectionRepository) RenewCurrentCollectionClaim(
	context.Context,
	application.CurrentCollectionClaim,
	time.Time,
	time.Duration,
) (bool, error) {
	repository.renewals.Add(1)
	return !repository.renewLost, nil
}

func (repository *memoryCurrentCollectionRepository) ReleaseCurrentCollectionClaim(
	context.Context,
	application.CurrentCollectionClaim,
) error {
	repository.released++
	return nil
}

func (repository *memoryCurrentCollectionRepository) CheckpointComplexMembership(
	_ context.Context,
	checkpoint application.MembershipCheckpoint,
) (application.AnalyticalSession, application.CollectionProgress, error) {
	repository.membership = checkpoint
	return repository.plan.Session, application.CollectionProgress{
		CompletedRequests: 1, TotalRequests: intPointer(2),
	}, nil
}

func (repository *memoryCurrentCollectionRepository) CheckpointCurrentExtended(
	_ context.Context,
	checkpoint application.ExtendedCheckpoint,
) (application.AnalyticalSession, application.CollectionProgress, error) {
	repository.extended = checkpoint
	return repository.extendedResult,
		application.CollectionProgress{CompletedRequests: 2, TotalRequests: intPointer(2)}, nil
}

func intPointer(value int) *int { return &value }

func progressTotal(progress application.CollectionProgress) int {
	if progress.TotalRequests == nil {
		return -1
	}
	return *progress.TotalRequests
}

type memoryCurrentSource struct {
	membership        application.CurrentSourceResponse
	extended          application.CurrentSourceResponse
	membershipCalls   int
	extendedCalls     int
	membershipStarted chan<- struct{}
	membershipRelease <-chan struct{}
}

type memoryCurrentNormalizer struct {
	membership application.CurrentMembership
	metrics    []application.CurrentCategoryMetrics
	err        error
}

func (normalizer memoryCurrentNormalizer) NormalizeComplexMembership(
	[]byte,
) (application.CurrentMembership, error) {
	return normalizer.membership, normalizer.err
}

func (normalizer memoryCurrentNormalizer) NormalizeCurrentExtended(
	[]byte,
	string,
) ([]application.CurrentCategoryMetrics, error) {
	return normalizer.metrics, normalizer.err
}

func (source *memoryCurrentSource) ComplexConstructionObjects(
	ctx context.Context,
	_ string,
	_ int,
) (application.CurrentSourceResponse, error) {
	source.membershipCalls++
	if source.membershipStarted != nil {
		close(source.membershipStarted)
	}
	if source.membershipRelease != nil {
		select {
		case <-source.membershipRelease:
		case <-ctx.Done():
			return application.CurrentSourceResponse{}, ctx.Err()
		}
	}
	return source.membership, nil
}

func (source *memoryCurrentSource) ConstructionObjectExtended(
	context.Context,
	string,
) (application.CurrentSourceResponse, error) {
	source.extendedCalls++
	return source.extended, nil
}

type recordingRawStorage struct {
	saved application.RawArtifact
}

func (storage *recordingRawStorage) PutRaw(
	_ context.Context,
	artifact application.RawArtifact,
	_ []byte,
) error {
	storage.saved = artifact
	return nil
}

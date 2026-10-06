package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CurrentCollectionStepKind identifies the next quota-bearing current-only request.
type CurrentCollectionStepKind string

const (
	// CollectionStepComplexMembership reads page zero of one selected ЖК's objects.
	CollectionStepComplexMembership CurrentCollectionStepKind = "complex_membership"
	// CollectionStepCurrentExtended reads one construction object's current extended data.
	CollectionStepCurrentExtended      CurrentCollectionStepKind = "current_extended"
	defaultCurrentCollectionLease                                = 2 * time.Minute
	defaultCurrentCollectionRenewEvery                           = 30 * time.Second
)

// CurrentCollectionTiming controls the durable claim heartbeat.
type CurrentCollectionTiming struct {
	Lease      time.Duration
	RenewEvery time.Duration
}

// CollectionProgress is safe public progress without source payloads or credentials.
type CollectionProgress struct {
	Phase             string
	CompletedRequests int
	// TotalRequests stays unknown during authoritative membership discovery. Once every
	// selected complex is planned, it becomes the exact membership plus object request total.
	TotalRequests    *int
	DiscoveredTotal  int
	Cached           int
	Fetched          int
	Failed           int
	Remaining        *int
	CurrentComplexID *string
	CurrentObjectID  *string
	NextActionAt     *time.Time
	Percent          *int
}

// CurrentCollectionStep is the one business request allowed for the current collect call.
type CurrentCollectionStep struct {
	Kind      CurrentCollectionStepKind
	ComplexID string
	Page      int
	ObjectID  string
}

// CurrentCollectionPlan is a durable session checkpoint and its next request.
type CurrentCollectionPlan struct {
	Session  AnalyticalSession
	Progress CollectionProgress
	Next     *CurrentCollectionStep
}

// CurrentCollectionClaim is a private durable lease for one quota-bearing collection step.
type CurrentCollectionClaim struct {
	Token     string
	SessionID string
	Step      CurrentCollectionStep
}

// CurrentSourceResponse preserves the exact successful licensed response for raw-first storage.
type CurrentSourceResponse struct {
	Body          []byte
	ContentType   string
	SourceAddress string
}

// RawArtifact is immutable provenance for one exact source response.
type RawArtifact struct {
	Key           string
	SHA256        string
	Size          int64
	ContentType   string
	SourceAddress string
	ObservedAt    time.Time
}

// CurrentMembership is one validated authoritative membership page without transport fields.
type CurrentMembership struct {
	Total     int64
	PageSize  int
	ObjectIDs []string
}

// MembershipCheckpoint durably advances one selected ЖК after its raw response exists.
type MembershipCheckpoint struct {
	ClaimToken  string
	SessionID   string
	ComplexID   string
	Page        int
	Total       int64
	PageSize    int
	ObjectIDs   []string
	RawArtifact RawArtifact
}

// CurrentCategoryMetrics are the current cumulative published ЕИСЖС values for one category.
type CurrentCategoryMetrics struct {
	SegmentCode        string
	CumulativeDDUCount int64
}

// ExtendedCheckpoint durably advances one object after its raw response exists.
type ExtendedCheckpoint struct {
	ClaimToken  string
	SessionID   string
	ComplexID   string
	ObjectID    string
	Metrics     []CurrentCategoryMetrics
	RawArtifact RawArtifact
}

// CurrentCollectionRepository persists and atomically advances current-only collection plans.
type CurrentCollectionRepository interface {
	ClaimCurrentCollection(
		context.Context,
		string,
		time.Time,
		time.Duration,
	) (CurrentCollectionPlan, *CurrentCollectionClaim, bool, error)
	RenewCurrentCollectionClaim(
		context.Context,
		CurrentCollectionClaim,
		time.Time,
		time.Duration,
	) (bool, error)
	ReleaseCurrentCollectionClaim(context.Context, CurrentCollectionClaim) error
	CheckpointComplexMembership(
		context.Context,
		MembershipCheckpoint,
	) (AnalyticalSession, CollectionProgress, error)
	CheckpointCurrentExtended(
		context.Context,
		ExtendedCheckpoint,
	) (AnalyticalSession, CollectionProgress, error)
}

// CurrentSnapshotSource exposes only the two allowed current-only ЕИСЖС business requests.
type CurrentSnapshotSource interface {
	ComplexConstructionObjects(context.Context, string, int) (CurrentSourceResponse, error)
	ConstructionObjectExtended(context.Context, string) (CurrentSourceResponse, error)
}

// CurrentSnapshotNormalizer validates source transport payloads after their raw bytes are stored.
type CurrentSnapshotNormalizer interface {
	NormalizeComplexMembership([]byte) (CurrentMembership, error)
	NormalizeCurrentExtended([]byte, string) ([]CurrentCategoryMetrics, error)
}

// RawArtifactStorage stores exact source bytes in a private object store.
type RawArtifactStorage interface {
	PutRaw(context.Context, RawArtifact, []byte) error
}

// CurrentCollectionService advances a session by at most one quota-bearing request per call.
type CurrentCollectionService struct {
	identity   SessionIdentity
	repository CurrentCollectionRepository
	source     CurrentSnapshotSource
	normalizer CurrentSnapshotNormalizer
	storage    RawArtifactStorage
	now        func() time.Time
	timing     CurrentCollectionTiming
}

// NewCurrentCollectionService creates the explicit sequential current-only collector.
func NewCurrentCollectionService(
	identity SessionIdentity,
	repository CurrentCollectionRepository,
	source CurrentSnapshotSource,
	normalizer CurrentSnapshotNormalizer,
	storage RawArtifactStorage,
	now func() time.Time,
) *CurrentCollectionService {
	return NewCurrentCollectionServiceWithTiming(
		identity, repository, source, normalizer, storage, now, CurrentCollectionTiming{
			Lease: defaultCurrentCollectionLease, RenewEvery: defaultCurrentCollectionRenewEvery,
		},
	)
}

// NewCurrentCollectionServiceWithTiming creates a collector with explicit claim timing.
func NewCurrentCollectionServiceWithTiming(
	identity SessionIdentity,
	repository CurrentCollectionRepository,
	source CurrentSnapshotSource,
	normalizer CurrentSnapshotNormalizer,
	storage RawArtifactStorage,
	now func() time.Time,
	timing CurrentCollectionTiming,
) *CurrentCollectionService {
	if timing.Lease <= 0 || timing.RenewEvery <= 0 || timing.RenewEvery >= timing.Lease {
		timing = CurrentCollectionTiming{
			Lease: defaultCurrentCollectionLease, RenewEvery: defaultCurrentCollectionRenewEvery,
		}
	}
	return &CurrentCollectionService{
		identity: identity, repository: repository, source: source, normalizer: normalizer,
		storage: storage, now: now, timing: timing,
	}
}

// Collect authenticates an employee and advances one durable collection step.
//
//nolint:funlen // Authentication, durable claim, and one-step dispatch form the public use case.
func (service *CurrentCollectionService) Collect(
	ctx context.Context,
	rawToken string,
	sessionID string,
) (AnalyticalSession, CollectionProgress, error) {
	account, err := service.identity.CurrentAccount(ctx, rawToken)
	if err != nil {
		return AnalyticalSession{}, CollectionProgress{}, err
	}
	if account.State != AccountActive {
		return AnalyticalSession{}, CollectionProgress{}, ErrPasswordChangeNeeded
	}
	plan, claim, found, err := service.repository.ClaimCurrentCollection(
		ctx, sessionID, service.now().UTC(), service.timing.Lease,
	)
	if err != nil {
		return AnalyticalSession{}, CollectionProgress{}, fmt.Errorf("load current collection: %w", err)
	}
	if !found {
		return AnalyticalSession{}, CollectionProgress{}, ErrNotFound
	}
	if plan.Next == nil {
		return plan.Session, plan.Progress, nil
	}
	if claim == nil {
		return AnalyticalSession{}, CollectionProgress{}, errors.New("current collection step lacks a claim")
	}
	heartbeat := service.startClaimHeartbeat(ctx, *claim)
	defer heartbeat.stop()
	switch plan.Next.Kind {
	case CollectionStepComplexMembership:
		return service.collectMembership(heartbeat.ctx, *claim, heartbeat)
	case CollectionStepCurrentExtended:
		return service.collectExtended(heartbeat.ctx, *claim, heartbeat)
	default:
		return AnalyticalSession{}, CollectionProgress{}, errors.New("unknown current collection step")
	}
}

//nolint:funlen // Raw-first storage and the fenced checkpoint are one temporal consistency unit.
func (service *CurrentCollectionService) collectMembership(
	ctx context.Context,
	claim CurrentCollectionClaim,
	heartbeat *claimHeartbeat,
) (AnalyticalSession, CollectionProgress, error) {
	step := claim.Step
	response, err := service.source.ComplexConstructionObjects(ctx, step.ComplexID, step.Page)
	if err != nil {
		return service.releaseFailedClaim(ctx, claim, fmt.Errorf("read ЕИСЖС complex membership: %w", err))
	}
	artifact := rawArtifact(
		fmt.Sprintf("eiszhs/sessions/%s/complexes/%s/pages/%d", claim.SessionID, step.ComplexID, step.Page),
		response,
		service.now().UTC(),
	)
	if err := service.storage.PutRaw(ctx, artifact, response.Body); err != nil {
		return service.releaseFailedClaim(ctx, claim, fmt.Errorf("store raw ЕИСЖС complex membership: %w", err))
	}
	membership, err := service.normalizer.NormalizeComplexMembership(response.Body)
	if err != nil {
		return service.releaseFailedClaim(ctx, claim, fmt.Errorf(
			"normalize ЕИСЖС complex membership: %w", err,
		))
	}
	if err := heartbeat.beginCheckpoint(); err != nil {
		return AnalyticalSession{}, CollectionProgress{}, fmt.Errorf(
			"renew current collection claim before membership checkpoint: %w", err,
		)
	}
	return service.repository.CheckpointComplexMembership(ctx, MembershipCheckpoint{
		ClaimToken: claim.Token, SessionID: claim.SessionID,
		ComplexID: step.ComplexID, Page: step.Page,
		Total: membership.Total, PageSize: membership.PageSize,
		ObjectIDs: membership.ObjectIDs, RawArtifact: artifact,
	})
}

//nolint:funlen // Raw-first storage and the fenced checkpoint are one temporal consistency unit.
func (service *CurrentCollectionService) collectExtended(
	ctx context.Context,
	claim CurrentCollectionClaim,
	heartbeat *claimHeartbeat,
) (AnalyticalSession, CollectionProgress, error) {
	step := claim.Step
	response, err := service.source.ConstructionObjectExtended(ctx, step.ObjectID)
	if err != nil {
		return service.releaseFailedClaim(ctx, claim, fmt.Errorf("read ЕИСЖС current extended object: %w", err))
	}
	artifact := rawArtifact(
		fmt.Sprintf("eiszhs/sessions/%s/objects/%s/extended", claim.SessionID, step.ObjectID),
		response,
		service.now().UTC(),
	)
	if err := service.storage.PutRaw(ctx, artifact, response.Body); err != nil {
		return service.releaseFailedClaim(ctx, claim, fmt.Errorf("store raw ЕИСЖС current extended object: %w", err))
	}
	metrics, err := service.normalizer.NormalizeCurrentExtended(response.Body, step.ObjectID)
	if err != nil {
		return service.releaseFailedClaim(ctx, claim, fmt.Errorf(
			"normalize ЕИСЖС current extended object: %w", err,
		))
	}
	if err := heartbeat.beginCheckpoint(); err != nil {
		return AnalyticalSession{}, CollectionProgress{}, fmt.Errorf(
			"renew current collection claim before extended checkpoint: %w", err,
		)
	}
	return service.repository.CheckpointCurrentExtended(ctx, ExtendedCheckpoint{
		ClaimToken: claim.Token, SessionID: claim.SessionID,
		ComplexID: step.ComplexID, ObjectID: step.ObjectID,
		Metrics: metrics, RawArtifact: artifact,
	})
}

type claimHeartbeat struct {
	ctx           context.Context
	cancel        context.CancelCauseFunc
	done          chan struct{}
	mu            sync.Mutex
	checkpointing bool
	lost          error
}

func (service *CurrentCollectionService) startClaimHeartbeat(
	ctx context.Context,
	claim CurrentCollectionClaim,
) *claimHeartbeat {
	heartbeatContext, cancel := context.WithCancelCause(ctx)
	heartbeat := &claimHeartbeat{
		ctx: heartbeatContext, cancel: cancel, done: make(chan struct{}),
	}
	go service.renewCurrentCollectionClaim(heartbeat, claim)
	return heartbeat
}

func (service *CurrentCollectionService) renewCurrentCollectionClaim(
	heartbeat *claimHeartbeat,
	claim CurrentCollectionClaim,
) {
	defer close(heartbeat.done)
	ticker := time.NewTicker(service.timing.RenewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-heartbeat.ctx.Done():
			return
		case <-ticker.C:
			renewed, err := service.repository.RenewCurrentCollectionClaim(
				heartbeat.ctx, claim, service.now().UTC(), service.timing.Lease,
			)
			if err != nil {
				heartbeat.lose(fmt.Errorf("renew current collection claim: %w", err))
				return
			}
			if !renewed {
				heartbeat.lose(ErrConflict)
				return
			}
		}
	}
}

func (heartbeat *claimHeartbeat) beginCheckpoint() error {
	heartbeat.mu.Lock()
	defer heartbeat.mu.Unlock()
	if heartbeat.lost != nil {
		return heartbeat.lost
	}
	if err := heartbeat.ctx.Err(); err != nil {
		return context.Cause(heartbeat.ctx)
	}
	heartbeat.checkpointing = true
	return nil
}

func (heartbeat *claimHeartbeat) lose(cause error) {
	heartbeat.mu.Lock()
	if heartbeat.lost == nil {
		heartbeat.lost = cause
	}
	cancel := !heartbeat.checkpointing
	heartbeat.mu.Unlock()
	if cancel {
		heartbeat.cancel(cause)
	}
}

func (heartbeat *claimHeartbeat) stop() {
	heartbeat.cancel(context.Canceled)
	<-heartbeat.done
}

func (service *CurrentCollectionService) releaseFailedClaim(
	ctx context.Context,
	claim CurrentCollectionClaim,
	cause error,
) (AnalyticalSession, CollectionProgress, error) {
	if err := service.repository.ReleaseCurrentCollectionClaim(ctx, claim); err != nil {
		return AnalyticalSession{}, CollectionProgress{}, errors.Join(
			cause, fmt.Errorf("release current collection claim: %w", err),
		)
	}
	return AnalyticalSession{}, CollectionProgress{}, cause
}

func rawArtifact(keyPrefix string, response CurrentSourceResponse, observedAt time.Time) RawArtifact {
	digest := sha256.Sum256(response.Body)
	checksum := hex.EncodeToString(digest[:])
	contentType := strings.TrimSpace(strings.Split(response.ContentType, ";")[0])
	if contentType == "" {
		contentType = "application/json"
	}
	return RawArtifact{
		Key: keyPrefix + "/" + checksum + ".json", SHA256: checksum, Size: int64(len(response.Body)),
		ContentType: contentType, SourceAddress: response.SourceAddress, ObservedAt: observedAt,
	}
}

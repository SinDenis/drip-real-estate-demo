package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const createSessionEndpoint = "/api/v1/sessions"

var (
	// ErrIdempotencyConflict means one key was reused with a different canonical request.
	ErrIdempotencyConflict = errors.New("idempotency key conflicts with an earlier request")
	// ErrInvalidIdempotencyKey means the request key is absent or outside printable ASCII.
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	// ErrInvalidSessionSelection means the submitted filters are not a canonical selectable market.
	ErrInvalidSessionSelection = errors.New("invalid analytical session selection")
	regionCodePattern          = regexp.MustCompile(`^[0-9]{2}$`)
)

// CreateSessionInput is the canonical identifier-only public session request.
type CreateSessionInput struct {
	RegionCodes           []string `json:"regionCodes"`
	ResidentialComplexIDs []string `json:"residentialComplexIds"`
	SegmentCodes          []string `json:"segmentCodes"`
}

// SessionRegion snapshots one canonical region display value.
type SessionRegion struct {
	Code string
	Name string
}

// SessionComplex snapshots one canonical ЕИСЖС complex display value.
type SessionComplex struct {
	ID         string
	RegionCode string
	Name       string
}

// SessionObservation is one fresh reusable current-month observation.
type SessionObservation struct {
	ID          string
	RegionCode  string
	ComplexID   string
	SegmentCode string
	ObservedAt  time.Time
}

// ResolvedSessionSelection contains server-authoritative names and fresh observations.
type ResolvedSessionSelection struct {
	Regions      []SessionRegion
	Complexes    []SessionComplex
	Observations []SessionObservation
}

// NewAnalyticalSession is an immutable session and its idempotency scope.
type NewAnalyticalSession struct {
	Session        AnalyticalSession
	AccountID      string
	Endpoint       string
	IdempotencyKey string
	RequestHash    string
}

// AnalyticalSessionIdempotency identifies one logical create submission.
type AnalyticalSessionIdempotency struct {
	AccountID      string
	Endpoint       string
	IdempotencyKey string
	RequestHash    string
}

// AnalyticalSessionRepository resolves shared observations and creates sessions atomically.
type AnalyticalSessionRepository interface {
	FindAnalyticalSessionReplay(context.Context, AnalyticalSessionIdempotency) (AnalyticalSession, bool, error)
	ResolveSessionSelection(context.Context, CreateSessionInput, time.Time) (ResolvedSessionSelection, error)
	CreateAnalyticalSession(context.Context, NewAnalyticalSession) (AnalyticalSession, bool, error)
}

// SessionIdentity resolves the authenticated employee for a request.
type SessionIdentity interface {
	CurrentAccount(context.Context, string) (Account, error)
}

// AnalyticalSessionService creates immutable current-only sessions.
type AnalyticalSessionService struct {
	identity   SessionIdentity
	repository AnalyticalSessionRepository
	now        func() time.Time
}

// NewAnalyticalSessionService creates the session use case.
func NewAnalyticalSessionService(
	identity SessionIdentity,
	repository AnalyticalSessionRepository,
	now func() time.Time,
) *AnalyticalSessionService {
	return &AnalyticalSessionService{identity: identity, repository: repository, now: now}
}

// Create validates canonical filters, pins fresh observations, and persists one session.
//
//nolint:funlen,gocyclo // Authentication, canonicalization, replay, resolution, and creation are one use case.
func (service *AnalyticalSessionService) Create(
	ctx context.Context,
	rawToken string,
	idempotencyKey string,
	input CreateSessionInput,
) (AnalyticalSession, bool, error) {
	account, err := service.identity.CurrentAccount(ctx, rawToken)
	if err != nil {
		return AnalyticalSession{}, false, err
	}
	if account.State != AccountActive {
		return AnalyticalSession{}, false, ErrPasswordChangeNeeded
	}
	if !validIdempotencyKey(idempotencyKey) {
		return AnalyticalSession{}, false, ErrInvalidIdempotencyKey
	}
	canonical, segments, err := canonicalSessionInput(input)
	if err != nil {
		return AnalyticalSession{}, false, err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return AnalyticalSession{}, false, fmt.Errorf("encode canonical session request: %w", err)
	}
	digest := sha256.Sum256(encoded)
	idempotency := AnalyticalSessionIdempotency{
		AccountID: account.ID, Endpoint: createSessionEndpoint,
		IdempotencyKey: idempotencyKey, RequestHash: hex.EncodeToString(digest[:]),
	}
	existing, found, err := service.repository.FindAnalyticalSessionReplay(ctx, idempotency)
	if err != nil {
		return AnalyticalSession{}, false, fmt.Errorf("find analytical session replay: %w", err)
	}
	if found {
		return existing, true, nil
	}
	now := service.now().UTC()
	resolved, err := service.repository.ResolveSessionSelection(ctx, canonical, now)
	if err != nil {
		return AnalyticalSession{}, false, fmt.Errorf("resolve analytical session selection: %w", err)
	}
	if len(resolved.Regions) != len(canonical.RegionCodes) ||
		len(resolved.Complexes) != len(canonical.ResidentialComplexIDs) {
		return AnalyticalSession{}, false, ErrInvalidSessionSelection
	}
	session := newResolvedSession(account, canonical, segments, resolved, now)
	created, replayed, err := service.repository.CreateAnalyticalSession(ctx, NewAnalyticalSession{
		Session: session, AccountID: idempotency.AccountID, Endpoint: idempotency.Endpoint,
		IdempotencyKey: idempotency.IdempotencyKey, RequestHash: idempotency.RequestHash,
	})
	if err != nil {
		return AnalyticalSession{}, false, fmt.Errorf("create analytical session: %w", err)
	}
	return created, replayed, nil
}

type resolvedSegment struct {
	Code      string
	Name      string
	Supported bool
}

//nolint:funlen // All three required filter families are canonicalized together.
func canonicalSessionInput(input CreateSessionInput) (CreateSessionInput, []resolvedSegment, error) {
	regions, err := canonicalValues(input.RegionCodes, 89, regionCodePattern.MatchString)
	if err != nil || slicesContain(regions, "99") {
		return CreateSessionInput{}, nil, ErrInvalidSessionSelection
	}
	complexes, err := canonicalDeduplicatedValues(input.ResidentialComplexIDs, 20, func(value string) bool {
		return len(value) <= 128
	})
	if err != nil {
		return CreateSessionInput{}, nil, ErrInvalidSessionSelection
	}
	segmentValues, err := canonicalValues(input.SegmentCodes, 6, func(value string) bool {
		_, exists := sessionSegmentCatalog()[value]
		return exists
	})
	if err != nil {
		return CreateSessionInput{}, nil, ErrInvalidSessionSelection
	}
	segments := make([]resolvedSegment, 0, len(segmentValues))
	supported := 0
	for _, code := range segmentValues {
		segment := sessionSegmentCatalog()[code]
		segments = append(segments, segment)
		if segment.Supported {
			supported++
		}
	}
	if supported == 0 {
		return CreateSessionInput{}, nil, ErrInvalidSessionSelection
	}
	return CreateSessionInput{
		RegionCodes: regions, ResidentialComplexIDs: complexes, SegmentCodes: segmentValues,
	}, segments, nil
}

func canonicalDeduplicatedValues(
	values []string,
	maximum int,
	valid func(string) bool,
) ([]string, error) {
	if len(values) == 0 || len(values) > maximum {
		return nil, ErrInvalidSessionSelection
	}
	result := append([]string(nil), values...)
	for index, value := range result {
		result[index] = strings.TrimSpace(value)
		if result[index] == "" || !valid(result[index]) {
			return nil, ErrInvalidSessionSelection
		}
	}
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique, nil
}

func canonicalValues(
	values []string,
	maximum int,
	valid func(string) bool,
) ([]string, error) {
	if len(values) == 0 || len(values) > maximum {
		return nil, ErrInvalidSessionSelection
	}
	result := append([]string(nil), values...)
	for index, value := range result {
		result[index] = strings.TrimSpace(value)
		if result[index] == "" || !valid(result[index]) {
			return nil, ErrInvalidSessionSelection
		}
	}
	sort.Strings(result)
	for index := 1; index < len(result); index++ {
		if result[index] == result[index-1] {
			return nil, ErrInvalidSessionSelection
		}
	}
	return result, nil
}

func sessionSegmentCatalog() map[string]resolvedSegment {
	return map[string]resolvedSegment{
		"apartments":           {Code: "apartments", Name: "Апартаменты"},
		"industrial_warehouse": {Code: "industrial_warehouse", Name: "Производственно-складская недвижимость"},
		"non_residential":      {Code: "non_residential", Name: "Нежилые помещения", Supported: true},
		"parking":              {Code: "parking", Name: "Машино-места", Supported: true},
		"residential":          {Code: "residential", Name: "Жилые помещения", Supported: true},
		"storage":              {Code: "storage", Name: "Кладовые"},
	}
}

//nolint:funlen // The immutable session snapshot is assembled in one place.
func newResolvedSession(
	account Account,
	input CreateSessionInput,
	segments []resolvedSegment,
	resolved ResolvedSessionSelection,
	now time.Time,
) AnalyticalSession {
	session := AnalyticalSession{
		ID: uuid.Must(uuid.NewV7()).String(), CreatedAt: now, CreatedByID: account.ID,
		CreatedByDisplayName: account.DisplayName, RegionCodes: input.RegionCodes,
		ResidentialComplexIDs: input.ResidentialComplexIDs, SegmentCodes: input.SegmentCodes,
	}
	for _, region := range resolved.Regions {
		session.Regions = append(session.Regions, region.Name)
	}
	for _, complex := range resolved.Complexes {
		session.ResidentialComplexes = append(session.ResidentialComplexes, complex.Name)
		session.ResidentialComplexRegionCodes = append(
			session.ResidentialComplexRegionCodes, complex.RegionCode,
		)
	}
	unsupported := false
	supported := make(map[string]bool)
	for _, segment := range segments {
		session.Segments = append(session.Segments, segment.Name)
		supported[segment.Code] = segment.Supported
		unsupported = unsupported || !segment.Supported
	}
	resolved.Observations = exactSessionObservations(resolved.Complexes, supported, resolved.Observations)
	session.Status = resolvedSessionStatus(resolved.Complexes, supported, resolved.Observations, unsupported)
	if session.Status == "collecting" {
		currentComplexID := session.ResidentialComplexIDs[0]
		session.CollectionProgress = &CollectionProgress{
			Phase: "discovering_objects", CompletedRequests: 0,
			CurrentComplexID: &currentComplexID, NextActionAt: &now,
		}
	}
	session.ObservedFrom, session.ObservedTo = observationWindow(resolved.Observations)
	for _, observation := range resolved.Observations {
		session.ObservationIDs = append(session.ObservationIDs, observation.ID)
	}
	sort.Strings(session.ObservationIDs)
	session.Name = DeterministicSessionName(session)
	return session
}

func resolvedSessionStatus(
	complexes []SessionComplex,
	supported map[string]bool,
	observations []SessionObservation,
	unsupported bool,
) string {
	expected := make(map[string]struct{})
	for _, residentialComplex := range complexes {
		for code, isSupported := range supported {
			if isSupported {
				expected[observationTuple(residentialComplex.RegionCode, residentialComplex.ID, code)] = struct{}{}
			}
		}
	}
	covered := make(map[string]struct{})
	for _, observation := range observations {
		covered[observationTuple(observation.RegionCode, observation.ComplexID, observation.SegmentCode)] = struct{}{}
	}
	if len(covered) < len(expected) {
		return "collecting"
	}
	if unsupported {
		return "partial"
	}
	return "complete"
}

func exactSessionObservations(
	complexes []SessionComplex,
	supported map[string]bool,
	observations []SessionObservation,
) []SessionObservation {
	expected := make(map[string]struct{})
	for _, residentialComplex := range complexes {
		for code, isSupported := range supported {
			if isSupported {
				expected[observationTuple(residentialComplex.RegionCode, residentialComplex.ID, code)] = struct{}{}
			}
		}
	}
	matching := make([]SessionObservation, 0, len(observations))
	for _, observation := range observations {
		if _, exists := expected[observationTuple(
			observation.RegionCode, observation.ComplexID, observation.SegmentCode,
		)]; exists {
			matching = append(matching, observation)
		}
	}
	return matching
}

func observationTuple(regionCode string, complexID string, segmentCode string) string {
	return regionCode + "\x00" + complexID + "\x00" + segmentCode
}

func observationWindow(observations []SessionObservation) (*time.Time, *time.Time) {
	if len(observations) == 0 {
		return nil, nil
	}
	from, to := observations[0].ObservedAt, observations[0].ObservedAt
	for _, observation := range observations[1:] {
		if observation.ObservedAt.Before(from) {
			from = observation.ObservedAt
		}
		if observation.ObservedAt.After(to) {
			to = observation.ObservedAt
		}
	}
	return &from, &to
}

func validIdempotencyKey(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range []byte(value) {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func slicesContain(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	"github.com/google/uuid"
)

//nolint:funlen,gocyclo // One public result is checked across its coupled immutable fields.
func TestCreateAnalyticalSessionPinsFreshObservations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	repository := &memoryAnalyticalSessionRepository{selection: application.ResolvedSessionSelection{
		Regions: []application.SessionRegion{{Code: "77", Name: "Москва"}},
		Complexes: []application.SessionComplex{
			{ID: "1001", RegionCode: "77", Name: "ЖК Северный"},
			{ID: "1002", RegionCode: "77", Name: "ЖК Южный"},
		},
		Observations: []application.SessionObservation{
			{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", RegionCode: "77", ComplexID: "1001", SegmentCode: "residential", ObservedAt: now.Add(-2 * time.Hour)},
			{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", RegionCode: "77", ComplexID: "1002", SegmentCode: "residential", ObservedAt: now.Add(-time.Hour)},
		},
	}}
	service := application.NewAnalyticalSessionService(
		staticSessionIdentity{account: application.Account{ID: "employee-1", DisplayName: "Аналитик", State: application.AccountActive}},
		repository,
		func() time.Time { return now },
	)

	session, replayed, err := service.Create(t.Context(), "cookie", "submit-1", application.CreateSessionInput{
		RegionCodes:           []string{"77"},
		ResidentialComplexIDs: []string{"1002", "1001"},
		SegmentCodes:          []string{"residential", "apartments"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	parsed, err := uuid.Parse(session.ID)
	if err != nil || parsed.Version() != 7 {
		t.Fatalf("session ID = %q, version = %v, err = %v", session.ID, parsed.Version(), err)
	}
	if replayed || session.Status != "partial" || session.Name != "Москва - 2 ЖК" {
		t.Fatalf("session = %#v, replayed = %t", session, replayed)
	}
	if len(session.ObservationIDs) != 2 || session.ObservedFrom == nil || session.ObservedTo == nil ||
		!session.ObservedFrom.Equal(now.Add(-2*time.Hour)) ||
		!session.ObservedTo.Equal(now.Add(-time.Hour)) ||
		session.PeriodFrom != nil || session.PeriodTo != nil {
		t.Fatalf("observation pin = %#v", session)
	}
	if repository.created.IdempotencyKey != "submit-1" || repository.created.AccountID != "employee-1" {
		t.Fatalf("created record = %#v", repository.created)
	}
}

//nolint:funlen // The table documents the complete public validation contract compactly.
func TestCreateAnalyticalSessionValidatesPublicRequest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	valid := application.CreateSessionInput{
		RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"},
		SegmentCodes: []string{"residential"},
	}
	tooManyComplexes := make([]string, 21)
	for index := range tooManyComplexes {
		tooManyComplexes[index] = uuid.NewString()
	}
	tests := []struct {
		name  string
		key   string
		input application.CreateSessionInput
	}{
		{name: "missing key", input: valid},
		{name: "long key", key: strings.Repeat("a", 129), input: valid},
		{name: "control key", key: "line\nbreak", input: valid},
		{name: "non ASCII key", key: "ключ", input: valid},
		{name: "missing region", key: "key", input: application.CreateSessionInput{}},
		{name: "invalid region", key: "key", input: application.CreateSessionInput{RegionCodes: []string{"7"}, ResidentialComplexIDs: []string{"1001"}, SegmentCodes: []string{"residential"}}},
		{name: "reserved region", key: "key", input: application.CreateSessionInput{RegionCodes: []string{"99"}, ResidentialComplexIDs: []string{"1001"}, SegmentCodes: []string{"residential"}}},
		{name: "duplicate region", key: "key", input: application.CreateSessionInput{RegionCodes: []string{"77", "77"}, ResidentialComplexIDs: []string{"1001"}, SegmentCodes: []string{"residential"}}},
		{name: "empty complex", key: "key", input: application.CreateSessionInput{RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{" "}, SegmentCodes: []string{"residential"}}},
		{name: "too many complexes", key: "key", input: application.CreateSessionInput{RegionCodes: []string{"77"}, ResidentialComplexIDs: tooManyComplexes, SegmentCodes: []string{"residential"}}},
		{name: "unknown segment", key: "key", input: application.CreateSessionInput{RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"}, SegmentCodes: []string{"unknown"}}},
		{name: "unsupported only", key: "key", input: application.CreateSessionInput{RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"}, SegmentCodes: []string{"apartments"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := application.NewAnalyticalSessionService(
				staticSessionIdentity{account: application.Account{State: application.AccountActive}},
				&memoryAnalyticalSessionRepository{}, func() time.Time { return now },
			)
			_, _, err := service.Create(t.Context(), "cookie", test.key, test.input)
			if !errors.Is(err, expectedValidationError(test.key)) {
				t.Fatalf("Create error = %v", err)
			}
		})
	}
}

func TestCreateAnalyticalSessionReportsAuthenticationAndRepositoryFailures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	input := application.CreateSessionInput{
		RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"},
		SegmentCodes: []string{"residential"},
	}
	dependencyFailure := errors.New("dependency failed")
	tests := []struct {
		name       string
		identity   staticSessionIdentity
		repository *memoryAnalyticalSessionRepository
		expected   error
	}{
		{name: "identity", identity: staticSessionIdentity{err: dependencyFailure}, repository: &memoryAnalyticalSessionRepository{}, expected: dependencyFailure},
		{name: "pending account", identity: staticSessionIdentity{account: application.Account{State: application.AccountPendingActivation}}, repository: &memoryAnalyticalSessionRepository{}, expected: application.ErrPasswordChangeNeeded},
		{name: "resolution", identity: activeSessionIdentity(), repository: &memoryAnalyticalSessionRepository{resolveErr: dependencyFailure}, expected: dependencyFailure},
		{name: "unknown canonical value", identity: activeSessionIdentity(), repository: &memoryAnalyticalSessionRepository{}, expected: application.ErrInvalidSessionSelection},
		{name: "creation", identity: activeSessionIdentity(), repository: &memoryAnalyticalSessionRepository{selection: resolvedSelection(now), createErr: dependencyFailure}, expected: dependencyFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := application.NewAnalyticalSessionService(
				test.identity, test.repository, func() time.Time { return now },
			)
			_, _, err := service.Create(t.Context(), "cookie", "key", input)
			if !errors.Is(err, test.expected) {
				t.Fatalf("Create error = %v, want %v", err, test.expected)
			}
		})
	}
}

//nolint:funlen // Warm and cold expectations share one compact public behavior table.
func TestCreateAnalyticalSessionUsesWarmAndColdStatuses(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	input := application.CreateSessionInput{
		RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"},
		SegmentCodes: []string{"residential"},
	}
	for _, test := range []struct {
		name      string
		selection application.ResolvedSessionSelection
		status    string
		replayed  bool
	}{
		{name: "warm", selection: resolvedSelection(now), status: "complete", replayed: true},
		{name: "cold", selection: resolvedSelection(time.Time{}), status: "collecting"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &memoryAnalyticalSessionRepository{selection: test.selection, replayed: test.replayed}
			service := application.NewAnalyticalSessionService(
				activeSessionIdentity(), repository, func() time.Time { return now },
			)
			session, replayed, err := service.Create(t.Context(), "cookie", "key", input)
			if err != nil || replayed != test.replayed || session.Status != test.status {
				t.Fatalf("Create = %#v, replayed %t, error %v", session, replayed, err)
			}
			if test.status == "collecting" &&
				(session.ObservedFrom != nil || session.ObservedTo != nil ||
					session.PeriodFrom != nil || session.PeriodTo != nil) {
				t.Fatalf("cold session invented an observation window or period: %#v", session)
			}
		})
	}
}

func TestCreateAnalyticalSessionReplaysBeforeMutableResolution(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	stored := application.AnalyticalSession{
		ID: "01996c43-6c00-7abc-8def-1234567890ab", Status: "complete",
		RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"},
		SegmentCodes: []string{"residential"},
	}
	repository := &memoryAnalyticalSessionRepository{
		existing: &stored, resolveErr: errors.New("catalog is currently unavailable"),
	}
	service := application.NewAnalyticalSessionService(
		activeSessionIdentity(), repository, func() time.Time { return now },
	)

	replayed, wasReplay, err := service.Create(t.Context(), "cookie", "same-logical-submit", application.CreateSessionInput{
		RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"},
		SegmentCodes: []string{"residential"},
	})
	if err != nil || !wasReplay || replayed.ID != stored.ID {
		t.Fatalf("Create replay = %#v, replayed %t, error %v", replayed, wasReplay, err)
	}
	if repository.resolveCalls != 0 {
		t.Fatalf("mutable selection resolution calls = %d, want 0", repository.resolveCalls)
	}
}

func TestCreateAnalyticalSessionPinsOnlyExactResolvedTuples(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	repository := &memoryAnalyticalSessionRepository{selection: application.ResolvedSessionSelection{
		Regions:   []application.SessionRegion{{Code: "77", Name: "Москва"}},
		Complexes: []application.SessionComplex{{ID: "1001", RegionCode: "77", Name: "ЖК Северный"}},
		Observations: []application.SessionObservation{{
			ID: uuid.NewString(), RegionCode: "78", ComplexID: "1001",
			SegmentCode: "residential", ObservedAt: now,
		}},
	}}
	service := application.NewAnalyticalSessionService(
		activeSessionIdentity(), repository, func() time.Time { return now },
	)

	session, _, err := service.Create(t.Context(), "cookie", "exact-tuples", application.CreateSessionInput{
		RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001"},
		SegmentCodes: []string{"residential"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if session.Status != "collecting" || len(session.ObservationIDs) != 0 ||
		session.ObservedFrom != nil || session.ObservedTo != nil {
		t.Fatalf("session accepted a mismatched observation tuple: %#v", session)
	}
}

func TestCreateAnalyticalSessionDeduplicatesRepeatedComplexIDs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	repository := &memoryAnalyticalSessionRepository{selection: resolvedSelection(time.Time{})}
	service := application.NewAnalyticalSessionService(
		activeSessionIdentity(), repository, func() time.Time { return now },
	)

	session, _, err := service.Create(t.Context(), "cookie", "deduplicate-complexes",
		application.CreateSessionInput{
			RegionCodes: []string{"77"}, ResidentialComplexIDs: []string{"1001", "1001"},
			SegmentCodes: []string{"residential"},
		})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(session.ResidentialComplexIDs) != 1 || session.ResidentialComplexIDs[0] != "1001" {
		t.Fatalf("returned session retained duplicate complexes: %#v", session)
	}
}

func expectedValidationError(key string) error {
	if key == "" || len(key) > 128 || key == "line\nbreak" || key == "ключ" {
		return application.ErrInvalidIdempotencyKey
	}
	return application.ErrInvalidSessionSelection
}

func activeSessionIdentity() staticSessionIdentity {
	return staticSessionIdentity{account: application.Account{
		ID: "employee-1", DisplayName: "Аналитик", State: application.AccountActive,
	}}
}

func resolvedSelection(observedAt time.Time) application.ResolvedSessionSelection {
	selection := application.ResolvedSessionSelection{
		Regions:   []application.SessionRegion{{Code: "77", Name: "Москва"}},
		Complexes: []application.SessionComplex{{ID: "1001", RegionCode: "77", Name: "ЖК Северный"}},
	}
	if !observedAt.IsZero() {
		selection.Observations = []application.SessionObservation{{
			ID: uuid.NewString(), RegionCode: "77", ComplexID: "1001",
			SegmentCode: "residential", ObservedAt: observedAt,
		}}
	}
	return selection
}

type staticSessionIdentity struct {
	account application.Account
	err     error
}

func (identity staticSessionIdentity) CurrentAccount(context.Context, string) (application.Account, error) {
	return identity.account, identity.err
}

type memoryAnalyticalSessionRepository struct {
	selection    application.ResolvedSessionSelection
	created      application.NewAnalyticalSession
	existing     *application.AnalyticalSession
	resolveErr   error
	createErr    error
	replayed     bool
	resolveCalls int
}

func (repository *memoryAnalyticalSessionRepository) ResolveSessionSelection(
	_ context.Context,
	_ application.CreateSessionInput,
	_ time.Time,
) (application.ResolvedSessionSelection, error) {
	repository.resolveCalls++
	return repository.selection, repository.resolveErr
}

func (repository *memoryAnalyticalSessionRepository) FindAnalyticalSessionReplay(
	context.Context,
	application.AnalyticalSessionIdempotency,
) (application.AnalyticalSession, bool, error) {
	if repository.existing == nil {
		return application.AnalyticalSession{}, false, nil
	}
	return *repository.existing, true, nil
}

func (repository *memoryAnalyticalSessionRepository) CreateAnalyticalSession(
	_ context.Context,
	created application.NewAnalyticalSession,
) (application.AnalyticalSession, bool, error) {
	repository.created = created
	return created.Session, repository.replayed, repository.createErr
}

package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/httpapi"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

const ceSourceID = "11111111-1111-4111-8111-111111111111"
const ceEmployeeID = "22222222-2222-4222-8222-222222222222"
const ceOtherID = "33333333-3333-4333-8333-333333333333"

var ceNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

type ceCredentials struct{ application.CredentialManager }

func (ceCredentials) HashSessionToken(raw string) string { return raw }

type ceStore struct {
	application.AccessRepository
	source  application.AnalyticalSession
	missing bool
	failure bool
	created []application.NewAnalyticalSession
	replay  map[string]application.NewAnalyticalSession
}

func (s *ceStore) FindAccountBySession(_ context.Context, token string, _, _ time.Time) (application.Account, bool, error) {
	state := application.AccountActive
	if token == "pending" {
		state = application.AccountPendingActivation
	}
	return application.Account{ID: ceEmployeeID, DisplayName: "New analyst", State: state, Role: application.RoleEmployee}, token == "active" || token == "pending", nil
}
func (s *ceStore) GetAnalyticalSession(_ context.Context, id string) (application.AnalyticalSession, bool, error) {
	if s.failure {
		return application.AnalyticalSession{}, false, errors.New("private storage diagnostic")
	}
	return s.source, !s.missing && id == s.source.ID, nil
}
func (s *ceStore) FindAnalyticalSessionReplay(_ context.Context, input application.AnalyticalSessionIdempotency) (application.AnalyticalSession, bool, error) {
	saved, found := s.replay[input.AccountID+"|"+input.Endpoint+"|"+input.IdempotencyKey]
	if found && saved.RequestHash != input.RequestHash {
		return application.AnalyticalSession{}, false, application.ErrIdempotencyConflict
	}
	return saved.Session, found, nil
}
func (s *ceStore) ResolveSessionSelection(_ context.Context, input application.CreateSessionInput, _ time.Time) (application.ResolvedSessionSelection, error) {
	regions := make([]application.SessionRegion, 0, len(input.RegionCodes))
	complexes := make([]application.SessionComplex, 0, len(input.ResidentialComplexIDs))
	for _, code := range input.RegionCodes {
		regions = append(regions, application.SessionRegion{Code: code, Name: "Current region"})
	}
	for _, id := range input.ResidentialComplexIDs {
		complexes = append(complexes, application.SessionComplex{ID: id, RegionCode: "77", Name: "Current complex"})
	}
	return application.ResolvedSessionSelection{Regions: regions, Complexes: complexes}, nil
}
func (s *ceStore) CreateAnalyticalSession(_ context.Context, input application.NewAnalyticalSession) (application.AnalyticalSession, bool, error) {
	key := input.AccountID + "|" + input.Endpoint + "|" + input.IdempotencyKey
	if old, found := s.replay[key]; found {
		return old.Session, true, nil
	}
	s.created = append(s.created, input)
	s.replay[key] = input
	return input.Session, false, nil
}
func ceFixture() (*ceStore, http.Handler) {
	count := int64(175)
	source := application.AnalyticalSession{
		ID: ceSourceID, Name: "Old, \"quoted\"\nname", Status: "complete", CreatedAt: ceNow.Add(-24 * time.Hour),
		CreatedByID: ceOtherID, CreatedByDisplayName: "Old analyst",
		RegionCodes: []string{"77"}, Regions: []string{"Old region"},
		ResidentialComplexIDs: []string{"1001"}, ResidentialComplexes: []string{"Old complex"},
		SegmentCodes: []string{"residential"}, Segments: []string{"Residential"},
		ObservationIDs: []string{ceOtherID}, CurrentDashboard: &application.CurrentDashboard{SourceLabel: "Текущий срез ЕИСЖС", CumulativeDDUCount: &count},
	}
	store := &ceStore{source: source, replay: map[string]application.NewAnalyticalSession{}}
	access := application.NewAccessService(store, ceCredentials{})
	sessions := application.NewAnalyticalSessionService(access, store, func() time.Time { return ceNow })
	return store, httpapi.NewHandler(httpapi.Dependencies{Access: access, Sessions: sessions, Readiness: application.NewReadinessService(ceProbe{})})
}

type ceProbe struct{}

func (ceProbe) Check(context.Context) error { return errors.New("infrastructure offline") }
func ceRequest(h http.Handler, method, path, cookie, key, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: "ad_session", Value: cookie})
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	result := httptest.NewRecorder()
	h.ServeHTTP(result, request)
	return result
}
func ceJSON(t *testing.T, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v; body=%s", err, r.Body.String())
	}
	return body
}
func ceError(t *testing.T, r *httptest.ResponseRecorder, status int) {
	t.Helper()
	if r.Code != status {
		t.Fatalf("status=%d want=%d body=%s", r.Code, status, r.Body.String())
	}
	body := ceJSON(t, r)
	detail, ok := body["error"].(map[string]any)
	if !ok || detail["code"] == nil {
		t.Fatalf("missing public error envelope: %v", body)
	}
	if strings.Contains(r.Body.String(), "private storage diagnostic") {
		t.Fatal("storage details leaked")
	}
}
func TestSeminarFixtureControl(t *testing.T) {
	store, h := ceFixture()
	r := ceRequest(h, "GET", "/api/v1/sessions/"+ceSourceID, "active", "", "")
	if r.Code != 200 || ceJSON(t, r)["id"] != ceSourceID {
		t.Fatalf("existing GET failed: %d %s", r.Code, r.Body)
	}
	payload := `{"regionCodes":["77"],"residentialComplexIds":["1001"],"segmentCodes":["residential"]}`
	r = ceRequest(h, "POST", "/api/v1/sessions", "active", "control", payload)
	if r.Code != 201 || len(store.created) != 1 {
		t.Fatalf("existing create failed: %d %s", r.Code, r.Body)
	}
	if !reflect.DeepEqual(store.created[0].Session.RegionCodes, store.source.RegionCodes) {
		t.Fatal("fixture selected different region")
	}
}

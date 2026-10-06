package eiszhs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/eiszhs"
	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

//nolint:gocyclo,funlen // The fixture validates every request field at the public HTTP boundary.
func TestClientReusesOAuthAndGatesEveryBusinessPage(t *testing.T) {
	t.Parallel()
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
		case "/complexes":
			if request.Header.Get("Authorization") != "Bearer token" ||
				request.URL.Query().Get("region-code") != "77" || request.URL.Query().Get("size") != "50" {
				t.Fatalf("unexpected complexes request: %s %#v", request.URL, request.Header)
			}
			page := request.URL.Query().Get("page")
			firstID, count := 1, 50
			if page == "1" {
				firstID, count = 51, 1
			}
			items := make([]map[string]any, 0, count)
			for id := firstID; id < firstID+count; id++ {
				items = append(items, map[string]any{"complexId": id, "fullName": fmt.Sprintf("ЖК %02d", id)})
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"total": 51, "limitItems": 50,
				"elements": map[string]any{"complexShortInfo": items},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	gate := &countingGate{}
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), gate, newMemoryCheckpoint())

	items, err := client.Complexes(t.Context(), "77")
	if err != nil {
		t.Fatalf("list complexes: %v", err)
	}
	if len(items) != 51 || items[0].ID != "1" || tokenCalls.Load() != 1 || gate.calls.Load() != 2 {
		t.Fatalf("items=%#v token calls=%d gate calls=%d", items, tokenCalls.Load(), gate.calls.Load())
	}
}

func TestClientReadsOneRawComplexMembershipPage(t *testing.T) {
	t.Parallel()
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		requestPath = request.URL.RequestURI()
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = response.Write([]byte(`{"total":1,"limitItems":50,"elements":{"constructionObjectShortInfo":[{"objectId":2001}]}}`))
	}))
	defer server.Close()
	gate := &countingGate{}
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), gate, newMemoryCheckpoint())

	response, err := client.ComplexConstructionObjects(t.Context(), "1001", 0)
	if err != nil {
		t.Fatalf("ComplexConstructionObjects: %v", err)
	}
	if requestPath != "/complexes/1001/construction-objects?page=0&size=50" ||
		string(response.Body) != `{"total":1,"limitItems":50,"elements":{"constructionObjectShortInfo":[{"objectId":2001}]}}` ||
		response.ContentType != "application/json; charset=utf-8" ||
		response.SourceAddress != server.URL+"/complexes/1001/construction-objects?page=0&size=50" ||
		gate.calls.Load() != 1 {
		t.Fatalf("path=%q response=%#v gate=%d", requestPath, response, gate.calls.Load())
	}
}

func TestClientReadsOneRawCurrentExtendedObject(t *testing.T) {
	t.Parallel()
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		requestPath = request.URL.RequestURI()
		_, _ = response.Write([]byte(`{"objectId":2001,"dduInfo":{"dduLivNumber":17}}`))
	}))
	defer server.Close()
	gate := &countingGate{}
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), gate, newMemoryCheckpoint())

	response, err := client.ConstructionObjectExtended(t.Context(), "2001")
	if err != nil || requestPath != "/construction-objects/2001/extended" ||
		string(response.Body) != `{"objectId":2001,"dduInfo":{"dduLivNumber":17}}` || gate.calls.Load() != 1 {
		t.Fatalf("path=%q response=%#v error=%v gate=%d", requestPath, response, err, gate.calls.Load())
	}
}

func TestClientPreservesAcceptedRawBoundaryAndRejectsOversizeResponse(t *testing.T) {
	t.Parallel()
	var businessCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		if businessCalls.Add(1) == 1 {
			_, _ = response.Write([]byte("1234"))
			return
		}
		_, _ = response.Write([]byte("12345"))
	}))
	defer server.Close()
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
		MaxRawResponseBytes: 4,
	}, server.Client(), &countingGate{}, newMemoryCheckpoint())

	accepted, err := client.ConstructionObjectExtended(t.Context(), "2001")
	if err != nil || string(accepted.Body) != "1234" {
		t.Fatalf("accepted=%#v error=%v, want exact boundary bytes", accepted, err)
	}
	oversize, err := client.ConstructionObjectExtended(t.Context(), "2001")
	if err == nil || !strings.Contains(err.Error(), "exceeds") || len(oversize.Body) != 0 {
		t.Fatalf("oversize=%#v error=%v, want rejected untruncated response", oversize, err)
	}
}

//nolint:funlen // Table fixtures keep classification and exact call counts together.
func TestCurrentEndpointClassifiesFailureWithoutBusinessRetry(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name       string
		status     int
		retryable  bool
		retryAfter time.Duration
	}{
		{name: "permanent bad request", status: http.StatusBadRequest},
		{name: "rate limited", status: http.StatusTooManyRequests, retryable: true, retryAfter: 5 * time.Second},
		{name: "internal error", status: http.StatusInternalServerError, retryable: true},
		{name: "temporarily unavailable", status: http.StatusServiceUnavailable, retryable: true},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/token" {
					_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
					return
				}
				calls.Add(1)
				if fixture.retryAfter > 0 {
					response.Header().Set("Retry-After", "5")
				}
				response.WriteHeader(fixture.status)
			}))
			defer server.Close()
			gate := &countingGate{}
			client := eiszhs.New(eiszhs.Settings{
				APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
			}, server.Client(), gate, newMemoryCheckpoint())

			_, err := client.ConstructionObjectExtended(t.Context(), "2001")
			var requestErr *eiszhs.CurrentRequestError
			if !errors.As(err, &requestErr) || requestErr.StatusCode != fixture.status ||
				requestErr.Retryable != fixture.retryable || requestErr.RetryAfter != fixture.retryAfter ||
				calls.Load() != 1 || gate.calls.Load() != 1 {
				t.Fatalf("error=%#v calls=%d gate=%d", requestErr, calls.Load(), gate.calls.Load())
			}
		})
	}
}

func TestCurrentEndpointClassifiesNetworkFailureWithoutBusinessRetry(t *testing.T) {
	t.Parallel()
	var businessCalls atomic.Int32
	oauth := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
	}))
	defer oauth.Close()
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == strings.TrimPrefix(oauth.URL, "http://") {
			return http.DefaultTransport.RoundTrip(request)
		}
		businessCalls.Add(1)
		return nil, errors.New("connection reset by peer")
	})}
	gate := &countingGate{}
	client := eiszhs.New(eiszhs.Settings{
		APIURL: "http://source.invalid", OAuthURL: oauth.URL, ClientID: "id", ClientSecret: "secret",
	}, httpClient, gate, newMemoryCheckpoint())

	_, err := client.ConstructionObjectExtended(t.Context(), "2001")
	var requestErr *eiszhs.CurrentRequestError
	if !errors.As(err, &requestErr) || !requestErr.Retryable || requestErr.StatusCode != 0 ||
		businessCalls.Load() != 1 || gate.calls.Load() != 1 {
		t.Fatalf("error=%#v calls=%d gate=%d", requestErr, businessCalls.Load(), gate.calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

//nolint:funlen,gocyclo // Two invocations keep token, gate, and call counts auditable together.
func TestCurrentEndpointDefersOAuthRefreshToNextInvocationAfterUnauthorized(t *testing.T) {
	t.Parallel()
	var tokenCalls atomic.Int32
	var businessCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			call := tokenCalls.Add(1)
			_, _ = fmt.Fprintf(response,
				`{"access_token":"token-%d","token_type":"Bearer","expires_in":300}`, call)
			return
		}
		if businessCalls.Add(1) == 1 {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = response.Write([]byte(`{"objectId":2001,"dduInfo":{"dduLivNumber":17,"dduNonlivNumber":4,"dduParkingNumber":2}}`))
	}))
	defer server.Close()
	gate := &countingGate{}
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), gate, newMemoryCheckpoint())

	first, err := client.ConstructionObjectExtended(t.Context(), "2001")
	var requestErr *eiszhs.CurrentRequestError
	if !errors.As(err, &requestErr) || requestErr.StatusCode != http.StatusUnauthorized ||
		len(first.Body) != 0 || tokenCalls.Load() != 1 || businessCalls.Load() != 1 || gate.calls.Load() != 1 {
		t.Fatalf("first=%#v error=%#v token=%d business=%d gate=%d",
			first, requestErr, tokenCalls.Load(), businessCalls.Load(), gate.calls.Load())
	}
	second, err := client.ConstructionObjectExtended(t.Context(), "2001")
	if err != nil || len(second.Body) == 0 || tokenCalls.Load() != 2 ||
		businessCalls.Load() != 2 || gate.calls.Load() != 2 {
		t.Fatalf("second=%#v error=%v token=%d business=%d gate=%d",
			second, err, tokenCalls.Load(), businessCalls.Load(), gate.calls.Load())
	}
}

func TestNormalizerRejectsIncompleteCurrentCategoriesAndMismatchedObject(t *testing.T) {
	t.Parallel()
	normalizer := eiszhs.NewNormalizer()
	fixtures := map[string]string{
		"missing dduInfo":         `{"objectId":2001}`,
		"missing residential":     `{"objectId":2001,"dduInfo":{"dduNonlivNumber":4,"dduParkingNumber":2}}`,
		"missing non-residential": `{"objectId":2001,"dduInfo":{"dduLivNumber":17,"dduParkingNumber":2}}`,
		"missing parking":         `{"objectId":2001,"dduInfo":{"dduLivNumber":17,"dduNonlivNumber":4}}`,
		"mismatched object":       `{"objectId":2002,"dduInfo":{"dduLivNumber":17,"dduNonlivNumber":4,"dduParkingNumber":2}}`,
	}
	for name, raw := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			metrics, err := normalizer.NormalizeCurrentExtended([]byte(raw), "2001")
			if err == nil || metrics != nil {
				t.Fatalf("metrics=%#v error=%v, want rejected incomplete response", metrics, err)
			}
		})
	}
}

func TestNormalizerRequiresAuthoritativeMembershipFields(t *testing.T) {
	t.Parallel()
	normalizer := eiszhs.NewNormalizer()
	valid := []byte(`{"total":1,"limitItems":50,"elements":{"constructionObjectShortInfo":[{"objectId":2001}]}}`)
	membership, err := normalizer.NormalizeComplexMembership(valid)
	if err != nil || membership.Total != 1 || membership.PageSize != 50 ||
		len(membership.ObjectIDs) != 1 || membership.ObjectIDs[0] != "2001" {
		t.Fatalf("membership=%#v error=%v", membership, err)
	}
	for name, raw := range map[string]string{
		"missing total":     `{"limitItems":50,"elements":{"constructionObjectShortInfo":[]}}`,
		"missing page size": `{"total":0,"elements":{"constructionObjectShortInfo":[]}}`,
		"missing elements":  `{"total":0,"limitItems":50}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if result, normalizeErr := normalizer.NormalizeComplexMembership([]byte(raw)); normalizeErr == nil || len(result.ObjectIDs) != 0 {
				t.Fatalf("membership=%#v error=%v, want rejected response", result, normalizeErr)
			}
		})
	}
}

func TestNormalizerPreservesDuplicateMembershipEntriesForTotalVerification(t *testing.T) {
	t.Parallel()
	normalizer := eiszhs.NewNormalizer()
	membership, err := normalizer.NormalizeComplexMembership([]byte(
		`{"total":2,"limitItems":50,"elements":{"constructionObjectShortInfo":[{"objectId":2001},{"objectId":2001}]}}`,
	))
	if err != nil || len(membership.ObjectIDs) != 2 || membership.ObjectIDs[0] != membership.ObjectIDs[1] {
		t.Fatalf("duplicate membership=%#v error=%v", membership, err)
	}
}

func TestClientRejectsIncompletePagination(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		id := 1
		if request.URL.Query().Get("page") == "1" {
			id = 2
		}
		_, _ = fmt.Fprintf(response,
			`{"total":51,"limitItems":50,"elements":{"complexShortInfo":[{"complexId":%d,"fullName":"ЖК"}]}}`,
			id)
	}))
	defer server.Close()
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), &countingGate{}, newMemoryCheckpoint())

	items, err := client.Complexes(t.Context(), "77")
	if err == nil || !strings.Contains(err.Error(), "total") || items != nil {
		t.Fatalf("items=%#v err=%v, want incomplete total error", items, err)
	}
}

//nolint:funlen,gocyclo // Two source generations prove invalid pages cannot poison a healthy retry.
func TestClientDiscardsIncompleteCheckpointBeforeHealthyRetry(t *testing.T) {
	t.Parallel()
	var healthy atomic.Bool
	var pageZeroCalls atomic.Int32
	var pageOneCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		if request.URL.Query().Get("page") == "0" {
			pageZeroCalls.Add(1)
			if healthy.Load() {
				writeComplexPage(t, response, 51, 1, 50)
				return
			}
			writeComplexPage(t, response, 51, 1, 1)
			return
		}
		pageOneCalls.Add(1)
		if healthy.Load() {
			writeComplexPage(t, response, 51, 51, 1)
			return
		}
		writeComplexPage(t, response, 51, 2, 1)
	}))
	defer server.Close()
	checkpoint := newMemoryCheckpoint()
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), &countingGate{}, checkpoint)

	if items, err := client.Complexes(t.Context(), "77"); err == nil || items != nil {
		t.Fatalf("incomplete items=%#v err=%v, want integrity failure", items, err)
	}
	healthy.Store(true)
	items, err := client.Complexes(t.Context(), "77")
	if err != nil || len(items) != 51 || checkpoint.clearCalls.Load() != 2 ||
		pageZeroCalls.Load() != 3 || pageOneCalls.Load() != 3 {
		t.Fatalf("healthy items=%d err=%v clears=%d page calls=%d/%d",
			len(items), err, checkpoint.clearCalls.Load(), pageZeroCalls.Load(), pageOneCalls.Load())
	}
}

func TestClientRefreshesTokenOnceAfterUnauthorized(t *testing.T) {
	t.Parallel()
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			call := tokenCalls.Add(1)
			_, _ = fmt.Fprintf(response,
				`{"access_token":"token-%d","token_type":"Bearer","expires_in":300}`, call)
			return
		}
		if request.Header.Get("Authorization") == "Bearer token-1" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = response.Write([]byte(
			`{"total":1,"limitItems":50,"elements":{"complexShortInfo":[{"complexId":1,"fullName":"ЖК"}]}}`,
		))
	}))
	defer server.Close()
	gate := &countingGate{}
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), gate, newMemoryCheckpoint())

	items, err := client.Complexes(t.Context(), "77")
	if err != nil || len(items) != 1 || tokenCalls.Load() != 2 || gate.calls.Load() != 2 {
		t.Fatalf("items=%#v err=%v token calls=%d gate calls=%d", items, err, tokenCalls.Load(), gate.calls.Load())
	}
}

func TestClientReportsPersistentUnauthorizedAsUnavailable(t *testing.T) {
	t.Parallel()
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			call := tokenCalls.Add(1)
			_, _ = fmt.Fprintf(response,
				`{"access_token":"token-%d","token_type":"Bearer","expires_in":300}`, call)
			return
		}
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	gate := &countingGate{}
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), gate, newMemoryCheckpoint())

	items, err := client.Complexes(t.Context(), "77")
	if err == nil || !strings.Contains(err.Error(), "unauthorized") || items != nil ||
		tokenCalls.Load() != 2 || gate.calls.Load() != 2 {
		t.Fatalf("items=%#v err=%v token calls=%d gate calls=%d",
			items, err, tokenCalls.Load(), gate.calls.Load())
	}
}

//nolint:funlen // The two-call fixture proves the quota-bearing first page is reused after failure.
func TestClientResumesFromDurablePagesAfterLaterPageFails(t *testing.T) {
	t.Parallel()
	var pageZeroCalls atomic.Int32
	var pageOneCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		page := request.URL.Query().Get("page")
		if page == "0" {
			pageZeroCalls.Add(1)
			writeComplexPage(t, response, 51, 1, 50)
			return
		}
		if pageOneCalls.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeComplexPage(t, response, 51, 51, 1)
	}))
	defer server.Close()
	checkpoint := newMemoryCheckpoint()
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), &countingGate{}, checkpoint)

	if items, err := client.Complexes(t.Context(), "77"); err == nil || items != nil {
		t.Fatalf("first items=%#v err=%v, want later-page failure", items, err)
	}
	if len(checkpoint.pages["77"]) != 1 {
		t.Fatalf("checkpoint pages=%#v, want durable first page", checkpoint.pages["77"])
	}
	items, err := client.Complexes(t.Context(), "77")
	if err != nil || len(items) != 51 || pageZeroCalls.Load() != 1 || pageOneCalls.Load() != 2 {
		t.Fatalf("items=%d err=%v page calls=%d/%d",
			len(items), err, pageZeroCalls.Load(), pageOneCalls.Load())
	}
}

//nolint:funlen,gocyclo // The second invocation proves one bounded restart after a changed total.
func TestClientRestartsOnceWhenResumedCatalogTotalChanged(t *testing.T) {
	t.Parallel()
	var changed atomic.Bool
	var pageZeroCalls atomic.Int32
	var pageOneCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		if request.URL.Query().Get("page") == "0" {
			pageZeroCalls.Add(1)
			total := 51
			if changed.Load() {
				total = 52
			}
			writeComplexPage(t, response, total, 1, 50)
			return
		}
		if pageOneCalls.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeComplexPage(t, response, 52, 51, 2)
	}))
	defer server.Close()
	checkpoint := newMemoryCheckpoint()
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), &countingGate{}, checkpoint)

	if items, err := client.Complexes(t.Context(), "77"); err == nil || items != nil {
		t.Fatalf("transport-failed items=%#v err=%v", items, err)
	}
	changed.Store(true)
	items, err := client.Complexes(t.Context(), "77")
	if err != nil || len(items) != 52 || checkpoint.clearCalls.Load() != 1 ||
		pageZeroCalls.Load() != 2 || pageOneCalls.Load() != 3 {
		t.Fatalf("restarted items=%d err=%v clears=%d page calls=%d/%d",
			len(items), err, checkpoint.clearCalls.Load(), pageZeroCalls.Load(), pageOneCalls.Load())
	}
}

//nolint:funlen,gocyclo // A rejected page generation must not mix with the next healthy generation.
func TestClientDiscardsCheckpointWhenNewPageIsSemanticallyInvalid(t *testing.T) {
	t.Parallel()
	var healthy atomic.Bool
	var pageZeroCalls atomic.Int32
	var pageOneCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, _ = response.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":300}`))
			return
		}
		if request.URL.Query().Get("page") == "0" {
			pageZeroCalls.Add(1)
			total := 51
			if healthy.Load() {
				total = 52
			}
			writeComplexPage(t, response, total, 1, 50)
			return
		}
		pageOneCalls.Add(1)
		if healthy.Load() {
			writeComplexPage(t, response, 52, 51, 2)
			return
		}
		_, _ = response.Write([]byte(
			`{"total":51,"limitItems":49,"elements":{"complexShortInfo":[{"complexId":51,"fullName":"ЖК 51"}]}}`,
		))
	}))
	defer server.Close()
	checkpoint := newMemoryCheckpoint()
	client := eiszhs.New(eiszhs.Settings{
		APIURL: server.URL, OAuthURL: server.URL + "/token", ClientID: "id", ClientSecret: "secret",
	}, server.Client(), &countingGate{}, checkpoint)

	if items, err := client.Complexes(t.Context(), "77"); err == nil || items != nil {
		t.Fatalf("semantic-invalid items=%#v err=%v", items, err)
	}
	healthy.Store(true)
	items, err := client.Complexes(t.Context(), "77")
	if err != nil || len(items) != 52 || checkpoint.clearCalls.Load() != 2 ||
		pageZeroCalls.Load() != 3 || pageOneCalls.Load() != 3 {
		t.Fatalf("healthy items=%d err=%v clears=%d page calls=%d/%d",
			len(items), err, checkpoint.clearCalls.Load(), pageZeroCalls.Load(), pageOneCalls.Load())
	}
}

type countingGate struct {
	calls atomic.Int32
}

func (gate *countingGate) WaitForEISZHSRequest(context.Context) error {
	gate.calls.Add(1)
	return nil
}

type memoryCheckpoint struct {
	mu         sync.Mutex
	pages      map[string][]application.ComplexCatalogPage
	clearCalls atomic.Int32
}

func newMemoryCheckpoint() *memoryCheckpoint {
	return &memoryCheckpoint{pages: make(map[string][]application.ComplexCatalogPage)}
}

func (checkpoint *memoryCheckpoint) LoadComplexCatalogPages(
	_ context.Context,
	regionCode string,
) ([]application.ComplexCatalogPage, error) {
	checkpoint.mu.Lock()
	defer checkpoint.mu.Unlock()
	return append([]application.ComplexCatalogPage(nil), checkpoint.pages[regionCode]...), nil
}

func (checkpoint *memoryCheckpoint) SaveComplexCatalogPage(
	_ context.Context,
	page application.ComplexCatalogPage,
) error {
	checkpoint.mu.Lock()
	defer checkpoint.mu.Unlock()
	checkpoint.pages[page.RegionCode] = append(checkpoint.pages[page.RegionCode], page)
	return nil
}

func (checkpoint *memoryCheckpoint) ClearComplexCatalogPages(
	_ context.Context,
	regionCode string,
) error {
	checkpoint.mu.Lock()
	defer checkpoint.mu.Unlock()
	delete(checkpoint.pages, regionCode)
	checkpoint.clearCalls.Add(1)
	return nil
}

func writeComplexPage(t *testing.T, response http.ResponseWriter, total int, firstID int, count int) {
	t.Helper()
	items := make([]map[string]any, 0, count)
	for id := firstID; id < firstID+count; id++ {
		items = append(items, map[string]any{"complexId": id, "fullName": fmt.Sprintf("ЖК %02d", id)})
	}
	if err := json.NewEncoder(response).Encode(map[string]any{
		"total": total, "limitItems": 50,
		"elements": map[string]any{"complexShortInfo": items},
	}); err != nil {
		t.Fatalf("encode complex page: %v", err)
	}
}

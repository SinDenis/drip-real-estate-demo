// Package eiszhs adapts the licensed read-only ЕИСЖС HTTP API.
package eiszhs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

const (
	pageSize                  = 50
	defaultMaxRawResponseSize = int64(16 << 20)
)

var errInvalidCheckpointGeneration = errors.New("invalid ЕИСЖС complex checkpoint generation")

// RequestGate coordinates quota-bearing calls across application replicas.
type RequestGate interface {
	WaitForEISZHSRequest(context.Context) error
}

// PageCheckpoint makes every successful quota-bearing page reusable after a later failure.
type PageCheckpoint interface {
	LoadComplexCatalogPages(context.Context, string) ([]application.ComplexCatalogPage, error)
	SaveComplexCatalogPage(context.Context, application.ComplexCatalogPage) error
	ClearComplexCatalogPages(context.Context, string) error
}

// Settings contains the explicit source contract and credential pair.
type Settings struct {
	APIURL              string
	OAuthURL            string
	ClientID            string
	ClientSecret        string
	MaxRawResponseBytes int64
}

// Client reads residential complex catalogs with a reused OAuth token.
type Client struct {
	settings   Settings
	httpClient *http.Client
	gate       RequestGate
	checkpoint PageCheckpoint
	now        func() time.Time
	tokenMu    sync.Mutex
	token      string
	tokenUntil time.Time
	maxRawSize int64
}

// ComplexConstructionObjects reads one raw authoritative membership page for a selected ЖК.
func (client *Client) ComplexConstructionObjects(
	ctx context.Context,
	complexID string,
	page int,
) (application.CurrentSourceResponse, error) {
	return client.currentRawGET(
		ctx,
		"/complexes/"+url.PathEscape(complexID)+"/construction-objects",
		url.Values{"page": {strconv.Itoa(page)}, "size": {strconv.Itoa(pageSize)}},
	)
}

// ConstructionObjectExtended reads one raw current extended construction object response.
func (client *Client) ConstructionObjectExtended(
	ctx context.Context,
	objectID string,
) (application.CurrentSourceResponse, error) {
	return client.currentRawGET(
		ctx,
		"/construction-objects/"+url.PathEscape(objectID)+"/extended",
		nil,
	)
}

func (client *Client) currentRawGET(
	ctx context.Context,
	requestPath string,
	query url.Values,
) (application.CurrentSourceResponse, error) {
	token, err := client.accessToken(ctx, false)
	if err != nil {
		return application.CurrentSourceResponse{}, err
	}
	result, err := client.currentRawAttempt(ctx, requestPath, query, token)
	var requestErr *CurrentRequestError
	if errors.As(err, &requestErr) && requestErr.StatusCode == http.StatusUnauthorized {
		// One collect owns exactly one business attempt. Clearing the rejected token lets
		// the next collect acquire fresh OAuth credentials before its own single attempt.
		client.discardToken(token)
	}
	return result, err
}

// currentRawAttempt performs one quota-bearing HTTP attempt. DEN-54 does not retry 429,
// transient server responses, or network failures inside a collect call.
//
//nolint:funlen // Gate, request, classification, and bounded raw read are one transport attempt.
func (client *Client) currentRawAttempt(
	ctx context.Context,
	requestPath string,
	query url.Values,
	token string,
) (application.CurrentSourceResponse, error) {
	if err := client.gate.WaitForEISZHSRequest(ctx); err != nil {
		return application.CurrentSourceResponse{}, err
	}
	endpoint, err := url.Parse(strings.TrimRight(client.settings.APIURL, "/") + requestPath)
	if err != nil {
		return application.CurrentSourceResponse{}, fmt.Errorf("parse ЕИСЖС current URL: %w", err)
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return application.CurrentSourceResponse{}, fmt.Errorf("create ЕИСЖС current request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return application.CurrentSourceResponse{}, &CurrentRequestError{Retryable: true, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return application.CurrentSourceResponse{}, classifyCurrentStatus(
			response.StatusCode, response.Header.Get("Retry-After"), client.now(),
		)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, client.maxRawSize+1))
	if err != nil {
		return application.CurrentSourceResponse{}, fmt.Errorf("read ЕИСЖС current data: %w", err)
	}
	if int64(len(body)) > client.maxRawSize {
		return application.CurrentSourceResponse{}, fmt.Errorf(
			"ЕИСЖС current response exceeds %d bytes", client.maxRawSize,
		)
	}
	return application.CurrentSourceResponse{
		Body: body, ContentType: response.Header.Get("Content-Type"), SourceAddress: endpoint.String(),
	}, nil
}

func (client *Client) discardToken(token string) {
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	if client.token == token {
		client.token = ""
		client.tokenUntil = time.Time{}
	}
}

// New creates a read-only ЕИСЖС client.
func New(
	settings Settings,
	httpClient *http.Client,
	gate RequestGate,
	checkpoint PageCheckpoint,
) *Client {
	maxRawSize := settings.MaxRawResponseBytes
	if maxRawSize <= 0 {
		maxRawSize = defaultMaxRawResponseSize
	}
	return &Client{
		settings: settings, httpClient: httpClient, gate: gate, checkpoint: checkpoint, now: time.Now,
		maxRawSize: maxRawSize,
	}
}

// Complexes returns all pages of the selected region's ЖК catalog.
func (client *Client) Complexes(
	ctx context.Context,
	regionCode string,
) ([]application.ResidentialComplex, error) {
	token, err := client.accessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	oauthRefreshed := false
	generationRestarted := false
	for {
		items, unauthorized, pagesErr := client.complexPages(ctx, regionCode, token)
		if errors.Is(pagesErr, errInvalidCheckpointGeneration) && !generationRestarted {
			generationRestarted = true
			continue
		}
		if !unauthorized {
			return items, pagesErr
		}
		if oauthRefreshed {
			return nil, errors.New("ЕИСЖС complexes remained unauthorized after OAuth refresh")
		}
		token, err = client.accessToken(ctx, true)
		if err != nil {
			return nil, err
		}
		oauthRefreshed = true
	}
}

//nolint:funlen,gocognit,gocyclo // Pagination, checkpoints, validation, and ordering form one seam.
func (client *Client) complexPages(
	ctx context.Context,
	regionCode string,
	token string,
) ([]application.ResidentialComplex, bool, error) {
	byID := make(map[string]application.ResidentialComplex)
	savedPages, err := client.checkpoint.LoadComplexCatalogPages(ctx, regionCode)
	if err != nil {
		return nil, false, fmt.Errorf("load ЕИСЖС complex page checkpoints: %w", err)
	}
	byPage := make(map[int]application.ComplexCatalogPage, len(savedPages))
	for _, saved := range savedPages {
		byPage[saved.Page] = saved
	}
	var expectedTotal int64 = -1
	for page := 0; ; page++ {
		checkpoint, found := byPage[page]
		if !found {
			response, unauthorized, pageErr := client.complexPage(ctx, regionCode, page, token)
			if pageErr != nil || unauthorized {
				return nil, unauthorized, pageErr
			}
			checkpoint, pageErr = normalizePage(regionCode, page, response)
			if pageErr != nil {
				return nil, false, client.discardInvalidPages(ctx, regionCode, pageErr)
			}
			if pageErr := client.checkpoint.SaveComplexCatalogPage(ctx, checkpoint); pageErr != nil {
				return nil, false, fmt.Errorf("save ЕИСЖС complex page checkpoint: %w", pageErr)
			}
		}
		if checkpoint.RegionCode != regionCode || checkpoint.Page != page ||
			checkpoint.Total < 0 || checkpoint.PageSize != pageSize {
			integrityErr := fmt.Errorf(
				"ЕИСЖС complexes returned total %d and page size %d, want non-negative total and size %d",
				checkpoint.Total, checkpoint.PageSize, pageSize,
			)
			return nil, false, client.discardInvalidPages(ctx, regionCode, integrityErr)
		}
		if expectedTotal < 0 {
			expectedTotal = checkpoint.Total
		} else if checkpoint.Total != expectedTotal {
			integrityErr := fmt.Errorf(
				"ЕИСЖС complexes total changed from %d to %d during pagination",
				expectedTotal, checkpoint.Total,
			)
			return nil, false, client.discardInvalidPages(ctx, regionCode, integrityErr)
		}
		for _, item := range checkpoint.Items {
			if item.ID == "" || item.ID == "0" || strings.TrimSpace(item.Name) == "" {
				integrityErr := errors.New("ЕИСЖС complex response lacks a stable ID or full name")
				return nil, false, client.discardInvalidPages(ctx, regionCode, integrityErr)
			}
			byID[item.ID] = item
		}
		if int64((page+1)*pageSize) >= expectedTotal {
			break
		}
	}
	if int64(len(byID)) != expectedTotal {
		integrityErr := fmt.Errorf(
			"ЕИСЖС complexes returned %d unique items for total %d",
			len(byID), expectedTotal,
		)
		return nil, false, client.discardInvalidPages(ctx, regionCode, integrityErr)
	}
	items := make([]application.ResidentialComplex, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].Name == items[right].Name {
			return items[left].ID < items[right].ID
		}
		return items[left].Name < items[right].Name
	})
	return items, false, nil
}

func (client *Client) discardInvalidPages(
	ctx context.Context,
	regionCode string,
	cause error,
) error {
	if err := client.checkpoint.ClearComplexCatalogPages(ctx, regionCode); err != nil {
		return errors.Join(cause, fmt.Errorf("clear invalid ЕИСЖС complex page checkpoints: %w", err))
	}
	return errors.Join(errInvalidCheckpointGeneration, cause)
}

func normalizePage(
	regionCode string,
	page int,
	response complexPage,
) (application.ComplexCatalogPage, error) {
	if response.Total < 0 || response.LimitItems != pageSize {
		return application.ComplexCatalogPage{}, fmt.Errorf(
			"ЕИСЖС complexes returned total %d and page size %d, want non-negative total and size %d",
			response.Total, response.LimitItems, pageSize,
		)
	}
	checkpoint := application.ComplexCatalogPage{
		RegionCode: regionCode, Page: page, PageSize: pageSize, Total: response.Total,
		Items: make([]application.ResidentialComplex, 0, len(response.Elements.Complexes)),
	}
	for _, item := range response.Elements.Complexes {
		id := strconv.FormatInt(item.ID, 10)
		name := strings.TrimSpace(item.FullName)
		if id == "0" || name == "" {
			return application.ComplexCatalogPage{}, errors.New(
				"ЕИСЖС complex response lacks a stable ID or full name",
			)
		}
		checkpoint.Items = append(checkpoint.Items, application.ResidentialComplex{ID: id, Name: name})
	}
	return checkpoint, nil
}

type complexPage struct {
	Total      int64 `json:"total"`
	LimitItems int   `json:"limitItems"`
	Elements   struct {
		Complexes []struct {
			ID       int64  `json:"complexId"`
			FullName string `json:"fullName"`
		} `json:"complexShortInfo"`
	} `json:"elements"`
}

//nolint:funlen // The request gate and the single quota-bearing HTTP call stay visibly adjacent.
func (client *Client) complexPage(
	ctx context.Context,
	regionCode string,
	page int,
	token string,
) (complexPage, bool, error) {
	if err := client.gate.WaitForEISZHSRequest(ctx); err != nil {
		return complexPage{}, false, err
	}
	endpoint, err := url.Parse(strings.TrimRight(client.settings.APIURL, "/") + "/complexes")
	if err != nil {
		return complexPage{}, false, fmt.Errorf("parse ЕИСЖС complexes URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("region-code", regionCode)
	query.Set("page", strconv.Itoa(page))
	query.Set("size", strconv.Itoa(pageSize))
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return complexPage{}, false, fmt.Errorf("create ЕИСЖС complexes request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return complexPage{}, false, fmt.Errorf("request ЕИСЖС complexes: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, response.Body)
		return complexPage{}, true, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return complexPage{}, false, fmt.Errorf("ЕИСЖС complexes returned HTTP %d", response.StatusCode)
	}
	var payload complexPage
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return complexPage{}, false, fmt.Errorf("decode ЕИСЖС complexes: %w", err)
	}
	return payload, false, nil
}

//nolint:funlen // Token cache and refresh share one mutex-protected critical section.
func (client *Client) accessToken(ctx context.Context, force bool) (string, error) {
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	now := client.now().UTC()
	if !force && client.token != "" && now.Before(client.tokenUntil) {
		return client.token, nil
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client.settings.ClientID},
		"client_secret": {client.settings.ClientSecret},
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, client.settings.OAuthURL, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("create ЕИСЖС OAuth request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request ЕИСЖС OAuth token: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return "", fmt.Errorf("ЕИСЖС OAuth returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode ЕИСЖС OAuth token: %w", err)
	}
	if payload.AccessToken == "" || !strings.EqualFold(payload.TokenType, "bearer") {
		return "", errors.New("ЕИСЖС OAuth response lacks a bearer token")
	}
	lifetime := time.Duration(payload.ExpiresIn) * time.Second
	if lifetime <= 30*time.Second {
		lifetime = 30 * time.Second
	}
	client.token = payload.AccessToken
	client.tokenUntil = now.Add(lifetime - 15*time.Second)
	return client.token, nil
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func isRetriableStatusCode(statusCode int) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}

	return statusCode >= 500 && statusCode <= 599
}

type TokenData struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

type TokenEnvelope struct {
	Success bool      `json:"success"`
	Data    TokenData `json:"data"`
	Message string    `json:"message"`
}

type OAuthClient struct {
	httpClient   *http.Client
	tokenURL     string
	clientID     string
	clientSecret string
	scope        string

	token     *TokenData
	expiresAt time.Time
	mu        sync.Mutex

	statsMu            sync.Mutex
	statsStartedAt     time.Time
	statsTotalRequests int
	stats2xxCount      int
	stats4xxCount      int
	stats5xxCount      int
	statsNetworkErrors int
	stats401Count      int
	stats403Count      int
	statsInFlight      int
	statsPeakInFlight  int
}

const tokenEarlyRefreshWindow = 10 * time.Minute

type pageFetchResult int

type govAPIStatsSnapshot struct {
	StartedAt     time.Time
	TotalRequests int
	Status2xx     int
	Status4xx     int
	Status5xx     int
	NetworkErrors int
	Status401     int
	Status403     int
	InFlight      int
	PeakInFlight  int
}

const (
	defaultMaxPagesPerCycle = 200
	learnedMaxPagesBuffer   = 3

	pageFetchContinue pageFetchResult = iota
	pageFetchFinalPage
	pageFetchSkipPage
	pageFetchAbortCycle
)

var (
	dynamicMaxPagesMutex        sync.RWMutex
	pricesMaxPagesPerCycleCap   = defaultMaxPagesPerCycle
	stationsMaxPagesPerCycleCap = defaultMaxPagesPerCycle
	activeOAuthClientMu         sync.RWMutex
	activeOAuthClient           *OAuthClient
)

func setActiveOAuthClient(client *OAuthClient) {
	activeOAuthClientMu.Lock()
	activeOAuthClient = client
	activeOAuthClientMu.Unlock()
}

func getGovAPIStatsSnapshot() (govAPIStatsSnapshot, bool) {
	activeOAuthClientMu.RLock()
	client := activeOAuthClient
	activeOAuthClientMu.RUnlock()

	if client == nil {
		return govAPIStatsSnapshot{}, false
	}

	return client.snapshotGovAPIStats(), true
}

func getDynamicMaxPagesPerCycle(isStations bool) int {
	dynamicMaxPagesMutex.RLock()
	defer dynamicMaxPagesMutex.RUnlock()

	if isStations {
		return stationsMaxPagesPerCycleCap
	}

	return pricesMaxPagesPerCycleCap
}

func setDynamicMaxPagesFromTerminalPage(isStations bool, terminalPage int) {
	if terminalPage < 1 {
		return
	}

	learnedCap := terminalPage + learnedMaxPagesBuffer

	dynamicMaxPagesMutex.Lock()
	if isStations {
		stationsMaxPagesPerCycleCap = learnedCap
	} else {
		pricesMaxPagesPerCycleCap = learnedCap
	}
	dynamicMaxPagesMutex.Unlock()

	if isStations {
		log.Printf("[STATIONS] Learned dynamic safety cap from terminal page %d: max pages now %d", terminalPage, learnedCap)
		return
	}

	log.Printf("[PRICES] Learned dynamic safety cap from terminal page %d: max pages now %d", terminalPage, learnedCap)
}

func computeAbortBackoff(baseDelay, maxDelay time.Duration, consecutiveAttempts int) time.Duration {
	if consecutiveAttempts <= 1 {
		return baseDelay
	}

	delay := baseDelay
	for i := 1; i < consecutiveAttempts; i++ {
		if delay >= maxDelay {
			return maxDelay
		}
		nextDelay := delay * 2
		if nextDelay > maxDelay {
			return maxDelay
		}
		delay = nextDelay
	}

	if delay > maxDelay {
		return maxDelay
	}

	return delay
}

// Constructor
func NewOAuthClient(tokenURL, clientID, clientSecret, scope string) *OAuthClient {
	return &OAuthClient{
		httpClient:     &http.Client{Timeout: 120 * time.Second},
		tokenURL:       tokenURL,
		clientID:       clientID,
		clientSecret:   clientSecret,
		scope:          scope,
		statsStartedAt: time.Now(),
	}
}

func (c *OAuthClient) recordGovAPIRequestStart() {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	if c.statsStartedAt.IsZero() {
		c.statsStartedAt = time.Now()
	}

	c.statsTotalRequests++
	c.statsInFlight++
	if c.statsInFlight > c.statsPeakInFlight {
		c.statsPeakInFlight = c.statsInFlight
	}
}

func (c *OAuthClient) recordGovAPIRequestResult(statusCode int, err error) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	if c.statsInFlight > 0 {
		c.statsInFlight--
	}

	if err != nil {
		c.statsNetworkErrors++
		return
	}

	switch {
	case statusCode >= 200 && statusCode <= 299:
		c.stats2xxCount++
	case statusCode >= 400 && statusCode <= 499:
		c.stats4xxCount++
	case statusCode >= 500 && statusCode <= 599:
		c.stats5xxCount++
	}

	if statusCode == http.StatusUnauthorized {
		c.stats401Count++
	}
	if statusCode == http.StatusForbidden {
		c.stats403Count++
	}
}

func (c *OAuthClient) snapshotGovAPIStats() govAPIStatsSnapshot {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	return govAPIStatsSnapshot{
		StartedAt:     c.statsStartedAt,
		TotalRequests: c.statsTotalRequests,
		Status2xx:     c.stats2xxCount,
		Status4xx:     c.stats4xxCount,
		Status5xx:     c.stats5xxCount,
		NetworkErrors: c.statsNetworkErrors,
		Status401:     c.stats401Count,
		Status403:     c.stats403Count,
		InFlight:      c.statsInFlight,
		PeakInFlight:  c.statsPeakInFlight,
	}
}

func startGovAPIStatsLogger(ctx context.Context, client *OAuthClient, interval time.Duration) {
	if client == nil || interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		lastLogAt := time.Now()
		lastTotal := 0

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snapshot := client.snapshotGovAPIStats()
				now := time.Now()

				intervalDuration := now.Sub(lastLogAt)
				intervalRequests := snapshot.TotalRequests - lastTotal
				intervalRPM := 0.0
				if intervalDuration > 0 {
					intervalRPM = float64(intervalRequests) / intervalDuration.Minutes()
				}

				lifetimeDuration := now.Sub(snapshot.StartedAt)
				lifetimeRPM := 0.0
				if lifetimeDuration > 0 {
					lifetimeRPM = float64(snapshot.TotalRequests) / lifetimeDuration.Minutes()
				}

				percentOfLimit := (intervalRPM / 30.0) * 100.0
				if percentOfLimit < 0 {
					percentOfLimit = 0
				}

				log.Printf(
					"[RATE] Gov API usage over last %v: %d request(s), avg %.2f req/min (%.1f%% of 30 req/min limit), lifetime avg %.2f req/min, in_flight=%d peak_in_flight=%d, responses 2xx=%d 4xx=%d 5xx=%d net_err=%d 401=%d 403=%d",
					intervalDuration.Round(time.Second),
					intervalRequests,
					intervalRPM,
					percentOfLimit,
					lifetimeRPM,
					snapshot.InFlight,
					snapshot.PeakInFlight,
					snapshot.Status2xx,
					snapshot.Status4xx,
					snapshot.Status5xx,
					snapshot.NetworkErrors,
					snapshot.Status401,
					snapshot.Status403,
				)

				lastLogAt = now
				lastTotal = snapshot.TotalRequests
			}
		}
	}()
}

// Public API call helper (auto-refreshes)
func (c *OAuthClient) Do(req *http.Request) (*http.Response, error) {
	log.Printf("[AUTH] Preparing authenticated request: %s %s", req.Method, req.URL.String())

	token, err := c.getValidTokenWithForce(false)
	if err != nil {
		log.Printf("[AUTH] Failed to get valid token for request %s %s: %v", req.Method, req.URL.String(), err)
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	c.recordGovAPIRequestStart()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordGovAPIRequestResult(0, err)
		return nil, err
	}
	c.recordGovAPIRequestResult(resp.StatusCode, nil)

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return resp, nil
	}

	log.Printf("[AUTH] Received %d for %s %s, forcing token refresh and retrying once", resp.StatusCode, req.Method, req.URL.String())
	resp.Body.Close()

	refreshedToken, refreshErr := c.getValidTokenWithForce(true)
	if refreshErr != nil {
		log.Printf("[AUTH] Forced refresh after 401 failed for %s %s: %v", req.Method, req.URL.String(), refreshErr)
		return nil, refreshErr
	}

	retryReq := req.Clone(req.Context())
	retryReq.Header.Set("Authorization", "Bearer "+refreshedToken)

	c.recordGovAPIRequestStart()
	retryResp, retryErr := c.httpClient.Do(retryReq)
	if retryErr != nil {
		c.recordGovAPIRequestResult(0, retryErr)
		log.Printf("[AUTH] Retry after forced refresh failed for %s %s: %v", req.Method, req.URL.String(), retryErr)
		return nil, retryErr
	}
	c.recordGovAPIRequestResult(retryResp.StatusCode, nil)

	if retryResp.StatusCode == http.StatusUnauthorized {
		log.Printf("[AUTH] Retry after forced refresh still returned 401 for %s %s", req.Method, req.URL.String())
	}

	return retryResp, nil
}

// Get a valid token (cached + refresh-safe)
func (c *OAuthClient) getValidToken() (string, error) {
	return c.getValidTokenWithForce(false)
}

func (c *OAuthClient) getValidTokenWithForce(forceRefresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If token exists and is not close to expiring, reuse it
	if !forceRefresh && c.token != nil && time.Now().Before(c.expiresAt.Add(-tokenEarlyRefreshWindow)) {
		log.Printf("[AUTH] Reusing cached access token (expires in %v)", time.Until(c.expiresAt).Round(time.Second))
		return c.token.AccessToken, nil
	}

	if forceRefresh {
		log.Printf("[AUTH] Forced token refresh requested")
	}

	if c.token != nil && c.token.RefreshToken != "" {
		log.Printf("[AUTH] Access token expired/expiring, attempting refresh token flow")
		if err := c.refreshToken(); err == nil {
			log.Printf("[AUTH] Refresh token flow succeeded")
			return c.token.AccessToken, nil
		}
		log.Printf("[AUTH] Refresh token flow failed, falling back to client credentials flow")
	}

	// Fallback to client credentials flow
	log.Printf("[AUTH] Requesting new access token via client credentials flow")
	if err := c.fetchToken(); err != nil {
		log.Printf("[AUTH] Client credentials token request failed: %v", err)
		return "", err
	}
	log.Printf("[AUTH] Client credentials token request succeeded")

	return c.token.AccessToken, nil
}

// Fetch token (client_credentials)
func (c *OAuthClient) fetchToken() error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("scope", c.scope)

	return c.requestToken(form)
}

// Refresh token
func (c *OAuthClient) refreshToken() error {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", c.token.RefreshToken)
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)

	return c.requestToken(form)
}

// Shared token request logic
func (c *OAuthClient) requestToken(form url.Values) error {
	grantType := form.Get("grant_type")
	if grantType == "" {
		grantType = "unknown"
	}
	log.Printf("[AUTH] Requesting token from token endpoint using grant_type=%s", grantType)

	req, err := http.NewRequest(
		http.MethodPost,
		c.tokenURL,
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		log.Printf("[AUTH] Failed to build token request (grant_type=%s): %v", grantType, err)
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	c.recordGovAPIRequestStart()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordGovAPIRequestResult(0, err)
		log.Printf("[AUTH] Token endpoint request failed (grant_type=%s): %v", grantType, err)
		return err
	}
	c.recordGovAPIRequestResult(resp.StatusCode, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[AUTH] Token endpoint returned non-200 status=%d (grant_type=%s)", resp.StatusCode, grantType)
		return fmt.Errorf("token request failed: %s", body)
	}

	var envelope TokenEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		log.Printf("[AUTH] Failed to decode token response JSON (grant_type=%s): %v", grantType, err)
		return err
	}

	if !envelope.Success {
		log.Printf("[AUTH] Token endpoint returned success=false (grant_type=%s): %s", grantType, envelope.Message)
		return fmt.Errorf("token error: %s", envelope.Message)
	}

	c.token = &envelope.Data
	c.expiresAt = time.Now().Add(time.Duration(c.token.ExpiresIn) * time.Second)
	log.Printf("[AUTH] Token updated (grant_type=%s, expires in %v, has_refresh_token=%t)", grantType, time.Until(c.expiresAt).Round(time.Second), c.token.RefreshToken != "")

	return nil
}

func fetchStationsPage(ctx context.Context, client *OAuthClient, pageNum int, rateLimiter *time.Ticker) pageFetchResult {
	// Wait for rate limiter only when we're about to make an API call
	log.Printf("[STATIONS] Waiting for rate limiter before fetching page %d", pageNum)
	select {
	case <-ctx.Done():
		log.Printf("[STATIONS] Shutdown requested, aborting page %d fetch", pageNum)
		return pageFetchAbortCycle
	case <-rateLimiter.C:
	}

	log.Printf("[STATIONS] Fetching page %d", pageNum)

	// Construct URL with current batch number
	apiURL := fmt.Sprintf("https://www.fuel-finder.service.gov.uk/api/v1/pfs?batch-number=%d", pageNum)
	req, _ := http.NewRequest("GET", apiURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[STATIONS] Error making request for page %d: %v", pageNum, err)
		globalRetryQueue.AddRequest(pageNum, true)
		return pageFetchContinue
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		resp.Body.Close()
		log.Printf("[STATIONS] API returned status %d for page %d: %s", resp.StatusCode, pageNum, strings.TrimSpace(string(body)))

		if resp.StatusCode == http.StatusNotFound {
			log.Printf("[STATIONS] Page %d returned 404, treating as last page", pageNum)
			return pageFetchFinalPage
		}

		if isRetriableStatusCode(resp.StatusCode) {
			globalRetryQueue.AddRequest(pageNum, true)
			return pageFetchContinue
		}

		log.Printf("[STATIONS] Non-retriable status %d on page %d, skipping page and continuing cycle", resp.StatusCode, pageNum)
		return pageFetchSkipPage
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		log.Printf("[STATIONS] Error reading response body for page %d: %v", pageNum, err)
		globalRetryQueue.AddRequest(pageNum, true)
		return pageFetchContinue
	}

	bodyString := string(body)

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(bodyString, "node_id")

	// If no node_id found, treat as last page
	if nodeIdCount == 0 {
		log.Printf("[STATIONS] Page %d contains no node_id occurrences, treating as last page", pageNum)
		setDynamicMaxPagesFromTerminalPage(true, pageNum)
		return pageFetchFinalPage
	}

	log.Printf("[STATIONS] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	StoreJSONPageInMemory(pageNum, bodyString, RequestTypeStationsPage, nodeIdCount)

	// Save the page to JSON so startup can reload cached responses into memory.
	filePath, err := savePageJSON(bodyString, pageNum, "stations")
	if err != nil {
		log.Printf("[STATIONS] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[STATIONS] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Return true if this page has less than NodeIDCountThreshold node_ids (last page)
	if nodeIdCount < NodeIDCountThreshold {
		log.Printf("[STATIONS] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		setDynamicMaxPagesFromTerminalPage(true, pageNum)
		return pageFetchFinalPage
	}

	return pageFetchContinue
}

func fetchPricesPage(ctx context.Context, client *OAuthClient, pageNum int, rateLimiter *time.Ticker) pageFetchResult {
	// Wait for rate limiter only when we're about to make an API call
	log.Printf("[PRICES] Waiting for rate limiter before fetching page %d", pageNum)
	select {
	case <-ctx.Done():
		log.Printf("[PRICES] Shutdown requested, aborting page %d fetch", pageNum)
		return pageFetchAbortCycle
	case <-rateLimiter.C:
	}

	log.Printf("[PRICES] Fetching page %d", pageNum)

	// Construct URL with current batch number
	apiURL := fmt.Sprintf("https://www.fuel-finder.service.gov.uk/api/v1/pfs/fuel-prices?batch-number=%d", pageNum)
	req, _ := http.NewRequest("GET", apiURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PRICES] Error making request for page %d: %v", pageNum, err)
		globalRetryQueue.AddRequest(pageNum, false)
		return pageFetchContinue
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		resp.Body.Close()
		log.Printf("[PRICES] API returned status %d for page %d: %s", resp.StatusCode, pageNum, strings.TrimSpace(string(body)))

		if resp.StatusCode == http.StatusNotFound {
			log.Printf("[PRICES] Page %d returned 404, treating as last page", pageNum)
			return pageFetchFinalPage
		}

		if isRetriableStatusCode(resp.StatusCode) {
			globalRetryQueue.AddRequest(pageNum, false)
			return pageFetchContinue
		}

		log.Printf("[PRICES] Non-retriable status %d on page %d, skipping page and continuing cycle", resp.StatusCode, pageNum)
		return pageFetchSkipPage
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		log.Printf("[PRICES] Error reading response body for page %d: %v", pageNum, err)
		globalRetryQueue.AddRequest(pageNum, false)
		return pageFetchContinue
	}

	bodyString := string(body)

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(bodyString, "node_id")

	// If no node_id found, treat as last page
	if nodeIdCount == 0 {
		log.Printf("[PRICES] Page %d contains no node_id occurrences, treating as last page", pageNum)
		setDynamicMaxPagesFromTerminalPage(false, pageNum)
		return pageFetchFinalPage
	}

	log.Printf("[PRICES] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	StoreJSONPageInMemory(pageNum, bodyString, RequestTypePricesPage, nodeIdCount)

	// Save the page to JSON so startup can reload cached responses into memory.
	filePath, err := savePageJSON(bodyString, pageNum, "prices")
	if err != nil {
		log.Printf("[PRICES] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[PRICES] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Return true if this page has less than NodeIDCountThreshold node_ids (last page)
	if nodeIdCount < NodeIDCountThreshold {
		log.Printf("[PRICES] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		setDynamicMaxPagesFromTerminalPage(false, pageNum)
		return pageFetchFinalPage
	}

	return pageFetchContinue
}

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
}

// Constructor
func NewOAuthClient(tokenURL, clientID, clientSecret, scope string) *OAuthClient {
	return &OAuthClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scope:        scope,
	}
}

// Public API call helper (auto-refreshes)
func (c *OAuthClient) Do(req *http.Request) (*http.Response, error) {
	token, err := c.getValidToken()
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	return c.httpClient.Do(req)
}

// Get a valid token (cached + refresh-safe)
func (c *OAuthClient) getValidToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If token exists and is not close to expiring, reuse it
	if c.token != nil && time.Now().Before(c.expiresAt.Add(-30*time.Second)) {
		return c.token.AccessToken, nil
	}

	if c.token != nil && c.token.RefreshToken != "" {
		if err := c.refreshToken(); err == nil {
			return c.token.AccessToken, nil
		}
	}

	// Fallback to client credentials flow
	if err := c.fetchToken(); err != nil {
		return "", err
	}

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
	req, err := http.NewRequest(
		http.MethodPost,
		c.tokenURL,
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token request failed: %s", body)
	}

	var envelope TokenEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}

	if !envelope.Success {
		return fmt.Errorf("token error: %s", envelope.Message)
	}

	c.token = &envelope.Data
	c.expiresAt = time.Now().Add(time.Duration(c.token.ExpiresIn) * time.Second)

	return nil
}

func fetchStationsPage(ctx context.Context, client *OAuthClient, pageNum int, rateLimiter *time.Ticker) bool {
	// Wait for rate limiter only when we're about to make an API call
	log.Printf("[STATIONS] Waiting for rate limiter before fetching page %d", pageNum)
	select {
	case <-ctx.Done():
		log.Printf("[STATIONS] Shutdown requested, aborting page %d fetch", pageNum)
		return false
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
		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[STATIONS] API returned status %d for page %d", resp.StatusCode, pageNum)
		resp.Body.Close()
		globalRetryQueue.AddRequest(pageNum, true)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		log.Printf("[STATIONS] Error reading response body for page %d: %v", pageNum, err)
		globalRetryQueue.AddRequest(pageNum, true)
		return false
	}

	bodyString := string(body)

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(bodyString, "node_id")

	// If no node_id found, treat as last page
	if nodeIdCount == 0 {
		log.Printf("[STATIONS] Page %d contains no node_id occurrences, treating as last page", pageNum)
		return true
	}

	log.Printf("[STATIONS] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	StoreJSONPageInMemory(pageNum, bodyString, RequestTypeStationsPage, nodeIdCount)

	// Save the page to JSON file (for debug purposes, not used for enrichment)
	filePath, err := savePageJSON(bodyString, pageNum, "stations")
	if err != nil {
		log.Printf("[STATIONS] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[STATIONS] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	log.Printf("[STATIONS] Saved request log for page %d with status %d", pageNum, resp.StatusCode)

	// Return true if this page has less than NodeIDCountThreshold node_ids (last page)
	if nodeIdCount < NodeIDCountThreshold {
		log.Printf("[STATIONS] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		return true
	}

	return false
}

func fetchPricesPage(ctx context.Context, client *OAuthClient, pageNum int, rateLimiter *time.Ticker) bool {
	// Wait for rate limiter only when we're about to make an API call
	log.Printf("[PRICES] Waiting for rate limiter before fetching page %d", pageNum)
	select {
	case <-ctx.Done():
		log.Printf("[PRICES] Shutdown requested, aborting page %d fetch", pageNum)
		return false
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
		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[PRICES] API returned status %d for page %d", resp.StatusCode, pageNum)
		resp.Body.Close()
		globalRetryQueue.AddRequest(pageNum, false)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		log.Printf("[PRICES] Error reading response body for page %d: %v", pageNum, err)
		globalRetryQueue.AddRequest(pageNum, false)
		return false
	}

	bodyString := string(body)

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(bodyString, "node_id")

	// If no node_id found, treat as last page
	if nodeIdCount == 0 {
		log.Printf("[PRICES] Page %d contains no node_id occurrences, treating as last page", pageNum)
		return true
	}

	log.Printf("[PRICES] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	StoreJSONPageInMemory(pageNum, bodyString, RequestTypePricesPage, nodeIdCount)

	// Save the page to JSON file

	// If we cached the price 5 minutes ago anyway, skip the cache refresh, worst case we lose 5mins of data
	filePath, err := savePageJSON(bodyString, pageNum, "prices")
	if err != nil {
		log.Printf("[PRICES] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[PRICES] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	log.Printf("[PRICES] Saved request log for page %d with status %d", pageNum, resp.StatusCode)

	// Return true if this page has less than NodeIDCountThreshold node_ids (last page)
	if nodeIdCount < NodeIDCountThreshold {
		log.Printf("[PRICES] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		return true
	}

	return false
}

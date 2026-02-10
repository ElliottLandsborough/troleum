package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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

type Config struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scope        string
}

type RetryRequest struct {
	PageNum      int
	IsStations   bool // true for stations, false for prices
	LastAttempt  time.Time
	AttemptCount int
}

type RetryQueue struct {
	requests []RetryRequest
	mu       sync.Mutex
}

// Price-related structs
type FuelPrice struct {
	FuelType         string `json:"fuel_type"`
	Price            string `json:"price"`
	PriceLastUpdated string `json:"price_last_updated"`
}

type PriceStation struct {
	NodeID              string      `json:"node_id"`
	MftOrganisationName string      `json:"mft_organisation_name"`
	PublicPhoneNumber   string      `json:"public_phone_number"`
	TradingName         string      `json:"trading_name"`
	FuelPrices          []FuelPrice `json:"fuel_prices"`
}

// Station-related structs
type Location struct {
	AddressLine1 string `json:"address_line_1"`
	AddressLine2 string `json:"address_line_2"`
	City         string `json:"city"`
	Country      string `json:"country"`
	County       string `json:"county"`
	Postcode     string `json:"postcode"`
	Latitude     string `json:"latitude"`
	Longitude    string `json:"longitude"`
}

type DayHours struct {
	Open      string `json:"open"`
	Close     string `json:"close"`
	Is24Hours bool   `json:"is_24_hours"`
}

type UsualDays struct {
	Monday    DayHours `json:"monday"`
	Tuesday   DayHours `json:"tuesday"`
	Wednesday DayHours `json:"wednesday"`
	Thursday  DayHours `json:"thursday"`
	Friday    DayHours `json:"friday"`
	Saturday  DayHours `json:"saturday"`
	Sunday    DayHours `json:"sunday"`
}

type BankHoliday struct {
	Type      string `json:"type"`
	OpenTime  string `json:"open_time"`
	CloseTime string `json:"close_time"`
	Is24Hours bool   `json:"is_24_hours"`
}

type OpeningTimes struct {
	UsualDays   UsualDays   `json:"usual_days"`
	BankHoliday BankHoliday `json:"bank_holiday"`
}

type Station struct {
	NodeID                      string       `json:"node_id"`
	MftOrganisationName         string       `json:"mft_organisation_name"`
	PublicPhoneNumber           string       `json:"public_phone_number"`
	TradingName                 string       `json:"trading_name"`
	IsSameTradingAndBrandName   bool         `json:"is_same_trading_and_brand_name"`
	BrandName                   string       `json:"brand_name"`
	TemporaryClosure            bool         `json:"temporary_closure"`
	PermanentClosure            *bool        `json:"permanent_closure"`
	PermanentClosureDate        *string      `json:"permanent_closure_date"`
	IsMotorwayServiceStation    bool         `json:"is_motorway_service_station"`
	IsSupermarketServiceStation bool         `json:"is_supermarket_service_station"`
	Location                    Location     `json:"location"`
	Amenities                   []string     `json:"amenities"`
	OpeningTimes                OpeningTimes `json:"opening_times"`
	FuelTypes                   []string     `json:"fuel_types"`
}

// Global retry queue
var globalRetryQueue = &RetryQueue{
	requests: make([]RetryRequest, 0),
}

// Global cycle completion tracking
var (
	lastStationsCycleComplete time.Time
	lastPricesCycleComplete   time.Time
	cycleTimeMutex            sync.RWMutex
)

func (rq *RetryQueue) AddRequest(pageNum int, isStations bool) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	retryReq := RetryRequest{
		PageNum:      pageNum,
		IsStations:   isStations,
		LastAttempt:  time.Now(),
		AttemptCount: 1,
	}

	rq.requests = append(rq.requests, retryReq)
	log.Printf("[RETRY] Added %s page %d to retry queue", getRequestType(isStations), pageNum)
}

func (rq *RetryQueue) GetNextRequest() (RetryRequest, bool) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if len(rq.requests) == 0 {
		return RetryRequest{}, false
	}

	// Get the first request and remove it from the queue
	req := rq.requests[0]
	rq.requests = rq.requests[1:]

	return req, true
}

func (rq *RetryQueue) HasRequests() bool {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	return len(rq.requests) > 0
}

func getRequestType(isStations bool) string {
	if isStations {
		return "STATIONS"
	}
	return "PRICES"
}

// todo:
// 1. retry data could? be saved to the database, so if the app restarts we lose all retry attempts and just move on to the next page
// 2. cached data from files is loaded but not saved to memory, the json files are separate from the db, and possibly useless. do we need the cached data in files at all? could we just save the raw response to the db and load from there for the enrich function? or is it useful to have the json files as a backup/cache?
// 3. work out what order the data is saved and retreived in
func main() {
	// Initialize database connection
	if err := InitDatabase(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Start enriching saved pages on a 15-second timer
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		// Run enrichSavedPages immediately on startup
		enrichSavedPages()

		// Then run it every 15 seconds
		for range ticker.C {
			enrichSavedPages()
		}
	}()

	// Start web server for saved pages
	setupWebServer()

	// load the .env file manually
	if err := loadDotEnv(".env"); err != nil {
		fmt.Println("Warning: could not load .env file:", err)
	}

	cfg := LoadConfig()

	client := NewOAuthClient(
		"https://www.fuel-finder.service.gov.uk/api/v1/oauth/generate_access_token",
		cfg.ClientID,
		cfg.ClientSecret,
		"fuelfinder.read",
	)

	// Create rate limiter (3 requests per minute = 1 request every 20 seconds)
	rateLimiter := time.NewTicker(20 * time.Second)
	// prod allows 6 requests per minute, so use:
	// rateLimiter := time.NewTicker(10 * time.Second)
	defer rateLimiter.Stop()

	// Start continuous fetching in a goroutine
	go continuousFetchStations(client, rateLimiter)

	// Start continuous fetching of prices in a goroutine
	go continuousFetchPrices(client, rateLimiter)

	// Start retry worker in a goroutine
	go retryWorker(client, rateLimiter)

	// Keep main running and allow for other code
	log.Println("Started continuous data fetching...")
	select {} // Block forever, allowing the goroutine to run
}

func retryWorker(client *OAuthClient, rateLimiter *time.Ticker) {
	for {
		// Check for retry requests every 30 seconds
		time.Sleep(30 * time.Second)

		if !globalRetryQueue.HasRequests() {
			continue
		}

		req, hasRequest := globalRetryQueue.GetNextRequest()
		if !hasRequest {
			continue
		}

		// Check time limits before processing retry
		cycleTimeMutex.RLock()
		var shouldWait bool
		var waitTime time.Duration
		var limitType string

		if req.IsStations {
			if !lastStationsCycleComplete.IsZero() {
				timeSinceLastCycle := time.Since(lastStationsCycleComplete)
				if timeSinceLastCycle < time.Hour {
					shouldWait = true
					waitTime = time.Hour - timeSinceLastCycle
					limitType = "hourly"
				}
			}
		} else {
			if !lastPricesCycleComplete.IsZero() {
				timeSinceLastCycle := time.Since(lastPricesCycleComplete)
				if timeSinceLastCycle < 15*time.Minute {
					shouldWait = true
					waitTime = 15*time.Minute - timeSinceLastCycle
					limitType = "15-minute"
				}
			}
		}
		cycleTimeMutex.RUnlock()

		if shouldWait {
			log.Printf("[RETRY] Postponing %s page %d retry, waiting %v for %s limit", getRequestType(req.IsStations), req.PageNum, waitTime, limitType)
			// Re-queue the request for later
			globalRetryQueue.mu.Lock()
			globalRetryQueue.requests = append(globalRetryQueue.requests, req)
			globalRetryQueue.mu.Unlock()
			continue
		}

		// Wait for rate limiter before processing retry
		log.Printf("[RETRY] Waiting for rate limiter before processing retry for %s page %d", getRequestType(req.IsStations), req.PageNum)
		<-rateLimiter.C

		log.Printf("[RETRY] Processing %s page %d (attempt %d)", getRequestType(req.IsStations), req.PageNum, req.AttemptCount)

		var success bool
		if req.IsStations {
			success = retryFetchStationsPage(client, req.PageNum)
		} else {
			success = retryFetchPricesPage(client, req.PageNum)
		}

		if !success {
			req.AttemptCount++
			if req.AttemptCount <= 3 { // Max 3 retry attempts
				req.LastAttempt = time.Now()
				globalRetryQueue.mu.Lock()
				globalRetryQueue.requests = append(globalRetryQueue.requests, req)
				globalRetryQueue.mu.Unlock()
				log.Printf("[RETRY] Re-queued %s page %d for retry (attempt %d)", getRequestType(req.IsStations), req.PageNum, req.AttemptCount)
			} else {
				log.Printf("[RETRY] Giving up on %s page %d after %d attempts", getRequestType(req.IsStations), req.PageNum, req.AttemptCount)
			}
		} else {
			log.Printf("[RETRY] Successfully processed %s page %d", getRequestType(req.IsStations), req.PageNum)
		}
	}
}

func continuousFetchStations(client *OAuthClient, rateLimiter *time.Ticker) {
	currentPage := 1
	var cycleStartTime time.Time

	for {
		// Start timing when we begin a new cycle at page 1
		if currentPage == 1 {
			// Check if we need to skip this cycle due to hourly limit
			cycleTimeMutex.RLock()
			lastComplete := lastStationsCycleComplete
			cycleTimeMutex.RUnlock()

			if !lastComplete.IsZero() {
				timeSinceLastCycle := time.Since(lastComplete)
				if timeSinceLastCycle < time.Hour {
					waitTime := time.Hour - timeSinceLastCycle
					log.Printf("[STATIONS] Skipping cycle, waiting %v for hourly limit", waitTime)
					time.Sleep(waitTime)
					continue
				}
			}

			cycleStartTime = time.Now()
			log.Println("[STATIONS] Starting new cycle from page 1")
		}

		isLastPage := fetchStationsPage(client, currentPage, rateLimiter)

		if isLastPage {
			cycleDuration := time.Since(cycleStartTime)
			now := time.Now()

			cycleTimeMutex.Lock()
			lastStationsCycleComplete = now
			cycleTimeMutex.Unlock()

			log.Printf("[STATIONS] Reached final page, cycle completed in %v, restarting from page 1", cycleDuration)
			currentPage = 1
		} else {
			currentPage++
		}
	}
}

func isFileRecentEnough(filePath string, maxAgeMinutes int) bool {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		// File doesn't exist or can't be accessed
		return false
	}

	maxAge := time.Duration(maxAgeMinutes) * time.Minute
	return time.Since(fileInfo.ModTime()) < maxAge
}

func LoadConfig() Config {
	return Config{
		ClientID:     mustEnv("OAUTH_CLIENT_ID"),
		ClientSecret: mustEnv("OAUTH_CLIENT_SECRET"),
	}
}

// Get contents of latest json file
func getLatestJSONFileContents() (string, error) {
	dir := "json"

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var latestFile string
	var latestTimestamp int64 = 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var timestamp int64
		if _, err := fmt.Sscanf(entry.Name(), "%d", &timestamp); err == nil {
			if timestamp > latestTimestamp {
				latestTimestamp = timestamp
				latestFile = entry.Name()
			}
		}
	}

	if latestFile == "" {
		return "", fmt.Errorf("no JSON files found")
	}

	fullPath := filepath.Join(dir, latestFile)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}

	return string(data), nil
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

// shouldCreateNewFile checks if we need to create a new JSON file
// based on the age of existing timestamp-named files
func shouldCreateNewFile() bool {
	dir := "json"

	// Check if directory exists and read entries
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist or can't read it, so create new file
		return true
	}

	var latestTimestamp int64 = 0

	// Find the most recent timestamp
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Parse filename as Unix timestamp
		var timestamp int64
		if _, err := fmt.Sscanf(entry.Name(), "%d", &timestamp); err == nil {
			if timestamp > latestTimestamp {
				latestTimestamp = timestamp
			}
		}
	}

	// If no valid timestamp files found, create new file
	if latestTimestamp == 0 {
		return true
	}

	// Check if latest file is older than 1 hour
	latestTime := time.Unix(latestTimestamp, 0)
	return time.Since(latestTime) > time.Hour
}

func savePageJSON(jsonString string, pageNumber int, logName string) (string, error) {
	dir := "json"
	filename := fmt.Sprintf("%s_page_%d.json", logName, pageNumber)
	fullPath := filepath.Join(dir, filename)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	f, err := os.OpenFile(
		fullPath,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0600,
	)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = f.WriteString(jsonString)
	return fullPath, nil
}

func continuousFetchPrices(client *OAuthClient, rateLimiter *time.Ticker) {
	currentPage := 1
	var cycleStartTime time.Time

	for {
		// Start timing when we begin a new cycle at page 1
		if currentPage == 1 {
			// Check if we need to skip this cycle due to 15-minute limit
			cycleTimeMutex.RLock()
			lastComplete := lastPricesCycleComplete
			cycleTimeMutex.RUnlock()

			if !lastComplete.IsZero() {
				timeSinceLastCycle := time.Since(lastComplete)
				if timeSinceLastCycle < 15*time.Minute {
					waitTime := 15*time.Minute - timeSinceLastCycle
					log.Printf("[PRICES] Skipping cycle, waiting %v for 15-minute limit", waitTime)
					time.Sleep(waitTime)
					continue
				}
			}

			cycleStartTime = time.Now()
			log.Println("[PRICES] Starting new cycle from page 1")
		}

		isLastPage := fetchPricesPage(client, currentPage, rateLimiter)

		if isLastPage {
			cycleDuration := time.Since(cycleStartTime)
			now := time.Now()

			cycleTimeMutex.Lock()
			lastPricesCycleComplete = now
			cycleTimeMutex.Unlock()

			log.Printf("[PRICES] Reached final page, cycle completed in %v, restarting from page 1", cycleDuration)
			currentPage = 1
		} else {
			currentPage++
		}
	}
}

func retryFetchStationsPage(client *OAuthClient, pageNum int) bool {
	log.Printf("[RETRY-STATIONS] Fetching page %d", pageNum)

	// Construct URL with current batch number
	apiURL := fmt.Sprintf("https://www.fuel-finder.service.gov.uk/api/v1/pfs?batch-number=%d", pageNum)
	req, _ := http.NewRequest("GET", apiURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[RETRY-STATIONS] Error making request for page %d: %v", pageNum, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RETRY-STATIONS] API returned status %d for page %d", resp.StatusCode, pageNum)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[RETRY-STATIONS] Error reading response body for page %d: %v", pageNum, err)
		return false
	}

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(string(body), "node_id")
	log.Printf("[RETRY-STATIONS] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	// Save the page
	filePath, err := savePageJSON(string(body), pageNum, "stations")
	if err != nil {
		log.Printf("[RETRY-STATIONS] Error saving JSON file for page %d: %v", pageNum, err)
		return false
	} else {
		log.Printf("[RETRY-STATIONS] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Save request to database regardless of success/failure
	var errorMessage string
	if err != nil {
		errorMessage = err.Error()
	}
	SaveRequestToDatabase(RequestTypeStationsPage, pageNum, resp.StatusCode, string(body), errorMessage)
	log.Printf("[RETRY-STATIONS] Saved request log for page %d with status %d", pageNum, resp.StatusCode)

	// Store the saved page datetime in a map
	storeSavedPage(savedStationsPages, &savedPagesMutex, pageNum, filePath)

	return true
}

func retryFetchPricesPage(client *OAuthClient, pageNum int) bool {
	log.Printf("[RETRY-PRICES] Fetching page %d", pageNum)

	// Construct URL with current batch number
	apiURL := fmt.Sprintf("https://www.fuel-finder.service.gov.uk/api/v1/pfs/fuel-prices?batch-number=%d", pageNum)
	req, _ := http.NewRequest("GET", apiURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[RETRY-PRICES] Error making request for page %d: %v", pageNum, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RETRY-PRICES] API returned status %d for page %d", resp.StatusCode, pageNum)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[RETRY-PRICES] Error reading response body for page %d: %v", pageNum, err)
		return false
	}

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(string(body), "node_id")
	log.Printf("[RETRY-PRICES] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	// Save the page
	filePath, err := savePageJSON(string(body), pageNum, "prices")
	if err != nil {
		log.Printf("[RETRY-PRICES] Error saving JSON file for page %d: %v", pageNum, err)
		return false
	} else {
		log.Printf("[RETRY-PRICES] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Save request to database regardless of success/failure
	var errorMessage string
	if err != nil {
		errorMessage = err.Error()
	}
	SaveRequestToDatabase(RequestTypePricesPage, pageNum, resp.StatusCode, string(body), errorMessage)
	log.Printf("[RETRY-PRICES] Saved request log for page %d with status %d", pageNum, resp.StatusCode)

	// Store the saved page datetime in a map
	storeSavedPage(savedPricesPages, &savedPagesMutex, pageNum, filePath)

	return true
}

func saveJSONOnce(jsonString string) (string, error) {
	dir := "json" // or "/json" if you really mean absolute path
	filename := fmt.Sprintf("%d", time.Now().Unix())
	fullPath := filepath.Join(dir, filename)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	f, err := os.OpenFile(
		fullPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0600,
	)
	if err != nil {
		// err will be something like "file exists" if microtime already exists
		return "", err
	}
	defer f.Close()

	_, err = f.WriteString(jsonString)
	return fullPath, nil
}

func loadDotEnv(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// skip empty lines or comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue // skip malformed lines
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		os.Setenv(key, value)
	}

	return scanner.Err()
}

// mustEnv ensures an environment variable is set
func mustEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return val
}

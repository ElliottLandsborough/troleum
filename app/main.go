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

func main() {

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

	// Keep main running and allow for other code
	log.Println("Started continuous data fetching...")
	select {} // Block forever, allowing the goroutine to run
}

func continuousFetchStations(client *OAuthClient, rateLimiter *time.Ticker) {
	currentPage := 1
	var cycleStartTime time.Time
	var lastCycleCompleteTime time.Time

	for {
		// Wait for rate limiter
		<-rateLimiter.C

		// Start timing when we begin a new cycle at page 1
		if currentPage == 1 {
			// Check if we need to skip this cycle due to hourly limit
			if !lastCycleCompleteTime.IsZero() {
				timeSinceLastCycle := time.Since(lastCycleCompleteTime)
				if timeSinceLastCycle < time.Hour {
					waitTime := time.Hour - timeSinceLastCycle
					log.Printf("[STATIONS] Skipping cycle, need to wait %v more (hourly limit)", waitTime)
					continue // Skip this rate limiter tick
				}
			}

			cycleStartTime = time.Now()
			log.Println("[STATIONS] Starting new cycle from page 1")
		}

		isLastPage := fetchStationsPage(client, currentPage, rateLimiter)

		if isLastPage {
			cycleDuration := time.Since(cycleStartTime)
			lastCycleCompleteTime = time.Now()
			log.Printf("[STATIONS] Reached final page, cycle completed in %v, restarting from page 1", cycleDuration)
			currentPage = 1
		} else {
			currentPage++
		}
	}
}

func fetchStationsPage(client *OAuthClient, pageNum int, rateLimiter *time.Ticker) bool {
	log.Printf("[STATIONS] Fetching page %d", pageNum)

	// Construct URL with current batch number
	apiURL := fmt.Sprintf("https://www.fuel-finder.service.gov.uk/api/v1/pfs?batch-number=%d", pageNum)
	req, _ := http.NewRequest("GET", apiURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[STATIONS] Error making request for page %d: %v", pageNum, err)
		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[STATIONS] API returned status %d for page %d", resp.StatusCode, pageNum)
		resp.Body.Close()
		return false
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		log.Printf("[STATIONS] Error reading response body for page %d: %v", pageNum, err)
		return false
	}

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(string(body), "node_id")
	log.Printf("[STATIONS] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	// Save the page
	filePath, err := saveStationsPageJSON(string(body), pageNum)
	if err != nil {
		log.Printf("[STATIONS] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[STATIONS] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Return true if this page has less than 500 node_ids (last page)
	if nodeIdCount < 500 {
		log.Printf("[STATIONS] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		return true
	}

	return false
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

func saveStationsPageJSON(jsonString string, pageNumber int) (string, error) {
	dir := "json"
	filename := fmt.Sprintf("stations_page_%d.json", pageNumber)
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
	var lastCycleCompleteTime time.Time

	for {
		// Wait for rate limiter
		<-rateLimiter.C

		// Start timing when we begin a new cycle at page 1
		if currentPage == 1 {
			// Check if we need to skip this cycle due to 15-minute limit
			if !lastCycleCompleteTime.IsZero() {
				timeSinceLastCycle := time.Since(lastCycleCompleteTime)
				if timeSinceLastCycle < 15*time.Minute {
					waitTime := 15*time.Minute - timeSinceLastCycle
					log.Printf("[PRICES] Skipping cycle, need to wait %v more (15-minute limit)", waitTime)
					continue // Skip this rate limiter tick
				}
			}

			cycleStartTime = time.Now()
			log.Println("[PRICES] Starting new cycle from page 1")
		}

		isLastPage := fetchPricesPage(client, currentPage, rateLimiter)

		if isLastPage {
			cycleDuration := time.Since(cycleStartTime)
			lastCycleCompleteTime = time.Now()
			log.Printf("[PRICES] Reached final page, cycle completed in %v, restarting from page 1", cycleDuration)
			currentPage = 1
		} else {
			currentPage++
		}
	}
}

func fetchPricesPage(client *OAuthClient, pageNum int, rateLimiter *time.Ticker) bool {
	log.Printf("[PRICES] Fetching page %d", pageNum)

	// Construct URL with current batch number
	apiURL := fmt.Sprintf("https://www.fuel-finder.service.gov.uk/api/v1/pfs/fuel-prices?batch-number=%d", pageNum)
	req, _ := http.NewRequest("GET", apiURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PRICES] Error making request for page %d: %v", pageNum, err)
		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[PRICES] API returned status %d for page %d", resp.StatusCode, pageNum)
		resp.Body.Close()
		return false
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		log.Printf("[PRICES] Error reading response body for page %d: %v", pageNum, err)
		return false
	}

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(string(body), "node_id")
	log.Printf("[PRICES] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	// Save the page
	filePath, err := savePricesPageJSON(string(body), pageNum)
	if err != nil {
		log.Printf("[PRICES] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[PRICES] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Return true if this page has less than 500 node_ids (last page)
	if nodeIdCount < 500 {
		log.Printf("[PRICES] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		return true
	}

	return false
}

func savePricesPageJSON(jsonString string, pageNumber int) (string, error) {
	dir := "json"
	filename := fmt.Sprintf("prices_page_%d.json", pageNumber)
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

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

	// Fetch all pages from the API
	batchNumber := 1
	for {
		log.Printf("Fetching batch %d", batchNumber)

		// Construct URL with current batch number
		apiURL := fmt.Sprintf("https://www.fuel-finder.service.gov.uk/api/v1/pfs?batch-number=%d", batchNumber)
		req, _ := http.NewRequest("GET", apiURL, nil)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error making request for batch %d: %v", batchNumber, err)
			break
		}

		if resp.StatusCode != http.StatusOK {
			log.Printf("API returned status %d for batch %d, stopping", resp.StatusCode, batchNumber)
			resp.Body.Close()
			break
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			log.Printf("Error reading response body for batch %d: %v", batchNumber, err)
			break
		}

		// Save every page regardless of content
		filePath, err := savePageJSON(string(body), batchNumber)
		if err != nil {
			log.Printf("Error saving JSON file for batch %d: %v", batchNumber, err)
		} else {
			log.Printf("Saved batch %d to file: %s", batchNumber, filepath.Base(filePath))
		}

		batchNumber++

		// Add a small delay between requests to be respectful to the API
		time.Sleep(2 * time.Second)
	}

	log.Println("Finished fetching pages due to error")
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
		httpClient:   &http.Client{Timeout: 60 * time.Second},
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

	log.Println("Waiting for 3 seconds before next request...")
	time.Sleep(3 * time.Second)

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

func savePageJSON(jsonString string, pageNumber int) (string, error) {
	dir := "json"
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("page_%d_%d.json", pageNumber, timestamp)
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

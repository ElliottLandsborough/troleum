package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RawStationsResponse represents the raw response from the API when fetching station data.
type RawStationsResponse struct {
	StatusCode uint16
	Data       json.RawMessage
	CreatedAt  time.Time
}

// RawPricesResponse represents the raw response from the API when fetching price station data.
type RawPricesResponse struct {
	StatusCode uint16
	Data       json.RawMessage
	CreatedAt  time.Time
}

func fetchStationsPage(client *OAuthClient, pageNum int, rateLimiter *time.Ticker) bool {
	// We only just cached this 60 minutes ago, so skip if within that time
	filePath := filepath.Join("json", fmt.Sprintf("stations_page_%d.json", pageNum))
	if isFileRecentEnough(filePath, 60) {
		log.Printf("[STATIONS] Skipping page %d - file exists and is recent enough", pageNum)
		// Need to check if this is the last page by reading the existing file
		content, err := os.ReadFile(filePath)
		if err == nil {
			nodeIdCount := strings.Count(string(content), "node_id")
			log.Printf("[STATIONS] Existing page %d contains %d node_id occurrences", pageNum, nodeIdCount)
			return nodeIdCount < 500
		}
		// If we can't read the file, assume it's not the last page to be safe
		return false
	}

	// Wait for rate limiter only when we're about to make an API call
	log.Printf("[STATIONS] Waiting for rate limiter before fetching page %d", pageNum)
	<-rateLimiter.C

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

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(string(body), "node_id")
	log.Printf("[STATIONS] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	// If no node_id found, treat as last page
	if nodeIdCount == 0 {
		log.Printf("[STATIONS] Page %d contains no node_id occurrences, treating as last page", pageNum)
		return true
	}

	// Save the page
	filePath, err = savePageJSON(string(body), pageNum, "stations")
	if err != nil {
		log.Printf("[STATIONS] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[STATIONS] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Save request to database regardless of success/failure
	var errorMessage string
	if err != nil {
		errorMessage = err.Error()
	}
	SaveRequestToDatabase(RequestTypeStationsPage, pageNum, resp.StatusCode, string(body), errorMessage)
	log.Printf("[STATIONS] Saved request log for page %d with status %d", pageNum, resp.StatusCode)

	// Return true if this page has less than 500 node_ids (last page)
	if nodeIdCount < 500 {
		log.Printf("[STATIONS] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		return true
	}

	return false
}

func fetchPricesPage(client *OAuthClient, pageNum int, rateLimiter *time.Ticker) bool {
	// If we cached the price 5 minutes ago anyway, skip the cache refresh, worst case we lose 5mins of data
	filePath := filepath.Join("json", fmt.Sprintf("prices_page_%d.json", pageNum))
	if isFileRecentEnough(filePath, 5) {
		log.Printf("[PRICES] Skipping page %d - file exists and is recent enough", pageNum)
		// Need to check if this is the last page by reading the existing file
		content, err := os.ReadFile(filePath)
		if err == nil {
			nodeIdCount := strings.Count(string(content), "node_id")
			log.Printf("[PRICES] Existing page %d contains %d node_id occurrences", pageNum, nodeIdCount)
			return nodeIdCount < 500
		}
		// If we can't read the file, assume it's not the last page to be safe
		return false
	}

	// Wait for rate limiter only when we're about to make an API call
	log.Printf("[PRICES] Waiting for rate limiter before fetching page %d", pageNum)
	<-rateLimiter.C

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

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(string(body), "node_id")
	log.Printf("[PRICES] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)

	// If no node_id found, treat as last page
	if nodeIdCount == 0 {
		log.Printf("[PRICES] Page %d contains no node_id occurrences, treating as last page", pageNum)
		return true
	}

	// Save the page
	filePath, err = savePageJSON(string(body), pageNum, "prices")
	if err != nil {
		log.Printf("[PRICES] Error saving JSON file for page %d: %v", pageNum, err)
	} else {
		log.Printf("[PRICES] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// Save request to database regardless of success/failure
	var errorMessage string
	if err != nil {
		errorMessage = err.Error()
	}
	SaveRequestToDatabase(RequestTypePricesPage, pageNum, resp.StatusCode, string(body), errorMessage)
	log.Printf("[PRICES] Saved request log for page %d with status %d", pageNum, resp.StatusCode)

	// Return true if this page has less than 500 node_ids (last page)
	if nodeIdCount < 500 {
		log.Printf("[PRICES] Page %d appears to be the last page (%d node_ids)", pageNum, nodeIdCount)
		return true
	}

	return false
}

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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

// Global retry queue
var globalRetryQueue = &RetryQueue{
	requests: make([]RetryRequest, 0),
}

// Global cycle completion tracking
var (
	lastStationsCycleComplete time.Time
	lastPricesCycleComplete   time.Time
	cycleTimeMutex            sync.RWMutex
	retryWorkerTickAfter      = time.After
	retryStationsProcessor    = retryFetchStationsPage
	retryPricesProcessor      = retryFetchPricesPage
)

func (rq *RetryQueue) AddRequest(pageNum int, isStations bool) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	for _, existing := range rq.requests {
		if existing.PageNum == pageNum && existing.IsStations == isStations {
			log.Printf("[RETRY] Duplicate retry ignored for %s page %d", getRequestType(isStations), pageNum)
			return
		}
	}

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

func (rq *RetryQueue) Len() int {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	return len(rq.requests)
}

func getRequestType(isStations bool) string {
	if isStations {
		return "STATIONS"
	}
	return "PRICES"
}

func retryWorker(ctx context.Context, client *OAuthClient, rateLimiter *time.Ticker) {
	for {
		// Check for retry requests every 30 seconds
		select {
		case <-ctx.Done():
			log.Println("[RETRY] Shutdown requested, stopping retry worker")
			return
		case <-retryWorkerTickAfter(30 * time.Second):
		}

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
		select {
		case <-ctx.Done():
			log.Println("[RETRY] Shutdown requested, stopping retry worker")
			return
		case <-rateLimiter.C:
		}

		log.Printf("[RETRY] Processing %s page %d (attempt %d)", getRequestType(req.IsStations), req.PageNum, req.AttemptCount)

		var success bool
		if req.IsStations {
			success = retryStationsProcessor(client, req.PageNum)
		} else {
			success = retryPricesProcessor(client, req.PageNum)
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
		if resp.StatusCode == http.StatusNotFound {
			log.Printf("[RETRY-STATIONS] Page %d returned 404, dropping retry as terminal", pageNum)
			return true
		}

		if isRetriableStatusCode(resp.StatusCode) {
			return false
		}

		log.Printf("[RETRY-STATIONS] Non-retriable status %d for page %d, dropping retry", resp.StatusCode, pageNum)
		return true
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[RETRY-STATIONS] Error reading response body for page %d: %v", pageNum, err)
		return false
	}

	bodyString := string(body)

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(bodyString, "node_id")
	if nodeIdCount > 0 {
		log.Printf("[RETRY-STATIONS] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)
		StoreJSONPageInMemory(pageNum, bodyString, RequestTypeStationsPage, nodeIdCount)
	} else {
		log.Printf("[RETRY-STATIONS] Page %d contains no node_id occurrences", pageNum)
	}

	// Save the page
	filePath, err := savePageJSON(bodyString, pageNum, "stations")
	if err != nil {
		log.Printf("[RETRY-STATIONS] Error saving JSON file for page %d: %v", pageNum, err)
		// commented area:
		// return false
	} else {
		log.Printf("[RETRY-STATIONS] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// This used to be above, in the commented area.
	// Moved here so that we always save the log to db even if saving the file fails
	// todo: check db to see if this means any false page error gets saved
	if err != nil {
		return false
	}

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
		if resp.StatusCode == http.StatusNotFound {
			log.Printf("[RETRY-PRICES] Page %d returned 404, dropping retry as terminal", pageNum)
			return true
		}

		if isRetriableStatusCode(resp.StatusCode) {
			return false
		}

		log.Printf("[RETRY-PRICES] Non-retriable status %d for page %d, dropping retry", resp.StatusCode, pageNum)
		return true
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[RETRY-PRICES] Error reading response body for page %d: %v", pageNum, err)
		return false
	}

	bodyString := string(body)

	// Check if this is the last page by counting 'node_id' occurrences
	nodeIdCount := strings.Count(bodyString, "node_id")
	if nodeIdCount > 0 {
		log.Printf("[RETRY-PRICES] Page %d contains %d node_id occurrences", pageNum, nodeIdCount)
		StoreJSONPageInMemory(pageNum, bodyString, RequestTypePricesPage, nodeIdCount)
	} else {
		log.Printf("[RETRY-PRICES] Page %d contains no node_id occurrences", pageNum)
	}

	// Save the page
	filePath, err := savePageJSON(bodyString, pageNum, "prices")
	if err != nil {
		log.Printf("[RETRY-PRICES] Error saving JSON file for page %d: %v", pageNum, err)
		// commented area:
		// return false
	} else {
		log.Printf("[RETRY-PRICES] Saved page %d to file: %s", pageNum, filepath.Base(filePath))
	}

	// This used to be above, in the commented area.
	// Moved here so that we always save the log to db even if saving the file fails
	// todo: check db to see if this means any false page error gets saved
	if err != nil {
		return false
	}

	return true
}

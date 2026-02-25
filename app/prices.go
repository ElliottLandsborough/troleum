package main

import (
	"log"
	"time"
)

// FuelPrice returned as nested struct within PriceStation, which is returned by the prices endpoint
type FuelPrice struct {
	FuelType         string    `json:"fuel_type"`
	Price            float64   `json:"price"`
	PriceLastUpdated time.Time `json:"price_last_updated"`
}

// PriceStation struct returned by the prices endpoint, containing station details and a list of fuel prices
type PriceStation struct {
	NodeID              string      `json:"node_id"`
	MftOrganisationName string      `json:"mft_organisation_name"`
	PublicPhoneNumber   string      `json:"public_phone_number"`
	TradingName         string      `json:"trading_name"`
	FuelPrices          []FuelPrice `json:"fuel_prices"`
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

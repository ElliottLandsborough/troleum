package main

import (
	"log"
	"math"
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

// normalizeFuelPriceValue converts values that appear to be in pounds (e.g. 1.55)
// into pence (e.g. 155.0). Most API values are already in pence and are left unchanged.
func normalizeFuelPriceValue(rawPrice float64) (float64, bool) {
	if rawPrice > 0 && rawPrice < 10 {
		// Keep one decimal place in pence where needed (e.g. 1.608 -> 160.8).
		normalized := math.Round((rawPrice*100)*10) / 10
		return normalized, true
	}

	return rawPrice, false
}

// normalizePriceStationsFuelPrices normalizes fuel price units across a batch of
// price stations. It logs each conversion and a batch summary for traceability.
func normalizePriceStationsFuelPrices(priceStationsList []PriceStation) int {
	convertedCount := 0

	for stationIdx := range priceStationsList {
		for priceIdx := range priceStationsList[stationIdx].FuelPrices {
			price := &priceStationsList[stationIdx].FuelPrices[priceIdx]
			normalized, converted := normalizeFuelPriceValue(price.Price)
			if !converted {
				continue
			}

			log.Printf(
				"[PRICES] Normalized price from pounds to pence: node_id=%s fuel_type=%s raw=%.3f normalized=%.1f",
				priceStationsList[stationIdx].NodeID,
				price.FuelType,
				price.Price,
				normalized,
			)

			price.Price = normalized
			convertedCount++
		}
	}

	if convertedCount > 0 {
		log.Printf("[PRICES] Completed normalization: converted %d fuel price value(s) from pounds to pence", convertedCount)
	}

	return convertedCount
}

// Update the fuel types cache with the latest unique fuel types from all stations in memory
func updateFuelTypesCache() {
	fuelTypesCacheMutex.Lock()
	defer fuelTypesCacheMutex.Unlock()

	fuelTypeSet := make(map[string]struct{})
	for _, station := range priceStations {
		for _, price := range station.FuelPrices {
			fuelTypeSet[price.FuelType] = struct{}{}
		}
	}

	fuelTypes := make([]string, 0, len(fuelTypeSet))
	for fuelType := range fuelTypeSet {
		fuelTypes = append(fuelTypes, fuelType)
	}
	fuelTypesCache = fuelTypes
	log.Printf("[CACHE] Updated fuel types cache with %d unique fuel types", len(fuelTypesCache))
}

// Get the current list of unique fuel types from the cache
func getCachedFuelTypes() []string {
	fuelTypesCacheMutex.Lock()
	defer fuelTypesCacheMutex.Unlock()

	return append([]string(nil), fuelTypesCache...) // Return a copy to prevent external modification
}

// return a list of stations that have the specified fuel type available, by checking the fuel types of each station in memory
func filterStationsByFuelType(stations []Station, fuelType string) []Station {
	if fuelType == "" {
		return stations
	}

	filtered := make([]Station, 0)
	for _, station := range stations {
		// a station has a slice of strings called FuelTypes.
		// if any of the strings in that slice match the fuelType parameter,
		// include that station in the filtered results
		for _, ft := range station.FuelTypes {
			if ft == fuelType {
				filtered = append(filtered, station)
				break
			}
		}
	}

	return filtered
}

func continuousUpdateCachedFuelTypes() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		log.Println("[CACHE] Updating fuel types cache")
		updateFuelTypesCache()
		<-ticker.C
	}
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

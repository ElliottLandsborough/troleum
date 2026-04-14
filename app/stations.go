package main

import (
	"context"
	"log"
	"math"
	"sort"
	"time"
)

var stationsCycleWait = time.After
var stationsAbortCycleWait = time.After
var fetchStationsPageForCycle = fetchStationsPage

const stationsAbortCycleBackoff = 5 * time.Minute
const stationsAbortCycleMaxBackoff = time.Hour
const stationsMaxConsecutiveSkippedPages = 3

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
	Distance                    float64      `json:"distance,omitempty"` // Distance in miles from a given location, calculated on the fly
}

type Location struct {
	AddressLine1 string    `json:"address_line_1"`
	AddressLine2 string    `json:"address_line_2"`
	City         string    `json:"city"`
	Country      string    `json:"country"`
	County       string    `json:"county"`
	Postcode     string    `json:"postcode"`
	Latitude     FlexFloat `json:"latitude"`
	Longitude    FlexFloat `json:"longitude"`
}

type OpeningTimes struct {
	UsualDays   UsualDays   `json:"usual_days"`
	BankHoliday BankHoliday `json:"bank_holiday"`
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

type DayHours struct {
	Open      string `json:"open"`
	Close     string `json:"close"`
	Is24Hours bool   `json:"is_24_hours"`
}

type BankHoliday struct {
	Type      string `json:"type"`
	OpenTime  string `json:"open_time"`
	CloseTime string `json:"close_time"`
	Is24Hours bool   `json:"is_24_hours"`
}

func continuousFetchStations(ctx context.Context, client *OAuthClient, rateLimiter *time.Ticker) {
	currentPage := 1
	var cycleStartTime time.Time
	consecutiveCycleAborts := 0
	consecutiveSkippedPages := 0

	for {
		select {
		case <-ctx.Done():
			log.Println("[STATIONS] Shutdown requested, stopping fetch worker")
			return
		default:
		}

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
					select {
					case <-ctx.Done():
						log.Println("[STATIONS] Shutdown requested, stopping fetch worker")
						return
					case <-stationsCycleWait(waitTime):
					}
					continue
				}
			}

			cycleStartTime = time.Now()
			log.Println("[STATIONS] Starting new cycle from page 1")
		}

		fetchResult := fetchStationsPageForCycle(ctx, client, currentPage, rateLimiter)

		if fetchResult == pageFetchFinalPage {
			consecutiveCycleAborts = 0
			consecutiveSkippedPages = 0
			cycleDuration := time.Since(cycleStartTime)
			now := time.Now()

			cycleTimeMutex.Lock()
			lastStationsCycleComplete = now
			cycleTimeMutex.Unlock()

			log.Printf("[STATIONS] Reached final page, cycle completed in %v, restarting from page 1", cycleDuration)
			currentPage = 1
			continue
		}

		if fetchResult == pageFetchAbortCycle {
			consecutiveCycleAborts++
			consecutiveSkippedPages = 0
			abortDelay := computeAbortBackoff(stationsAbortCycleBackoff, stationsAbortCycleMaxBackoff, consecutiveCycleAborts)
			log.Printf("[STATIONS] Cycle aborted before final page (stopped at page %d), retrying page %d after %v (abort attempt %d)", currentPage, currentPage, abortDelay, consecutiveCycleAborts)
			select {
			case <-ctx.Done():
				log.Println("[STATIONS] Shutdown requested, stopping fetch worker")
				return
			case <-stationsAbortCycleWait(abortDelay):
			}
			continue
		}

		if fetchResult == pageFetchSkipPage {
			consecutiveCycleAborts = 0
			consecutiveSkippedPages++
			if consecutiveSkippedPages >= stationsMaxConsecutiveSkippedPages {
				cycleDuration := time.Since(cycleStartTime)
				now := time.Now()

				cycleTimeMutex.Lock()
				lastStationsCycleComplete = now
				cycleTimeMutex.Unlock()

				log.Printf("[STATIONS] Ending cycle after %d consecutive skipped page(s) (latest page %d), duration %v, restarting from page 1", consecutiveSkippedPages, currentPage, cycleDuration)
				currentPage = 1
				consecutiveSkippedPages = 0
				continue
			}

			currentPage++
			maxPagesThisCycle := getDynamicMaxPagesPerCycle(true)
			if currentPage > maxPagesThisCycle {
				cycleDuration := time.Since(cycleStartTime)
				now := time.Now()

				cycleTimeMutex.Lock()
				lastStationsCycleComplete = now
				cycleTimeMutex.Unlock()

				log.Printf("[STATIONS] Ending cycle at safety page cap (%d), duration %v, restarting from page 1", maxPagesThisCycle, cycleDuration)
				currentPage = 1
				consecutiveSkippedPages = 0
			}
			continue
		}

		if fetchResult == pageFetchContinue {
			consecutiveCycleAborts = 0
			consecutiveSkippedPages = 0
			currentPage++
			maxPagesThisCycle := getDynamicMaxPagesPerCycle(true)
			if currentPage > maxPagesThisCycle {
				cycleDuration := time.Since(cycleStartTime)
				now := time.Now()

				cycleTimeMutex.Lock()
				lastStationsCycleComplete = now
				cycleTimeMutex.Unlock()

				log.Printf("[STATIONS] Ending cycle at safety page cap (%d), duration %v, restarting from page 1", maxPagesThisCycle, cycleDuration)
				currentPage = 1
			}
		}
	}
}

// haversine returns the distance in kilometres between two lat/lon points.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Earth's radius in km

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	lat1R := lat1 * math.Pi / 180
	lat2R := lat2 * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1R)*math.Cos(lat2R)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func StationsByDistance(stations []Station, lat, lon float64) []Station {
	type candidate struct {
		station  Station
		distance float64
	}

	candidates := make([]candidate, len(stations))
	for i, s := range stations {
		candidates[i] = candidate{
			station:  s,
			distance: haversine(lat, lon, float64(s.Location.Latitude), float64(s.Location.Longitude)),
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})

	sortedStations := make([]Station, len(candidates))
	for i, c := range candidates {
		sortedStations[i] = c.station
	}

	return sortedStations
}

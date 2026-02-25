package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"
)

type LatLon struct {
	Lat float64
	Lon float64
}

type ResponseCache struct {
	CreatedAt time.Time       // timestamp
	Data      json.RawMessage // unlimited JSON string
}

// How many petrol stations are there in the uk vs how many are on the system?
// Google says 8,000 stations in uk, i've set this to 100,000 to be safe
var stations = make([]Station, 0, 100000)
var stationsIndex = make(map[string]int, 100000)
var stationsMutex sync.Mutex

// Stations and price listings are 1:1 so we should have no more then 8,000 (on dev?)
// Set the same extremely high size in case there are more on prod.
var priceStations = make([]PriceStation, 0, 100000)
var priceStationsIndex = make(map[string]int, 100000)
var priceStationsMutex sync.Mutex

// 1:1 with stations. This is what we will use to search for nearby stations
var stationLocations = make(map[string]LatLon, 100000)
var stationLocationsMutex sync.Mutex

// Map of indexed json files already saved with their page numbers and datestamps
// I have set a size of 1000, i think there are less than 20 pages on dev, not sure about prod.
var savedStationsPages = make(map[int]ResponseCache, 1000)
var savedPricesPages = make(map[int]ResponseCache, 1000)
var savedStationsPagesMutex sync.Mutex
var savedPricesPagesMutex sync.Mutex

// enrichmentTimer is a global timer that triggers the enrichment process
var enrichmentTimer *time.Timer
var enrichmentTimerMutex sync.Mutex

// enrichmentInterval is the duration between automatic enrichments
const enrichmentInterval = 10 * time.Minute

// Reset the enrichment timer with a new duration, ensuring thread safety
func resetEnrichmentTimerLocked(d time.Duration) {
	if enrichmentTimer == nil {
		return
	}
	if !enrichmentTimer.Stop() {
		select {
		case <-enrichmentTimer.C:
		default:
		}
	}
	enrichmentTimer.Reset(d)
}

// InitEnrichmentTimer initializes the enrichment timer that triggers loading data from cached responses
func initEnrichmentTimer(ctx context.Context) {
	enrichmentTimer = time.NewTimer(enrichmentInterval)

	go func() {
		for {
			select {
			case <-enrichmentTimer.C:
				triggerEnrichmentWithReset()
			case <-ctx.Done():
				enrichmentTimerMutex.Lock()
				if enrichmentTimer != nil {
					enrichmentTimer.Stop()
				}
				enrichmentTimerMutex.Unlock()
				log.Println("Enrichment worker stopped")
				return
			}
		}
	}()

	// Run immediately on startup
	triggerEnrichmentWithReset()
}

// Call this manually OR let the timer call it automatically
func triggerEnrichmentWithReset() {
	enrichmentTimerMutex.Lock()
	defer enrichmentTimerMutex.Unlock()

	if enrichmentTimer == nil {
		log.Println("[ENRICH] WARNING: Timer not initialized yet, skipping")
		return
	}
	loadDataFromCachedResponses()
	// Next enrichment will run 600 seconds (10 minutes) after the last one finishes
	// regardless of if you manually execute it or if the timer executes it.
	resetEnrichmentTimerLocked(enrichmentInterval)
}

// StoreJSONPageInMemory saves the raw JSON string of a page into the appropriate in-memory map for later enrichment
func StoreJSONPageInMemory(pageNum int, jsonString string, requestType RequestType, nodeIdCount int) {
	if nodeIdCount == 0 {
		log.Printf("[CACHE] ERROR: No node_id occurrences in current page %d of type %s, skipping caching in memory.", pageNum, requestType)
		return
	}

	cache := ResponseCache{
		Data:      json.RawMessage(jsonString),
		CreatedAt: time.Now(),
	}

	switch requestType {
	case RequestTypeStationsPage:
		savedStationsPagesMutex.Lock()
		savedStationsPages[pageNum] = cache
		if nodeIdCount < NodeIDCountThreshold {
			log.Printf("[CACHE] WARNING: Page %d of type %s has a low node_id count of %d", pageNum, requestType, nodeIdCount)
			ClearCachedPageDataAbovePageNum(savedStationsPages, requestType, pageNum)
		}

		savedStationsPagesMutex.Unlock()
	case RequestTypePricesPage:
		savedPricesPagesMutex.Lock()
		savedPricesPages[pageNum] = cache
		if nodeIdCount < NodeIDCountThreshold {
			log.Printf("[CACHE] WARNING: Page %d of type %s has a low node_id count of %d", pageNum, requestType, nodeIdCount)
			ClearCachedPageDataAbovePageNum(savedPricesPages, requestType, pageNum)
		}
		savedPricesPagesMutex.Unlock()
	}

	// After storing the page in memory, we can trigger the enrichment process immediately
	triggerEnrichmentWithReset()
}

// This function will take a global slice, a mutex, and an integer
func ClearCachedPageDataAbovePageNum(responseCache map[int]ResponseCache, requestType RequestType, pageNum int) {
	log.Printf("[CACHE] Clearing cached page data above page %d of type %s due to low node_id count", pageNum, requestType)
	for key := range responseCache {
		if key > pageNum {
			delete(responseCache, key)
			log.Printf("[CACHE] Deleted cached page %d of type %s from memory as it's higher than current page %d of type %s", key, requestType, pageNum, requestType)
		}
	}
}

// LoadDataFromCachedResponses processes the JSON data stored in the in-memory maps and merges it into the global stations and priceStations slices and indexes for enrichment
func loadDataFromCachedResponses() {
	log.Println("[ENRICH] Loading data from in-memory cached responses for enrichment")

	// Load price data from in-memory cache
	savedPricesPagesMutex.Lock()
	log.Printf("[ENRICH] Found %d cached price pages in memory", len(savedPricesPages))
	for pageNum, cache := range savedPricesPages {
		priceStationsList, err := processJSONArray[PriceStation](cache.Data, pageNum, "price")
		if err != nil {
			log.Printf("[ENRICH] Error processing cached price data for page %d: %v", pageNum, err)
			continue
		}
		mergeEntities(priceStationsList, &priceStations, priceStationsIndex, &priceStationsMutex)
	}
	savedPricesPagesMutex.Unlock()

	// Load station data from in-memory cache
	savedStationsPagesMutex.Lock()
	log.Printf("[ENRICH] Found %d cached station pages in memory", len(savedStationsPages))
	for pageNum, cache := range savedStationsPages {
		stationList, err := processJSONArray[Station](cache.Data, pageNum, "station")
		if err != nil {
			log.Printf("[ENRICH] Error processing cached station data for page %d: %v", pageNum, err)
			continue
		}
		mergeEntities(stationList, &stations, stationsIndex, &stationsMutex)
		mergeStationLocations(stationList)
	}
	savedStationsPagesMutex.Unlock()
}

// NodeIDEntity interface for entities that have a NodeID field
type NodeIDEntity interface {
	GetNodeID() string
}

// Implement NodeIDEntity interface for Station
func (s Station) GetNodeID() string {
	return s.NodeID
}

// Implement NodeIDEntity interface for PriceStation
func (p PriceStation) GetNodeID() string {
	return p.NodeID
}

// Generic merge function for entities with NodeID
func mergeEntities[T NodeIDEntity](newEntities []T, globalSlice *[]T, globalIndex map[string]int, mutex *sync.Mutex) {
	mutex.Lock()
	defer mutex.Unlock()

	for _, entity := range newEntities {
		nodeID := entity.GetNodeID()
		if _, exists := globalIndex[nodeID]; !exists {
			globalIndex[nodeID] = len(*globalSlice)
			*globalSlice = append(*globalSlice, entity)
		}
	}
}

// Merges station locations from newStations into the stationLocations map
func mergeStationLocations(newStations []Station) {
	stationLocationsMutex.Lock()
	defer stationLocationsMutex.Unlock()
	for _, newStation := range newStations {
		if _, exists := stationLocations[newStation.NodeID]; !exists {
			stationLocations[newStation.NodeID] = LatLon{
				Lat: float64(newStation.Location.Latitude),
				Lon: float64(newStation.Location.Longitude),
			}
		}
	}
}

/*
// Merges fuel prices from newPriceStations into the priceStations slice and index
func mergeFuelPrices(newPriceStations []PriceStation) {
	priceStationsMutex.Lock()
	defer priceStationsMutex.Unlock()

	for _, newPriceStation := range newPriceStations {
		nodeID := newPriceStation.NodeID
		if _, exists := priceStationsIndex[nodeID]; !exists {
			priceStationsIndex[nodeID] = len(priceStations)
			priceStations = append(priceStations, newPriceStation)
		} else {
			// If the station already exists, we can choose to update the price information if needed
			existingIndex := priceStationsIndex[nodeID]
			priceStations[existingIndex].DieselPrice = newPriceStation.DieselPrice
			priceStations[existingIndex].Petrol95Price = newPriceStation.Petrol95Price
			priceStations[existingIndex].Petrol98Price = newPriceStation.Petrol98Price
		}
	}
}
*/

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

// fuelTypesCache is a global cache of all unique fuel types available across all stations in memory, updated during enrichment
var fuelTypesCache []string
var fuelTypesCacheMutex sync.Mutex

// enrichmentInterval is the duration between automatic enrichments
const enrichmentInterval = 60 * time.Minute

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
				log.Println("[ENRICH] Enrichment worker stopped")
				return
			}
		}
	}()

	// Run immediately on startup
	//triggerEnrichmentWithReset()
}

// Call this manually OR let the timer call it automatically
func triggerEnrichmentWithReset() {
	enrichmentTimerMutex.Lock()
	defer enrichmentTimerMutex.Unlock()

	if enrichmentTimer == nil {
		log.Println("[ENRICH] WARNING: Timer not initialized yet, skipping")
		return
	}
	loadDataFromAllCachedPageResponses()
	// Next enrichment runs one enrichmentInterval after the last run finishes,
	// regardless of if you manually execute it or if the timer executes it.
	resetEnrichmentTimerLocked(enrichmentInterval)
}

// StoreJSONPageInMemory saves the raw JSON string of a page into the appropriate in-memory map for later enrichment
func StoreJSONPageInMemory(pageNum int, jsonString string, requestType RequestType, nodeIdCount int) {
	if nodeIdCount == 0 {
		log.Printf("[CACHE] ERROR: No node_id occurrences in current page %d of type %s, skipping caching in memory", pageNum, requestType)
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
	loadDataFromSingleCachedPageResponse(pageNum, requestType)
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

func loadDataFromSingleCachedPageResponse(pageNum int, requestType RequestType) {
	var cache ResponseCache
	var exists bool

	switch requestType {
	case RequestTypeStationsPage:
		savedStationsPagesMutex.Lock()
		cache, exists = savedStationsPages[pageNum]
		savedStationsPagesMutex.Unlock()
	case RequestTypePricesPage:
		savedPricesPagesMutex.Lock()
		cache, exists = savedPricesPages[pageNum]
		savedPricesPagesMutex.Unlock()
	default:
		log.Printf("[ENRICH] Invalid request type %s for loading cached page data", requestType)
		return
	}

	if !exists {
		log.Printf("[ENRICH] No cached data found in memory for page %d of type %s", pageNum, requestType)
		return
	}

	log.Printf("[ENRICH] Loading data from cached response for page %d of type %s", pageNum, requestType)

	switch requestType {
	case RequestTypeStationsPage:
		stationList, err := processJSONArray[Station](cache.Data, pageNum, RequestTypeStationsPage)
		if err != nil {
			log.Printf("[ENRICH] Error processing cached station data for page %d: %v", pageNum, err)
			return
		}

		sanitizedStations, fixedCoordsCount, droppedCoordsCount := sanitizeStationsForUKMapView(stationList)
		if fixedCoordsCount > 0 || droppedCoordsCount > 0 {
			log.Printf("[ENRICH] Page %d station coordinate normalization: fixed=%d dropped=%d", pageNum, fixedCoordsCount, droppedCoordsCount)
		}

		mergeEntities(sanitizedStations, &stations, stationsIndex, &stationsMutex)
		mergeStationLocations(sanitizedStations)
	case RequestTypePricesPage:
		priceStationsList, err := processJSONArray[PriceStation](cache.Data, pageNum, RequestTypePricesPage)
		if err != nil {
			log.Printf("[ENRICH] Error processing cached price data for page %d: %v", pageNum, err)
			return
		}

		// Some upstream records occasionally arrive in pounds (e.g. 1.55)
		// while most are in pence (e.g. 155.0). Normalize before merging.
		normalizedCount := normalizePriceStationsFuelPrices(priceStationsList)
		if normalizedCount > 0 {
			log.Printf("[ENRICH] Page %d: normalized %d price value(s) before merge", pageNum, normalizedCount)
		}

		mergePriceStations(priceStationsList)
	}
}

// LoadDataFromCachedResponses processes the JSON data stored in the in-memory maps and merges it into the global stations and priceStations slices and indexes for enrichment
func loadDataFromAllCachedPageResponses() {
	removeMissingStations()

	log.Println("[ENRICH] Loading data from ALL in-memory cached responses for enrichment")

	// Collect price page numbers while locked to avoid race condition
	savedPricesPagesMutex.Lock()
	pricePageNums := make([]int, 0, len(savedPricesPages))
	for pageNum := range savedPricesPages {
		pricePageNums = append(pricePageNums, pageNum)
	}
	log.Printf("[ENRICH] Found %d cached price pages in memory", len(pricePageNums))
	savedPricesPagesMutex.Unlock()

	// Load price data from in-memory cache
	for _, pageNum := range pricePageNums {
		loadDataFromSingleCachedPageResponse(pageNum, RequestTypePricesPage)
	}

	// Collect station page numbers while locked to avoid race condition
	savedStationsPagesMutex.Lock()
	stationPageNums := make([]int, 0, len(savedStationsPages))
	for pageNum := range savedStationsPages {
		stationPageNums = append(stationPageNums, pageNum)
	}
	log.Printf("[ENRICH] Found %d cached station pages in memory", len(stationPageNums))
	savedStationsPagesMutex.Unlock()

	// Load station data from in-memory cache
	for _, pageNum := range stationPageNums {
		loadDataFromSingleCachedPageResponse(pageNum, RequestTypeStationsPage)
	}
}

func removeMissingStations() {
	nodeIds := make([]string, 0)

	log.Printf("[CLEANUP] Collecting node IDs from cached station pages to identify which stations are still active")

	// Snapshot the station pages while locked to avoid race conditions.
	savedStationsPagesMutex.Lock()
	stationPagesCopy := make(map[int]ResponseCache)
	for pageNum, cache := range savedStationsPages {
		stationPagesCopy[pageNum] = cache
	}
	savedStationsPagesMutex.Unlock()

	// Process snapshot outside the lock
	for pageNum, cache := range stationPagesCopy {
		stationList, err := processJSONArray[Station](cache.Data, pageNum, RequestTypeStationsPage)
		if err != nil {
			log.Printf("[ENRICH] Error processing cached station data for page %d during index reset: %v", pageNum, err)
			continue
		}
		for _, station := range stationList {
			nodeIds = append(nodeIds, station.NodeID)
		}
	}

	removeMissingNodeIDs(nodeIds)
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

// mergePriceStations upserts price stations by NodeID so newer API data replaces stale entries.
func mergePriceStations(newPriceStations []PriceStation) {
	priceStationsMutex.Lock()
	defer priceStationsMutex.Unlock()

	inserted := 0
	updated := 0

	for _, station := range newPriceStations {
		if existingIdx, exists := priceStationsIndex[station.NodeID]; exists {
			priceStations[existingIdx] = station
			updated++
			continue
		}

		priceStationsIndex[station.NodeID] = len(priceStations)
		priceStations = append(priceStations, station)
		inserted++
	}

	if inserted > 0 || updated > 0 {
		log.Printf("[ENRICH] Upserted price stations: inserted=%d updated=%d", inserted, updated)
	}
}

// Generic function that takes a slice of nodeIds and removes any stations from the global stations slice and index that are not in the provided slice of nodeIds
func removeMissingNodeIDs(nodeIds []string) {
	nodeIdSet := make(map[string]struct{}, len(nodeIds))
	for _, nodeId := range nodeIds {
		nodeIdSet[nodeId] = struct{}{}
	}

	log.Printf("[CLEANUP] Node ID count from cached pages: %d", len(nodeIdSet))

	// Call separate functions to avoid holding multiple locks simultaneously
	removeStationsNotInSet(nodeIdSet)
	removePriceStationsNotInSet(nodeIdSet)
	removeStationLocationsNotInSet(nodeIdSet)

	stationsMutex.Lock()
	stationCount := len(stations)
	stationsMutex.Unlock()
	log.Printf("[CLEANUP] Finished removing missing stations. Current station count: %d", stationCount)
}

func removeStationsNotInSet(nodeIdSet map[string]struct{}) {
	stationsMutex.Lock()
	defer stationsMutex.Unlock()

	if len(stations) == len(nodeIdSet) {
		log.Printf("[CLEANUP] Station count matches node ID count, no stations to remove.")
		return
	}

	newStations := make([]Station, 0, 100000)
	newStationsIndex := make(map[string]int, 100000)

	log.Printf("[CLEANUP] Current station count before removal: %d", len(stations))
	for _, station := range stations {
		if _, exists := nodeIdSet[station.NodeID]; exists {
			newStationsIndex[station.NodeID] = len(newStations)
			newStations = append(newStations, station)
		}
	}

	log.Printf("[CLEANUP] New station count after removal: %d", len(newStations))
	stations = newStations
	stationsIndex = newStationsIndex
}

func removePriceStationsNotInSet(nodeIdSet map[string]struct{}) {
	priceStationsMutex.Lock()
	defer priceStationsMutex.Unlock()

	if len(priceStations) == len(nodeIdSet) {
		log.Printf("[CLEANUP] Price station count matches node ID count, no price stations to remove.")
		return
	}

	newPriceStations := make([]PriceStation, 0, 100000)
	newPriceStationsIndex := make(map[string]int, 100000)

	log.Printf("[CLEANUP] Current price station count before removal: %d", len(priceStations))
	for _, priceStation := range priceStations {
		if _, exists := nodeIdSet[priceStation.NodeID]; exists {
			newPriceStationsIndex[priceStation.NodeID] = len(newPriceStations)
			newPriceStations = append(newPriceStations, priceStation)
		}
	}

	log.Printf("[CLEANUP] New price station count after removal: %d", len(newPriceStations))
	priceStations = newPriceStations
	priceStationsIndex = newPriceStationsIndex
}

func removeStationLocationsNotInSet(nodeIdSet map[string]struct{}) {
	stationLocationsMutex.Lock()
	defer stationLocationsMutex.Unlock()

	if len(stationLocations) == len(nodeIdSet) {
		log.Printf("[CLEANUP] Station locations count matches node ID count, no station locations to remove.")
		return
	}

	log.Printf("[CLEANUP] Current station locations count before removal: %d", len(stationLocations))
	newStationLocations := make(map[string]LatLon, 100000)
	for nodeId, location := range stationLocations {
		if _, exists := nodeIdSet[nodeId]; exists {
			newStationLocations[nodeId] = location
		}
	}

	log.Printf("[CLEANUP] New station locations count after removal: %d", len(newStationLocations))
	stationLocations = newStationLocations
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

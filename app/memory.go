package main

import (
	"log"
	"sync"
	"time"
)

type LatLon struct {
	Lat float64
	Lon float64
}

type ResponseCache struct {
	CreatedAt time.Time // timestamp
	Data      string    // unlimited JSON string
}

// a map with the key node_id and the value LatLon
var stationLocations = make(map[string]LatLon, 100000)
var stationLocationsMutex sync.Mutex

// Up to 100,000 stations expected (~80,000 in the uk)
var stations = make([]Station, 0, 100000)
var stationsIndex = make(map[string]int, 100000)
var stationsMutex sync.Mutex

// Up to 100,000 price entries expected, same count as stations
var priceStations = make([]PriceStation, 0, 100000)
var priceStationsIndex = make(map[string]int, 100000)
var priceStationsMutex sync.Mutex

// Map of indexed json files already saved with their page numbers and datestamps
var savedStationsPages = make(map[int]ResponseCache, 100)
var savedPricesPages = make(map[int]ResponseCache, 100)
var savedStationsPagesMutex sync.Mutex
var savedPricesPagesMutex sync.Mutex

// StoreJSONPageInMemory saves the raw JSON string of a page into the appropriate in-memory map for later enrichment
func StoreJSONPageInMemory(pageNum int, jsonString string, requestType RequestType) {
	cache := ResponseCache{
		Data:      jsonString,
		CreatedAt: time.Now(),
	}

	switch requestType {
	case RequestTypeStationsPage:
		savedStationsPagesMutex.Lock()
		savedStationsPages[pageNum] = cache
		savedStationsPagesMutex.Unlock()
	case RequestTypePricesPage:
		savedPricesPagesMutex.Lock()
		savedPricesPages[pageNum] = cache
		savedPricesPagesMutex.Unlock()
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

// List the current json files, if they exist, into the savedStationsPages and savedPricesPages maps
func enrichSavedPages() {
	/*
		// Process price data
		priceResponses, err := GetFullDataForEnrichment(RequestTypePricesPage, 100)
		if err != nil {
			log.Printf("[ENRICH] Error getting full price data for enrichment: %v", err)
		}

		for _, req := range priceResponses {
			pageNum := req.PageNumber
			createdAt := req.CreatedAt
			savedPricesPages[pageNum] = ResponseCache{
				Data:      req.Data,
				CreatedAt: createdAt,
			}

			priceStationsList, err := processJSONArray[PriceStation](req.Data, pageNum, "price")
			if err != nil {
				log.Printf("[ENRICH] %v", err)
				continue
			}

			mergeEntities(priceStationsList, &priceStations, priceStationsIndex, &priceStationsMutex)
		}

		// Process station data
		stationRequests, err := GetFullDataForEnrichment(RequestTypeStationsPage, 100)
		if err != nil {
			log.Printf("[ENRICH] Error getting full station data for enrichment: %v", err)
		}

		for _, req := range stationRequests {
			pageNum := req.PageNumber
			createdAt := req.CreatedAt
			savedStationsPages[pageNum] = ResponseCache{
				Data:      req.Data,
				CreatedAt: createdAt,
			}

			stationList, err := processJSONArray[Station](req.Data, pageNum, "station")
			if err != nil {
				log.Printf("[ENRICH] %v", err)
				continue
			}

			mergeEntities(stationList, &stations, stationsIndex, &stationsMutex)

			// an 'array' of stations with their lat lon locations only
			mergeStationLocations(stationList)
		}
	*/
}

// Merges station locations from newStations into the stationLocations map
func mergeStationLocations(newStations []Station) {
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

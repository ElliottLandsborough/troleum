package main

import (
	"log"
	"os"
	"sync"
)

type LatLon struct {
	Lat float64
	Lon float64
}

// a map with the key node_id and the value LatLon
var stationLocations = make(map[string]LatLon)

// Up to 100,000 stations expected (~80,000 in the uk)
var stations = make([]Station, 0, 100000)
var stationsIndex = make(map[string]int, 100000)

// Up to 100,000 price entries expected, same count as stations
var priceStations = make([]PriceStation, 0, 100000)
var priceStationsIndex = make(map[string]int, 100000)

// Map of indexed json files already saved with their page numbers and datestamps
var savedStationsPages = make(map[int]string)
var savedPricesPages = make(map[int]string)

// Mutex for thread-safe access to stations slice and index
var stationsMutex sync.Mutex

// Mutex for thread-safe access to priceStations slice and index
var priceStationsMutex sync.Mutex

// update the savedStationsPages or savedPricesPages map with the last modified time of the saved file
func storeSavedPage(pageMap map[int]string, mutex *sync.Mutex, pageNum int, filePath string) {
	mutex.Lock()
	defer mutex.Unlock()

	info, err := os.Stat(filePath)
	if err == nil {
		modTime := info.ModTime()
		pageMap[pageNum] = modTime.String()
	}
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
	// Process price data
	priceResponses, err := GetFullDataForEnrichment(RequestTypePricesPage, 100)
	if err != nil {
		log.Printf("[ENRICH] Error getting full price data for enrichment: %v", err)
	}

	for _, req := range priceResponses {
		pageNum := req.PageNumber
		createdAt := req.CreatedAt
		savedPricesPages[pageNum] = createdAt.String()

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
		savedStationsPages[pageNum] = createdAt.String()

		stationList, err := processJSONArray[Station](req.Data, pageNum, "station")
		if err != nil {
			log.Printf("[ENRICH] %v", err)
			continue
		}

		mergeEntities(stationList, &stations, stationsIndex, &stationsMutex)

		// an 'array' of stations with their lat lon locations only
		mergeStationLocations(stationList)
	}
}

// Clears the in-memory saved pages maps, used for testing purposes to reset state
// Note: this does not delete any files or database entries, it just clears the in-memory maps.
func clearSavedPages() {
	savedPagesMutex.Lock()
	defer savedPagesMutex.Unlock()
	savedStationsPages = make(map[int]string)
	savedPricesPages = make(map[int]string)
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

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const testStationPageJSON = `[
	{
		"node_id": "station-1",
		"trading_name": "Alpha Fuel",
		"brand_name": "Alpha",
		"fuel_types": ["E10", "DIESEL"],
		"location": {
			"address_line_1": "1 Test Road",
			"city": "Testville",
			"postcode": "TE1 1ST",
			"latitude": 53.483959,
			"longitude": -2.244644
		}
	},
	{
		"node_id": "station-2",
		"trading_name": "Beta Fuel",
		"brand_name": "Beta",
		"fuel_types": ["E10"],
		"location": {
			"address_line_1": "2 Test Road",
			"city": "Testville",
			"postcode": "TE1 2ND",
			"latitude": 53.5,
			"longitude": -2.2
		}
	}
]`

const testPricePageJSON = `[
	{
		"node_id": "station-1",
		"trading_name": "Alpha Fuel",
		"fuel_prices": [
			{
				"fuel_type": "E10",
				"price": 152.9,
				"price_last_updated": "2026-04-10T10:00:00Z"
			},
			{
				"fuel_type": "DIESEL",
				"price": 1.609,
				"price_last_updated": "2026-04-10T10:00:00Z"
			}
		]
	},
	{
		"node_id": "station-2",
		"trading_name": "Beta Fuel",
		"fuel_prices": [
			{
				"fuel_type": "E10",
				"price": 149.9,
				"price_last_updated": "2026-04-10T10:00:00Z"
			}
		]
	}
]`

const testStationPageJSONStationTwoOnly = `[
	{
		"node_id": "station-2",
		"trading_name": "Beta Fuel",
		"brand_name": "Beta",
		"fuel_types": ["E10"],
		"location": {
			"address_line_1": "2 Test Road",
			"city": "Testville",
			"postcode": "TE1 2ND",
			"latitude": 53.5,
			"longitude": -2.2
		}
	}
]`

func resetGlobalMemoryStateForTest() {
	stationsMutex.Lock()
	stations = make([]Station, 0, 100000)
	stationsIndex = make(map[string]int, 100000)
	stationsMutex.Unlock()

	priceStationsMutex.Lock()
	priceStations = make([]PriceStation, 0, 100000)
	priceStationsIndex = make(map[string]int, 100000)
	priceStationsMutex.Unlock()

	stationLocationsMutex.Lock()
	stationLocations = make(map[string]LatLon, 100000)
	stationLocationsMutex.Unlock()

	savedStationsPagesMutex.Lock()
	savedStationsPages = make(map[int]ResponseCache, 1000)
	savedStationsPagesMutex.Unlock()

	savedPricesPagesMutex.Lock()
	savedPricesPages = make(map[int]ResponseCache, 1000)
	savedPricesPagesMutex.Unlock()

	fuelTypesCacheMutex.Lock()
	fuelTypesCache = nil
	fuelTypesCacheMutex.Unlock()

	enrichmentTimerMutex.Lock()
	enrichmentTimer = nil
	enrichmentTimerMutex.Unlock()
}

func TestPriceStationGetNodeID(t *testing.T) {
	station := PriceStation{NodeID: "station-xyz"}
	if got := station.GetNodeID(); got != "station-xyz" {
		t.Fatalf("GetNodeID() = %q, want %q", got, "station-xyz")
	}
}

func withTempWorkingDir(t *testing.T) string {
	t.Helper()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	return tempDir
}

func TestRemoveMissingStationsUsesStationCache(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	StoreJSONPageInMemory(1, testStationPageJSON, RequestTypeStationsPage, strings.Count(testStationPageJSON, "node_id"))
	StoreJSONPageInMemory(1, testPricePageJSON, RequestTypePricesPage, strings.Count(testPricePageJSON, "node_id"))

	StoreJSONPageInMemory(1, testStationPageJSONStationTwoOnly, RequestTypeStationsPage, strings.Count(testStationPageJSONStationTwoOnly, "node_id"))
	removeMissingStations()

	stationsMutex.Lock()
	defer stationsMutex.Unlock()

	if len(stations) != 1 {
		t.Fatalf("expected 1 active station after cleanup, got %d", len(stations))
	}
	if stations[0].NodeID != "station-2" {
		t.Fatalf("expected remaining station to be station-2, got %s", stations[0].NodeID)
	}
	if _, exists := stationsIndex["station-1"]; exists {
		t.Fatalf("expected station-1 to be removed from stationsIndex")
	}
}

func TestLoadDataFromAllCachedPageResponses(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	stationsMutex.Lock()
	stations = []Station{{NodeID: "stale"}}
	stationsIndex = map[string]int{"stale": 0}
	stationsMutex.Unlock()

	savedStationsPagesMutex.Lock()
	savedStationsPages[1] = ResponseCache{CreatedAt: time.Now(), Data: json.RawMessage(testStationPageJSON)}
	savedStationsPagesMutex.Unlock()

	savedPricesPagesMutex.Lock()
	savedPricesPages[1] = ResponseCache{CreatedAt: time.Now(), Data: json.RawMessage(testPricePageJSON)}
	savedPricesPagesMutex.Unlock()

	loadDataFromAllCachedPageResponses()

	stationsMutex.Lock()
	_, staleExists := stationsIndex["stale"]
	stationCount := len(stations)
	stationsMutex.Unlock()
	if staleExists {
		t.Fatal("expected stale station to be removed during full cached load")
	}
	if stationCount != 2 {
		t.Fatalf("expected 2 stations after reload, got %d", stationCount)
	}

	priceStationsMutex.Lock()
	priceCount := len(priceStations)
	priceStationsMutex.Unlock()
	if priceCount != 2 {
		t.Fatalf("expected 2 price stations after reload, got %d", priceCount)
	}
}

func TestConcurrentCacheUpdatesAndHandlers(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	stationNodeCount := strings.Count(testStationPageJSON, "node_id")
	priceNodeCount := strings.Count(testPricePageJSON, "node_id")

	StoreJSONPageInMemory(1, testStationPageJSON, RequestTypeStationsPage, stationNodeCount)
	StoreJSONPageInMemory(1, testPricePageJSON, RequestTypePricesPage, priceNodeCount)

	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(pageNum int) {
			defer wg.Done()
			for iteration := 0; iteration < 50; iteration++ {
				StoreJSONPageInMemory(pageNum, testStationPageJSON, RequestTypeStationsPage, stationNodeCount)
				StoreJSONPageInMemory(pageNum, testPricePageJSON, RequestTypePricesPage, priceNodeCount)
				updateFuelTypesCache()
			}
		}(i + 1)
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 50; iteration++ {
				stationRequest := httptest.NewRequest(http.MethodGet, "/api/stations?fuel_type=E10&lat=53.483959&lng=-2.244644", nil)
				stationRecorder := httptest.NewRecorder()
				stationsAPIHandler(stationRecorder, stationRequest)
				if stationRecorder.Code != http.StatusOK {
					t.Errorf("stations handler returned status %d", stationRecorder.Code)
					return
				}

				fuelTypesRequest := httptest.NewRequest(http.MethodGet, "/api/fuel-types", nil)
				fuelTypesRecorder := httptest.NewRecorder()
				fuelTypesAPIHandler(fuelTypesRecorder, fuelTypesRequest)
				if fuelTypesRecorder.Code != http.StatusOK {
					t.Errorf("fuel types handler returned status %d", fuelTypesRecorder.Code)
					return
				}
			}
		}()
	}

	wg.Wait()

	stationsMutex.Lock()
	stationCount := len(stations)
	stationsMutex.Unlock()
	if stationCount == 0 {
		t.Fatal("expected stations to remain populated after concurrent cache updates")
	}

	priceStationsMutex.Lock()
	priceStationCount := len(priceStations)
	priceStationsMutex.Unlock()
	if priceStationCount == 0 {
		t.Fatal("expected price stations to remain populated after concurrent cache updates")
	}

	fuelTypes := getCachedFuelTypes()
	if len(fuelTypes) == 0 {
		t.Fatal("expected fuel types cache to remain populated after concurrent cache updates")
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectDiskCacheStatsMissingDirectory(t *testing.T) {
	withTempWorkingDir(t)

	now := time.Now()
	stats := collectDiskCacheStats(now)

	if stats.JSONFileCount != 0 {
		t.Fatalf("expected 0 json files when directory is missing, got %d", stats.JSONFileCount)
	}
	if stats.OldestFileName != "" {
		t.Fatalf("expected no oldest file name, got %q", stats.OldestFileName)
	}
}

func TestCollectDiskCacheStatsWithFiles(t *testing.T) {
	withTempWorkingDir(t)

	if err := os.MkdirAll("json", 0o755); err != nil {
		t.Fatalf("mkdir json: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	oldestPath := filepath.Join("json", "stations_page_1.json")
	newestPath := filepath.Join("json", "prices_page_2.json")
	if err := os.WriteFile(oldestPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write oldest file: %v", err)
	}
	if err := os.WriteFile(newestPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write newest file: %v", err)
	}
	if err := os.WriteFile(filepath.Join("json", "README.txt"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write non-json file: %v", err)
	}

	oldestMod := now.Add(-20 * time.Minute)
	newestMod := now.Add(-2 * time.Minute)
	if err := os.Chtimes(oldestPath, oldestMod, oldestMod); err != nil {
		t.Fatalf("chtimes oldest file: %v", err)
	}
	if err := os.Chtimes(newestPath, newestMod, newestMod); err != nil {
		t.Fatalf("chtimes newest file: %v", err)
	}

	stats := collectDiskCacheStats(now)
	if stats.JSONFileCount != 2 {
		t.Fatalf("expected 2 json files, got %d", stats.JSONFileCount)
	}
	if stats.OldestFileName != "stations_page_1.json" {
		t.Fatalf("expected oldest file stations_page_1.json, got %q", stats.OldestFileName)
	}
	if stats.NewestFileName != "prices_page_2.json" {
		t.Fatalf("expected newest file prices_page_2.json, got %q", stats.NewestFileName)
	}
	if stats.OldestFileAgeSeconds < 1199 || stats.OldestFileAgeSeconds > 1201 {
		t.Fatalf("expected oldest age around 1200s, got %d", stats.OldestFileAgeSeconds)
	}
	if stats.NewestFileAgeSeconds < 119 || stats.NewestFileAgeSeconds > 121 {
		t.Fatalf("expected newest age around 120s, got %d", stats.NewestFileAgeSeconds)
	}
}

func TestCollectMemoryStats(t *testing.T) {
	resetGlobalMemoryStateForTest()
	t.Cleanup(resetGlobalMemoryStateForTest)

	now := time.Now().UTC().Truncate(time.Second)

	stationsMutex.Lock()
	stations = []Station{{NodeID: "s1"}, {NodeID: "s2"}, {NodeID: "s3"}}
	stationsMutex.Unlock()

	priceStationsMutex.Lock()
	priceStations = []PriceStation{
		{NodeID: "s1", FuelPrices: []FuelPrice{{FuelType: "E10", Price: 145.9}, {FuelType: "B7", Price: 154.9}}},
		{NodeID: "s2", FuelPrices: []FuelPrice{{FuelType: "E5", Price: 159.9}}},
	}
	priceStationsMutex.Unlock()

	stationLocationsMutex.Lock()
	stationLocations = map[string]LatLon{"s1": {Lat: 1, Lon: 1}, "s2": {Lat: 2, Lon: 2}}
	stationLocationsMutex.Unlock()

	fuelTypesCacheMutex.Lock()
	fuelTypesCache = []string{"E10", "E5", "B7"}
	fuelTypesCacheMutex.Unlock()

	savedStationsPagesMutex.Lock()
	savedStationsPages[1] = ResponseCache{CreatedAt: now.Add(-30 * time.Minute)}
	savedStationsPagesMutex.Unlock()

	savedPricesPagesMutex.Lock()
	savedPricesPages[1] = ResponseCache{CreatedAt: now.Add(-10 * time.Minute)}
	savedPricesPagesMutex.Unlock()

	stats := collectMemoryStats(now)
	if stats.StationsCount != 3 {
		t.Fatalf("expected 3 stations, got %d", stats.StationsCount)
	}
	if stats.PriceStationsCount != 2 {
		t.Fatalf("expected 2 price stations, got %d", stats.PriceStationsCount)
	}
	if stats.StationPriceEntriesCount != 3 {
		t.Fatalf("expected 3 station price entries, got %d", stats.StationPriceEntriesCount)
	}
	if stats.StationLocationsCount != 2 {
		t.Fatalf("expected 2 station locations, got %d", stats.StationLocationsCount)
	}
	if stats.FuelTypesCachedCount != 3 {
		t.Fatalf("expected 3 cached fuel types, got %d", stats.FuelTypesCachedCount)
	}
	if stats.CachedStationPagesCount != 1 {
		t.Fatalf("expected 1 cached station page, got %d", stats.CachedStationPagesCount)
	}
	if stats.CachedPricePagesCount != 1 {
		t.Fatalf("expected 1 cached price page, got %d", stats.CachedPricePagesCount)
	}
	if stats.OldestCachedPageAgeSeconds < 1799 || stats.OldestCachedPageAgeSeconds > 1801 {
		t.Fatalf("expected oldest cached page age around 1800s, got %d", stats.OldestCachedPageAgeSeconds)
	}
}

func TestCollectGovAPIStatsUnavailable(t *testing.T) {
	setActiveOAuthClient(nil)

	stats := collectGovAPIStats(time.Now())
	if stats.StatsAvailable {
		t.Fatal("expected stats to be unavailable without active OAuth client")
	}
}

func TestCollectGovAPIStatsAvailable(t *testing.T) {
	setActiveOAuthClient(nil)
	t.Cleanup(func() { setActiveOAuthClient(nil) })

	now := time.Now().UTC().Truncate(time.Second)
	client := NewOAuthClient("https://example.test/token", "id", "secret", "scope")
	client.statsMu.Lock()
	client.statsStartedAt = now.Add(-10 * time.Minute)
	client.statsTotalRequests = 30
	client.stats2xxCount = 20
	client.stats4xxCount = 8
	client.stats5xxCount = 2
	client.stats401Count = 1
	client.stats403Count = 3
	client.statsNetworkErrors = 4
	client.statsInFlight = 1
	client.statsPeakInFlight = 1
	client.statsMu.Unlock()
	setActiveOAuthClient(client)

	stats := collectGovAPIStats(now)
	if !stats.StatsAvailable {
		t.Fatal("expected stats to be available")
	}
	if stats.TotalRequests != 30 {
		t.Fatalf("expected total requests 30, got %d", stats.TotalRequests)
	}
	if stats.Requests403 != 3 {
		t.Fatalf("expected 3 forbidden responses, got %d", stats.Requests403)
	}
	if stats.AvgRequestsPerMinute < 2.99 || stats.AvgRequestsPerMinute > 3.01 {
		t.Fatalf("expected avg requests/min around 3.0, got %.4f", stats.AvgRequestsPerMinute)
	}
	if stats.PercentOf30RPMLimit < 9.9 || stats.PercentOf30RPMLimit > 10.1 {
		t.Fatalf("expected ~10%% of 30 rpm limit, got %.4f", stats.PercentOf30RPMLimit)
	}
}

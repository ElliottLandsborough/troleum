package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type statsResponse struct {
	Code int       `json:"code"`
	Data statsData `json:"data"`
}

type statsData struct {
	GeneratedAt string        `json:"generated_at"`
	DiskCache   diskCacheInfo `json:"disk_cache"`
	Memory      memoryInfo    `json:"memory"`
	GovAPI      govAPIInfo    `json:"gov_api"`
}

type diskCacheInfo struct {
	JSONFileCount         int    `json:"json_file_count"`
	OldestFileName        string `json:"oldest_file_name,omitempty"`
	OldestFileModifiedAt  string `json:"oldest_file_modified_at,omitempty"`
	OldestFileAgeSeconds  int64  `json:"oldest_file_age_seconds"`
	OldestFileAgeHuman    string `json:"oldest_file_age_human"`
	NewestFileName        string `json:"newest_file_name,omitempty"`
	NewestFileModifiedAt  string `json:"newest_file_modified_at,omitempty"`
	NewestFileAgeSeconds  int64  `json:"newest_file_age_seconds"`
	NewestFileAgeHuman    string `json:"newest_file_age_human"`
}

type memoryInfo struct {
	StationsCount              int   `json:"stations_count"`
	PriceStationsCount         int   `json:"price_stations_count"`
	StationPriceEntriesCount   int   `json:"station_price_entries_count"`
	StationLocationsCount      int   `json:"station_locations_count"`
	FuelTypesCachedCount       int   `json:"fuel_types_cached_count"`
	CachedStationPagesCount    int   `json:"cached_station_pages_count"`
	CachedPricePagesCount      int   `json:"cached_price_pages_count"`
	OldestCachedPageAgeSeconds int64 `json:"oldest_cached_page_age_seconds"`
	OldestCachedPageAgeHuman   string `json:"oldest_cached_page_age_human"`
}

type govAPIInfo struct {
	StatsAvailable       bool    `json:"stats_available"`
	StatsSince           string  `json:"stats_since,omitempty"`
	TotalRequests        int     `json:"total_requests"`
	Requests2xx          int     `json:"requests_2xx"`
	Requests4xx          int     `json:"requests_4xx"`
	Requests5xx          int     `json:"requests_5xx"`
	Requests401          int     `json:"requests_401"`
	Requests403          int     `json:"requests_403"`
	NetworkErrors        int     `json:"network_errors"`
	InFlightRequests     int     `json:"in_flight_requests"`
	PeakInFlightRequests int     `json:"peak_in_flight_requests"`
	AvgRequestsPerMinute float64 `json:"avg_requests_per_minute"`
	PercentOf30RPMLimit  float64 `json:"percent_of_30_rpm_limit"`
}

func statsAPIHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	disk := collectDiskCacheStats(now)
	memory := collectMemoryStats(now)
	gov := collectGovAPIStats(now)

	response := statsResponse{
		Code: http.StatusOK,
		Data: statsData{
			GeneratedAt: now.UTC().Format(time.RFC3339),
			DiskCache:   disk,
			Memory:      memory,
			GovAPI:      gov,
		},
	}

	if err := writeJSONPretty(w, response); err != nil {
		http.Error(w, "Failed to encode stats response", http.StatusInternalServerError)
		return
	}
}

func collectDiskCacheStats(now time.Time) diskCacheInfo {
	entries, err := os.ReadDir("json")
	if err != nil {
		return diskCacheInfo{}
	}

	oldestTime := now
	newestTime := time.Time{}
	oldestName := ""
	newestName := ""
	count := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}

		mod := info.ModTime()
		if count == 0 || mod.Before(oldestTime) {
			oldestTime = mod
			oldestName = entry.Name()
		}
		if count == 0 || mod.After(newestTime) {
			newestTime = mod
			newestName = entry.Name()
		}

		count++
	}

	if count == 0 {
		return diskCacheInfo{}
	}

	oldestAge := now.Sub(oldestTime)
	if oldestAge < 0 {
		oldestAge = 0
	}
	newestAge := now.Sub(newestTime)
	if newestAge < 0 {
		newestAge = 0
	}

	return diskCacheInfo{
		JSONFileCount:        count,
		OldestFileName:       filepath.Base(oldestName),
		OldestFileModifiedAt: oldestTime.UTC().Format(time.RFC3339),
		OldestFileAgeSeconds: int64(oldestAge.Seconds()),
		OldestFileAgeHuman:   oldestAge.Round(time.Second).String(),
		NewestFileName:       filepath.Base(newestName),
		NewestFileModifiedAt: newestTime.UTC().Format(time.RFC3339),
		NewestFileAgeSeconds: int64(newestAge.Seconds()),
		NewestFileAgeHuman:   newestAge.Round(time.Second).String(),
	}
}

func collectMemoryStats(now time.Time) memoryInfo {
	stationsMutex.Lock()
	stationsCount := len(stations)
	stationsMutex.Unlock()

	priceStationsMutex.Lock()
	priceStationsCount := len(priceStations)
	priceEntryCount := 0
	for _, station := range priceStations {
		priceEntryCount += len(station.FuelPrices)
	}
	priceStationsMutex.Unlock()

	stationLocationsMutex.Lock()
	stationLocationsCount := len(stationLocations)
	stationLocationsMutex.Unlock()

	fuelTypesCacheMutex.Lock()
	fuelTypesCount := len(fuelTypesCache)
	fuelTypesCacheMutex.Unlock()

	savedStationsPagesMutex.Lock()
	cachedStationPagesCount := len(savedStationsPages)
	oldestCachedAt := time.Time{}
	for _, cache := range savedStationsPages {
		if oldestCachedAt.IsZero() || cache.CreatedAt.Before(oldestCachedAt) {
			oldestCachedAt = cache.CreatedAt
		}
	}
	savedStationsPagesMutex.Unlock()

	savedPricesPagesMutex.Lock()
	cachedPricePagesCount := len(savedPricesPages)
	for _, cache := range savedPricesPages {
		if oldestCachedAt.IsZero() || cache.CreatedAt.Before(oldestCachedAt) {
			oldestCachedAt = cache.CreatedAt
		}
	}
	savedPricesPagesMutex.Unlock()

	oldestCachedAge := time.Duration(0)
	if !oldestCachedAt.IsZero() {
		oldestCachedAge = now.Sub(oldestCachedAt)
		if oldestCachedAge < 0 {
			oldestCachedAge = 0
		}
	}

	return memoryInfo{
		StationsCount:              stationsCount,
		PriceStationsCount:         priceStationsCount,
		StationPriceEntriesCount:   priceEntryCount,
		StationLocationsCount:      stationLocationsCount,
		FuelTypesCachedCount:       fuelTypesCount,
		CachedStationPagesCount:    cachedStationPagesCount,
		CachedPricePagesCount:      cachedPricePagesCount,
		OldestCachedPageAgeSeconds: int64(oldestCachedAge.Seconds()),
		OldestCachedPageAgeHuman:   oldestCachedAge.Round(time.Second).String(),
	}
}

func collectGovAPIStats(now time.Time) govAPIInfo {
	snapshot, ok := getGovAPIStatsSnapshot()
	if !ok {
		return govAPIInfo{StatsAvailable: false}
	}

	lifetime := now.Sub(snapshot.StartedAt)
	avgRPM := 0.0
	if lifetime > 0 {
		avgRPM = float64(snapshot.TotalRequests) / lifetime.Minutes()
	}

	percent := (avgRPM / 30.0) * 100.0
	if percent < 0 {
		percent = 0
	}

	return govAPIInfo{
		StatsAvailable:       true,
		StatsSince:           snapshot.StartedAt.UTC().Format(time.RFC3339),
		TotalRequests:        snapshot.TotalRequests,
		Requests2xx:          snapshot.Status2xx,
		Requests4xx:          snapshot.Status4xx,
		Requests5xx:          snapshot.Status5xx,
		Requests401:          snapshot.Status401,
		Requests403:          snapshot.Status403,
		NetworkErrors:        snapshot.NetworkErrors,
		InFlightRequests:     snapshot.InFlight,
		PeakInFlightRequests: snapshot.PeakInFlight,
		AvgRequestsPerMinute: avgRPM,
		PercentOf30RPMLimit:  percent,
	}
}
